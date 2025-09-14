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
	"github.com/aldoetobex/legal-mp-backend/pkg/sanitize"
	"github.com/aldoetobex/legal-mp-backend/pkg/validation"
)

// ===== DTOs =====

type UpsertQuoteRequest struct {
	CaseID      string `json:"case_id" validate:"required,uuid4"`
	AmountCents int    `json:"amount_cents" validate:"required,min=1,max=100000000"` // min S$10, max S$1,000,000
	Days        int    `json:"days" validate:"required,min=1,max=365"`
	Note        string `json:"note" validate:"omitempty,max=500"`
}

// Tambah metadata kasus agar FE bisa tampilkan nama dengan benar
type MyQuoteItem struct {
	ID           string `json:"id"`
	CaseID       string `json:"case_id"`
	CaseTitle    string `json:"case_title"`    // <—
	CaseCategory string `json:"case_category"` // optional
	CaseStatus   string `json:"case_status"`   // optional
	AmountCents  int    `json:"amount_cents"`
	Days         int    `json:"days"`
	Note         string `json:"note"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

type PageMyQuotes struct {
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
	Total    int64         `json:"total"`
	Pages    int           `json:"pages"`
	Items    []MyQuoteItem `json:"items"`
}

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

// Upsert Quote godoc
// @Summary      Submit or update a quote (1 active per case per lawyer)
// @Description  Lawyer creates or updates a quote while the case is still OPEN
// @Tags         quotes
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        payload  body  UpsertQuoteRequest  true  "Quote upsert payload"
// @Success      201  {object}  map[string]any  "id, status, amount_cents, days, note"
// @Failure      400  {object}  models.ValidationErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      409  {object}  models.ErrorResponse  "immutable or case not open"
// @Failure      500  {object}  models.ErrorResponse
// @Router       /quotes [post]
func (h *Handler) Upsert(c *fiber.Ctx) error {
	lawyerIDStr := auth.MustUserID(c)

	var in UpsertQuoteRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json")
	}
	// Validation (Laravel-style)
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	caseID, err := uuid.Parse(strings.TrimSpace(in.CaseID))
	if err != nil {
		// Defensive: should have been caught by validator uuid4
		return fiber.NewError(fiber.StatusBadRequest, "invalid case_id")
	}
	lawyerID, _ := uuid.Parse(lawyerIDStr)

	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1) Ensure case is still OPEN (row lock to avoid race)
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

	// 2) Find existing quote for (case_id, lawyer_id)
	var q models.Quote
	err = tx.Where("case_id = ? AND lawyer_id = ?", caseID, lawyerID).First(&q).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Insert new
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
	case err == nil:
		// Update only if still PROPOSED
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

// List My Quotes godoc
// @Summary      List my quotes
// @Description  Lawyer lists their quotes (filter by status, with pagination). Includes case title.
// @Tags         quotes
// @Security     BearerAuth
// @Produce      json
// @Param        page      query int    false "page"
// @Param        pageSize  query int    false "pageSize"
// @Param        status    query string false "proposed|accepted|rejected"
// @Success      200  {object}  PageMyQuotes
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /quotes/mine [get]
func (h *Handler) ListMine(c *fiber.Ctx) error {
	lawyerID := auth.MustUserID(c)
	page, size := parsePage(c)
	status := strings.TrimSpace(c.Query("status"))

	base := h.db.Table("quotes").Where("quotes.lawyer_id = ?", lawyerID)

	if status != "" {
		switch status {
		case string(models.QuoteProposed), string(models.QuoteAccepted), string(models.QuoteRejected):
			base = base.Where("quotes.status = ?", status)
		default:
			return fiber.NewError(fiber.StatusBadRequest, "invalid status filter")
		}
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	rows := make([]MyQuoteItem, 0, size)
	if err := base.
		Select(`
			quotes.id,
			quotes.case_id,
			quotes.amount_cents,
			quotes.days,
			quotes.note,
			quotes.status,
			quotes.created_at,
			cases.title    AS case_title,
			cases.category AS case_category,
			cases.status   AS case_status
		`).
		Joins("JOIN cases ON cases.id = quotes.case_id").
		Order("quotes.created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Scan(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// 🚨 Redact NOTE kalau case masih OPEN atau CANCELLED
	for i := range rows {
		if rows[i].CaseStatus == string(models.CaseOpen) ||
			rows[i].CaseStatus == string(models.CaseCancelled) {
			rows[i].Note = sanitize.RedactPII(rows[i].Note)
		}
	}

	return c.JSON(fiber.Map{
		"page":     page,
		"pageSize": size,
		"total":    total,
		"pages":    int(math.Ceil(float64(total) / float64(size))),
		"items":    rows,
	})
}

// ============================================================
// GET /api/cases/:id/quotes  (client owner views all quotes)
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

// Quotes by Case godoc
// @Summary      Quotes by case (owner)
// @Description  Client owner sees all quotes for their case (paginated)
// @Tags         quotes
// @Security     BearerAuth
// @Produce      json
// @Param        id        path  string true "case id (uuid)"
// @Param        page      query int    false "page"
// @Param        pageSize  query int    false "pageSize"
// @Success      200  {object}  PageMyQuotes
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /cases/{id}/quotes [get]
func (h *Handler) ListByCaseForOwner(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	caseID := c.Params("id")
	if _, err := uuid.Parse(caseID); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid case id")
	}

	// Ambil status case + verifikasi kepemilikan
	var cs struct {
		ID       uuid.UUID
		ClientID uuid.UUID
		Status   models.CaseStatus
	}
	if err := h.db.
		Model(&models.Case{}).
		Select("id, client_id, status").
		Where("id = ?", caseID).
		First(&cs).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	if cs.ClientID.String() != clientID {
		return fiber.ErrForbidden
	}

	page, size := parsePage(c)

	// Query semua quotes untuk case ini (tetap ditampilkan semua)
	q := h.db.Model(&models.Quote{}).Where("case_id = ?", cs.ID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// Selalu inisialisasi slice agar tidak null
	rows := make([]caseQuoteItem, 0, size)
	if err := q.
		Order("created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Scan(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// *** Redact hanya NOTE saat status case masih OPEN ***
	if cs.Status == models.CaseOpen || cs.Status == models.CaseCancelled {
		for i := range rows {
			rows[i].Note = sanitize.RedactPII(rows[i].Note)
		}
	}

	return c.JSON(fiber.Map{
		"page":     page,
		"pageSize": size,
		"total":    total,
		"pages":    int(math.Ceil(float64(total) / float64(size))),
		"items":    rows, // selalu [] ketika kosong
	})
}
