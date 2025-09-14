package payments

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/paymentintent"
	"github.com/stripe/stripe-go/v82/webhook"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
	"github.com/aldoetobex/legal-mp-backend/pkg/utils"
)

/* --------------------------------- types -------------------------------- */

type MockCompleteRequest struct {
	PaymentID string `json:"payment_id"`
}

type CheckoutResponse struct {
	PaymentID   string `json:"payment_id"`
	RedirectURL string `json:"redirect_url"`
	Provider    string `json:"provider"`
}

type Handler struct{ db *gorm.DB }

func NewHandler(db *gorm.DB) *Handler { return &Handler{db: db} }

/* =============================== MOCK FLOW ============================== */

// @Summary Create checkout (mock)
// @Description Client selects a quote → create or reuse an initiated payment (mock provider)
// @Tags payments
// @Security BearerAuth
// @Produce json
// @Param quoteID path string true "quote id (uuid)"
// @Success 201 {object} CheckoutResponse
// @Router /checkout/{quoteID} [post]
func (h *Handler) CreateCheckoutMock(c *fiber.Ctx) error {
	quoteID := c.Params("quoteID")
	if quoteID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing quote id")
	}
	qid, err := uuid.Parse(quoteID)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid quote id")
	}

	// Load quote & case
	var q models.Quote
	if err := h.db.First(&q, "id = ?", qid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	var cs models.Case
	if err := h.db.First(&cs, "id = ?", q.CaseID).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// AuthZ & state
	clientID := auth.MustUserID(c)
	if cs.ClientID.String() != clientID {
		return fiber.ErrForbidden
	}
	if cs.Status != models.CaseOpen {
		return fiber.NewError(fiber.StatusConflict, "case is not open")
	}

	// Idempotent by quote
	var pay models.Payment
	if err := h.db.Where("quote_id = ?", q.ID).First(&pay).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrInternalServerError
		}
		pay = models.Payment{
			CaseID:      cs.ID,
			QuoteID:     q.ID,
			ClientID:    cs.ClientID,
			AmountCents: q.AmountCents,
			Status:      models.PayInitiated,
			CreatedAt:   time.Now(),
		}
		if err := h.db.Create(&pay).Error; err != nil {
			return fiber.ErrInternalServerError
		}
	} else if pay.Status == models.PayPaid {
		return fiber.NewError(fiber.StatusConflict, "quote already paid")
	}

	resp := CheckoutResponse{
		PaymentID:   pay.ID.String(),
		RedirectURL: "http://localhost:3000/mock/checkout?pid=" + pay.ID.String(),
		Provider:    "mock",
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

/* ============================== STRIPE FLOW ============================= */

// @Summary Create checkout (Stripe)
// @Description Client selects a quote → create a Stripe Checkout Session (server-side) using amount from DB
// @Tags payments
// @Security BearerAuth
// @Produce json
// @Param quoteID path string true "quote id (uuid)"
// @Success 201 {object} CheckoutResponse
// @Router /checkout/{quoteID} [post]
func (h *Handler) CreateCheckout(c *fiber.Ctx) error {
	// Use mock provider if configured
	if os.Getenv("PAYMENT_PROVIDER") == "mock" {
		return h.CreateCheckoutMock(c)
	}

	stripe.Key = os.Getenv("STRIPE_SECRET")
	currency := os.Getenv("STRIPE_CURRENCY")
	if currency == "" {
		currency = "usd"
	}

	clientID := auth.MustUserID(c)
	qid, err := uuid.Parse(c.Params("quoteID"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid quote id")
	}

	// Load quote & case
	var q models.Quote
	if err := h.db.First(&q, "id = ?", qid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	var cs models.Case
	if err := h.db.First(&cs, "id = ?", q.CaseID).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// AuthZ & state checks
	if cs.ClientID.String() != clientID {
		return fiber.ErrForbidden
	}
	if cs.Status != models.CaseOpen {
		return fiber.NewError(fiber.StatusConflict, "case is not open")
	}

	// Idempotent by quote
	var pay models.Payment
	if err := h.db.Where("quote_id = ?", q.ID).First(&pay).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrInternalServerError
		}
		pay = models.Payment{
			CaseID:      cs.ID,
			QuoteID:     q.ID,
			ClientID:    cs.ClientID,
			AmountCents: q.AmountCents,
			Status:      models.PayInitiated,
			CreatedAt:   time.Now(),
		}
		if err := h.db.Create(&pay).Error; err != nil {
			return fiber.ErrInternalServerError
		}
	} else if pay.Status == models.PayPaid {
		return fiber.NewError(fiber.StatusConflict, "quote already paid")
	}

	// Success/cancel URLs
	successURL := os.Getenv("PUBLIC_BASE_URL") + "/payments/success?pid=" + pay.ID.String()
	cancelURL := os.Getenv("PUBLIC_BASE_URL") + "/payments/cancel?pid=" + pay.ID.String()

	// Create checkout session
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		ClientReferenceID: stripe.String(pay.ID.String()),
		Metadata: map[string]string{
			"payment_id":   pay.ID.String(),
			"quote_id":     q.ID.String(),
			"case_id":      cs.ID.String(),
			"client_id":    cs.ClientID.String(),
			"amount_cents": fmt.Sprintf("%d", q.AmountCents),
		},
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(currency),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String(fmt.Sprintf("Legal case #%s", cs.ID.String())),
						Description: stripe.String(fmt.Sprintf("Case engagement (%s)", q.Note)),
					},
					UnitAmount: stripe.Int64(int64(q.AmountCents)),
				},
				Quantity: stripe.Int64(1),
			},
		},
	}
	sess, err := session.New(params)
	if err != nil {
		return fiber.NewError(http.StatusBadGateway, err.Error())
	}

	// Simpan session id sebagai POINTER (hindari "")
	sid := sess.ID
	if err := h.db.Model(&models.Payment{}).
		Where("id = ?", pay.ID).
		Updates(map[string]any{
			"stripe_session_id": &sid,
		}).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	resp := CheckoutResponse{
		PaymentID:   pay.ID.String(),
		RedirectURL: sess.URL,
		Provider:    "stripe",
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

/* ============================== MOCK COMPLETE ============================== */

// @Summary Complete payment (mock)
// @Description Dev-only: finalize payment and set a single winner (atomic transaction)
// @Tags payments
// @Accept json
// @Produce json
// @Param payload body MockCompleteRequest true "Payment ID"
// @Success 200 {object} map[string]any "ok"
// @Router /payments/mock/complete [post]
func (h *Handler) MockComplete(c *fiber.Ctx) error {
	if os.Getenv("APP_ENV") != "dev" || os.Getenv("PAYMENT_PROVIDER") != "mock" {
		return fiber.ErrNotFound
	}
	if c.Get("X-Dev-Secret") == "" || c.Get("X-Dev-Secret") != os.Getenv("DEV_PAYMENT_SECRET") {
		return fiber.NewError(http.StatusUnauthorized, "missing/invalid X-Dev-Secret")
	}

	var in MockCompleteRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.ErrBadRequest
	}
	pid, err := uuid.Parse(in.PaymentID)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid payment id")
	}

	tx := h.db.Begin()

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

	if pay.AmountCents != q.AmountCents {
		tx.Rollback()
		return fiber.NewError(http.StatusConflict, "amount mismatch")
	}

	if cs.Status == models.CaseOpen {
		if err := tx.Model(&models.Quote{}).Where("id = ?", q.ID).
			Update("status", models.QuoteAccepted).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
		if err := tx.Model(&models.Quote{}).
			Where("case_id = ? AND id <> ? AND status = ?", cs.ID, q.ID, models.QuoteProposed).
			Update("status", models.QuoteRejected).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
		now := time.Now()
		if err := tx.Model(&models.Case{}).Where("id = ?", cs.ID).
			Updates(map[string]any{
				"status":             models.CaseEngaged,
				"engaged_at":         &now,
				"accepted_quote_id":  q.ID,
				"accepted_lawyer_id": q.LawyerID,
			}).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
		// Log history engaged
		utils.LogCaseHistory(c.Context(), tx, cs.ID, cs.ClientID,
			"engaged", models.CaseOpen, models.CaseEngaged, "payment completed (mock)")
	}

	if err := tx.Model(&models.Payment{}).Where("id = ?", pay.ID).
		Updates(map[string]any{
			"status": models.PayPaid,
		}).Error; err != nil {
		tx.Rollback()
		return fiber.ErrInternalServerError
	}

	if err := tx.Commit().Error; err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"ok": true})
}

