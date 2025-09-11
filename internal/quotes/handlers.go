package quotes

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler { return &Handler{db: db} }

func parsePage(c *fiber.Ctx) (page, size int) {
	page, _ = strconv.Atoi(c.Query("page", "1"))
	size, _ = strconv.Atoi(c.Query("pageSize", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 10
	}
	return
}

// =====================================
// POST /api/quotes (lawyer) — Upsert
// =====================================

type upsertReq struct {
	CaseID      string `json:"case_id"`
	AmountCents int    `json:"amount_cents"`
	Days        int    `json:"days"`
	Note        string `json:"note"`
}

func (h *Handler) Upsert(c *fiber.Ctx) error {
	lawyerIDStr := auth.MustUserID(c)

	var in upsertReq
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json")
	}
	in.CaseID = strings.TrimSpace(in.CaseID)
	if in.CaseID == "" || in.AmountCents <= 0 || in.Days <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "case_id, amount_cents>0, days>0 required")
	}

	caseID, err := uuid.Parse(in.CaseID)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid case_id")
	}
	lawyerID, _ := uuid.Parse(lawyerIDStr)

	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1) Pastikan case masih OPEN (lock untuk menghindari race)
	var cs models.Case
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&cs, "id = ?", caseID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			tx.Rollback()
			return fiber.ErrNotFound
		}
		tx.Rollback()
		return fiber.ErrInternalServerError
	}
	if cs.Status != models.CaseOpen {
		tx.Rollback()
		return fiber.NewError(fiber.StatusConflict, "case is not open")
	}

	// 2) Cari quote existing untuk (case_id, lawyer_id)
	var q models.Quote
	err = tx.Where("case_id = ? AND lawyer_id = ?", caseID, lawyerID).First(&q).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Insert baru
		q = models.Quote{
			CaseID:      caseID,
			LawyerID:    lawyerID,
			AmountCents: in.AmountCents,
			Days:        in.Days,
			Note:        strings.TrimSpace(in.Note),
			Status:      models.QuoteProposed,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := tx.Create(&q).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
		// (Opsional) Jika Anda telah menambah kolom denormalisasi quotes_count di models.Case:
		// tx.Model(&models.Case{}).Where("id = ?", caseID).
		//     UpdateColumn("quotes_count", gorm.Expr("quotes_count + 1"))
	case err == nil:
		// Update existing hanya jika masih PROPOSED
		if q.Status != models.QuoteProposed {
			tx.Rollback()
			return fiber.NewError(fiber.StatusConflict, "quote is immutable (already accepted/rejected)")
		}
		if err := tx.Model(&q).Updates(map[string]any{
			"amount_cents": in.AmountCents,
			"days":         in.Days,
			"note":         strings.TrimSpace(in.Note),
			"updated_at":   time.Now(),
		}).Error; err != nil {
			tx.Rollback()
			return fiber.ErrInternalServerError
		}
	default:
		tx.Rollback()
		return fiber.ErrInternalServerError
	}

	if err := tx.Commit().Error; err != nil {
		return fiber.ErrInternalServerError
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id": q.ID, "status": q.Status, "amount_cents": q.AmountCents, "days": q.Days, "note": q.Note,
	})
}

// =====================================================
// GET /api/quotes/mine?page=&pageSize=&status= (lawyer)
// =====================================================

type myQuoteItem struct {
	ID          uuid.UUID          `json:"id"`
	CaseID      uuid.UUID          `json:"case_id"`
	AmountCents int                `json:"amount_cents"`
	Days        int                `json:"days"`
	Note        string             `json:"note"`
	Status      models.QuoteStatus `json:"status"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

func (h *Handler) ListMine(c *fiber.Ctx) error {
	lawyerID := auth.MustUserID(c)
	page, size := parsePage(c)
	status := strings.TrimSpace(c.Query("status"))

	q := h.db.Model(&models.Quote{}).Where("lawyer_id = ?", lawyerID)
	if status != "" {
		switch status {
		case string(models.QuoteProposed), string(models.QuoteAccepted), string(models.QuoteRejected):
			q = q.Where("status = ?", status)
		default:
			return fiber.NewError(fiber.StatusBadRequest, "invalid status filter")
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	var rows []myQuoteItem
	if err := q.Order("created_at DESC").
		Offset((page - 1) * size).Limit(size).
		Scan(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	return c.JSON(fiber.Map{
		"page": page, "pageSize": size, "total": total,
		"pages": int(math.Ceil(float64(total) / float64(size))),
		"items": rows,
	})
}

// ============================================================
// GET /api/cases/:id/quotes  (client owner melihat semua quote)
// ============================================================

type caseQuoteItem struct {
	ID          uuid.UUID `json:"id"`
	LawyerID    uuid.UUID `json:"lawyer_id"`
	AmountCents int       `json:"amount_cents"`
	Days        int       `json:"days"`
	Note        string    `json:"note"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (h *Handler) ListByCaseForOwner(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	caseID := c.Params("id")
	if _, err := uuid.Parse(caseID); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid case id")
	}

	// Pastikan case ini milik client
	var cnt int64
	if err := h.db.Model(&models.Case{}).
		Where("id = ? AND client_id = ?", caseID, clientID).
		Count(&cnt).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	if cnt == 0 {
		return fiber.ErrForbidden
	}

	page, size := parsePage(c)
	q := h.db.Model(&models.Quote{}).Where("case_id = ?", caseID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	var rows []caseQuoteItem
	if err := q.Order("created_at DESC").
		Offset((page - 1) * size).Limit(size).
		Scan(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	return c.JSON(fiber.Map{
		"page": page, "pageSize": size, "total": total,
		"pages": int(math.Ceil(float64(total) / float64(size))),
		"items": rows,
	})
}
