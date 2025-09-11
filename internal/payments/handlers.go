package payments

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

type Handler struct{ db *gorm.DB }

func NewHandler(db *gorm.DB) *Handler { return &Handler{db: db} }

// ========== Create Checkout (client) ==========
// Dev (mock): buat payment 'initiated' dan kembalikan URL palsu
func (h *Handler) CreateCheckout(c *fiber.Ctx) error {
	if os.Getenv("PAYMENT_PROVIDER") == "stripe" {
		return fiber.NewError(fiber.StatusNotImplemented, "Stripe not wired yet")
	}
	// mock path
	clientID := auth.MustUserID(c)
	quoteIDStr := c.Params("quoteID")
	quoteID, err := uuid.Parse(quoteIDStr)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid quote id")
	}

	// Ambil quote & case untuk validasi
	var q models.Quote
	if err := h.db.First(&q, "id = ?", quoteID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	var cs models.Case
	if err := h.db.First(&cs, "id = ?", q.CaseID).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	// Pastikan client adalah pemilik case & case masih open
	if cs.ClientID.String() != clientID {
		return fiber.ErrForbidden
	}
	if cs.Status != models.CaseOpen {
		return fiber.NewError(fiber.StatusConflict, "case is not open")
	}

	// Buat payment initiated (idempotent by quoteID unique)
	pay := models.Payment{
		CaseID:          cs.ID,
		QuoteID:         q.ID,
		ClientID:        cs.ClientID,
		StripeSessionID: "mock_" + uuid.NewString(), // placeholder field
		AmountCents:     q.AmountCents,
		Status:          models.PayInitiated,
		CreatedAt:       time.Now(),
	}
	if err := h.db.Create(&pay).Error; err != nil {
		// Jika unique constraint, berarti sudah pernah dibuat → ambil saja
		var existing models.Payment
		if e := h.db.First(&existing, "quote_id = ?", q.ID).Error; e == nil {
			pay = existing
		} else {
			return fiber.ErrInternalServerError
		}
	}

	// Kembalikan URL palsu yang “seolah-olah” checkout page
	// Frontend bisa langsung redirect ke halaman success dan panggil /payments/mock/complete
	mockURL := "mock://checkout?payment_id=" + pay.ID.String()
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"payment_id":   pay.ID,
		"redirect_url": mockURL,
		"provider":     "mock",
	})
}

// ========== Mock Complete (dev only) ==========
// Body: { "payment_id": "<uuid>" }
// Header: X-Dev-Secret: <DEV_PAYMENT_SECRET>
type mockCompleteReq struct {
	PaymentID string `json:"payment_id"`
}

func (h *Handler) MockComplete(c *fiber.Ctx) error {
	if os.Getenv("APP_ENV") != "dev" || os.Getenv("PAYMENT_PROVIDER") != "mock" {
		return fiber.ErrNotFound
	}
	if c.Get("X-Dev-Secret") == "" || c.Get("X-Dev-Secret") != os.Getenv("DEV_PAYMENT_SECRET") {
		return fiber.NewError(http.StatusUnauthorized, "missing/invalid X-Dev-Secret")
	}
	var in mockCompleteReq
	if err := c.BodyParser(&in); err != nil {
		return fiber.ErrBadRequest
	}
	pid, err := uuid.Parse(in.PaymentID)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid payment id")
	}

	// Transaksi atomik: single winner
	tx := h.db.Begin()

	// 1) Muat payment (FOR UPDATE)
	var pay models.Payment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&pay, "id = ?", pid).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	if pay.Status == models.PayPaid {
		tx.Rollback()
		return c.JSON(fiber.Map{"ok": true, "message": "already paid (idempotent)"})
	}

	// 2) Muat case & quote winner
	var cs models.Case
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&cs, "id = ?", pay.CaseID).Error; err != nil {
		tx.Rollback()
		return fiber.ErrInternalServerError
	}
	var q models.Quote
	if err := tx.First(&q, "id = ?", pay.QuoteID).Error; err != nil {
		tx.Rollback()
		return fiber.ErrInternalServerError
	}

	// 3) Validasi amount (harus sesuai DB, bukan input client)
	if pay.AmountCents != q.AmountCents {
		tx.Rollback()
		return fiber.NewError(http.StatusConflict, "amount mismatch")
	}

	// 4) Set winner & reject others (jika case masih open)
	if cs.Status == models.CaseOpen {
		// winner
		if err := tx.Model(&models.Quote{}).Where("id = ?", q.ID).
			Update("status", models.QuoteAccepted).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
		// others
		if err := tx.Model(&models.Quote{}).
			Where("case_id = ? AND id <> ? AND status = ?", cs.ID, q.ID, models.QuoteProposed).
			Update("status", models.QuoteRejected).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
		// case engaged
		now := time.Now()
		if err := tx.Model(&models.Case{}).Where("id = ?", cs.ID).
			Updates(map[string]any{
				"status":             models.CaseEngaged,
				"engaged_at":         now,
				"accepted_quote_id":  q.ID,
				"accepted_lawyer_id": q.LawyerID,
			}).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
	}

	// 5) Mark payment paid (idempotent-safe)
	if err := tx.Model(&models.Payment{}).Where("id = ?", pay.ID).
		Updates(map[string]any{
			"status": models.PayPaid,
			// "stripe_payment_intent": "mock_"+uuid.NewString(), // opsional
		}).Error; err != nil {
		tx.Rollback()
		return fiber.ErrInternalServerError
	}

	if err := tx.Commit().Error; err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"ok": true})
}