/* ============================== STRIPE WEBHOOK ============================= */

// @Summary Stripe webhook endpoint
// @Description Verify signature and finalize payment (checkout.session.completed)
// @Tags payments
// @Accept json
// @Produce json
// @Success 200 {string} string "ok"
// @Router /payments/stripe/webhook [post]
func (h *Handler) StripeWebhook(c *fiber.Ctx) error {
	payload := c.Body()
	sig := c.Get("Stripe-Signature")
	secret := os.Getenv("STRIPE_WEBHOOK_SECRET")

	evt, err := webhook.ConstructEvent(payload, sig, secret)
	if err != nil {
		return fiber.NewError(http.StatusBadRequest, "signature verification failed")
	}

	switch evt.Type {
	case "checkout.session.completed":
		var s stripe.CheckoutSession
		if err := json.Unmarshal(evt.Data.Raw, &s); err != nil {
			return fiber.ErrBadRequest
		}

		// payment_id dari metadata atau client_reference_id
		pidStr := ""
		if s.Metadata != nil && s.Metadata["payment_id"] != "" {
			pidStr = s.Metadata["payment_id"]
		} else if s.ClientReferenceID != "" {
			pidStr = s.ClientReferenceID
		}
		if pidStr == "" {
			return fiber.NewError(http.StatusBadRequest, "missing payment_id")
		}
		pid, err := uuid.Parse(pidStr)
		if err != nil {
			return fiber.NewError(http.StatusBadRequest, "invalid payment_id")
		}

		// --- OPTION 2: fetch PaymentIntent (expanded) to get receipt number, BEFORE DB TX
		receiptNumber := ""
		if s.PaymentIntent != nil && s.PaymentIntent.ID != "" {
			if stripe.Key == "" {
				stripe.Key = os.Getenv("STRIPE_SECRET")
			}
			if stripe.Key != "" {
				// Expand latest_charge agar kita bisa baca receipt_number tanpa menyentuh Charges
				pi, piErr := paymentintent.Get(
					s.PaymentIntent.ID,
					&stripe.PaymentIntentParams{
						Expand: []*string{
							stripe.String("latest_charge"),
						},
					},
				)
				if piErr == nil && pi != nil && pi.LatestCharge != nil {
					// latest_charge sudah menjadi *stripe.Charge karena di-expand
					receiptNumber = pi.LatestCharge.ReceiptNumber
					// (Anda juga bisa ambil pi.LatestCharge.ReceiptURL bila ingin di-log)
				}
			}
		}

		tx := h.db.Begin()

		var pay models.Payment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&pay, "id = ?", pid).Error; err != nil {
			tx.Rollback()
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fiber.ErrNotFound
			}
			return fiber.ErrInternalServerError
		}
		// Idempotent
		if pay.Status == models.PayPaid {
			tx.Rollback()
			return c.SendStatus(http.StatusOK)
		}

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

		if pay.AmountCents != q.AmountCents {
			tx.Rollback()
			return fiber.NewError(http.StatusConflict, "amount mismatch")
		}

		if cs.Status == models.CaseOpen {
			if err := tx.Model(&models.Quote{}).Where("id = ?", q.ID).
				Update("status", models.QuoteAccepted).Error; err != nil {
				tx.Rollback()
				return fiber.ErrInternalServerError
			}
			if err := tx.Model(&models.Quote{}).
				Where("case_id = ? AND id <> ? AND status = ?", cs.ID, q.ID, models.QuoteProposed).
				Update("status", models.QuoteRejected).Error; err != nil {
				tx.Rollback()
				return fiber.ErrInternalServerError
			}
			now := time.Now()
			if err := tx.Model(&models.Case{}).Where("id = ?", cs.ID).
				Updates(map[string]any{
					"status":             models.CaseEngaged,
					"engaged_at":         &now,
					"accepted_quote_id":  q.ID,
					"accepted_lawyer_id": q.LawyerID,
				}).Error; err != nil {
				tx.Rollback()
				return fiber.ErrInternalServerError
			}
			// Bangun reason dengan receipt number (jika tersedia)
			reason := "payment completed (stripe)"
			if receiptNumber != "" {
				reason = fmt.Sprintf("payment completed (stripe: %s)", receiptNumber)
			}
			utils.LogCaseHistory(c.Context(), tx, cs.ID, cs.ClientID, "engaged",
				models.CaseOpen, models.CaseEngaged, reason)
		}

		// Simpan payment_intent sebagai POINTER bila tersedia
		if s.PaymentIntent != nil && s.PaymentIntent.ID != "" {
			pi := s.PaymentIntent.ID
			if err := tx.Model(&models.Payment{}).Where("id = ?", pay.ID).
				Updates(map[string]any{
					"status":                models.PayPaid,
					"stripe_payment_intent": &pi,
				}).Error; err != nil {
				tx.Rollback()
				return fiber.ErrInternalServerError
			}
		} else {
			if err := tx.Model(&models.Payment{}).Where("id = ?", pay.ID).
				Updates(map[string]any{
					"status": models.PayPaid,
				}).Error; err != nil {
				tx.Rollback()
				return fiber.ErrInternalServerError
			}
		}

		if err := tx.Commit().Error; err != nil {
			return fiber.ErrInternalServerError
		}
		return c.SendStatus(http.StatusOK)

	default:
		return c.SendStatus(http.StatusOK)
	}
}
