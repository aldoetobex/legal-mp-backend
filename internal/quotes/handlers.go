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

/* =============================== DTOs ==================================== */

type UpsertQuoteRequest struct {
	CaseID      string `json:"case_id" validate:"required,uuid4"`
	AmountCents int    `json:"amount_cents" validate:"required,min=1,max=100000000"` // min S$10, max S$1,000,000
	Days        int    `json:"days" validate:"required,min=1,max=365"`
	Note        string `json:"note" validate:"omitempty,max=500"`
}

// Returned to the lawyer in /quotes/mine (includes case metadata for FE display)
type MyQuoteItem struct {
	ID           string `json:"id"`
	CaseID       string `json:"case_id"`
	CaseTitle    string `json:"case_title"`    // from cases.title
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

/* ============================== Handler =================================== */

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler { return &Handler{db: db} }

/* ============================== Helpers =================================== */

// parsePage reads ?page and ?pageSize with sane bounds (1..50)
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

/* ============================ Upsert Quote ================================ */

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

	// Parse & validate payload
	var in UpsertQuoteRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json")
	}
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	caseID, err := uuid.Parse(strings.TrimSpace(in.CaseID))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid case_id")
	}
	lawyerID := uuid.MustParse(lawyerIDStr)

	// Quick pre-check: case must exist and be OPEN (no transaction yet)
	var cs models.Case
	if err := h.db.First(&cs, "id = ?", caseID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	if cs.Status != models.CaseOpen {
		return fiber.NewError(fiber.StatusConflict, "case is not open")
	}

	// Start TX and lock the case row to avoid races against accept/close
	tx := h.db.Begin()
	if tx.Error != nil {
		return fiber.ErrInternalServerError
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	// Re-fetch & lock case defensively
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&cs, "id = ?", caseID).Error; err != nil {
		_ = tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	if cs.Status != models.CaseOpen {
		_ = tx.Rollback()
		return fiber.NewError(fiber.StatusConflict, "case is not open")
	}

	// Enforce single active quote per (case_id, lawyer_id).
	// Create new or update only if the current one is still PROPOSED.
	var q models.Quote
	err = tx.Where("case_id = ? AND lawyer_id = ?", caseID, lawyerID).First(&q).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Insert a new proposed quote
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
			_ = tx.Rollback()
			return fiber.ErrInternalServerError
		}

	case err == nil:
		// Allow updates only when the quote is still PROPOSED
		if q.Status != models.QuoteProposed {
			_ = tx.Rollback()
			return fiber.NewError(fiber.StatusConflict, "quote is immutable (already accepted/rejected)")
		}
		// Extra safety: ensure ownership
		if q.LawyerID != lawyerID {
			_ = tx.Rollback()
			return fiber.ErrForbidden
		}
		// Apply updates
		if err := tx.Model(&q).Updates(map[string]any{
			"amount_cents": in.AmountCents,
			"days":         in.Days,
			"note":         strings.TrimSpace(in.Note),
			"updated_at":   time.Now(),
		}).Error; err != nil {
			_ = tx.Rollback()
			return fiber.ErrInternalServerError
		}

	default:
		_ = tx.Rollback()
		return fiber.ErrInternalServerError
	}

	if err := tx.Commit().Error; err != nil {
		return fiber.ErrInternalServerError
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":           q.ID,
		"status":       q.Status,
		"amount_cents": q.AmountCents,
		"days":         q.Days,
		"note":         strings.TrimSpace(q.Note),
	})
}

/* ============================= List Mine ================================== */

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

	// Optional status filter
	if status != "" {
		switch status {
		case string(models.QuoteProposed), string(models.QuoteAccepted), string(models.QuoteRejected):
			base = base.Where("quotes.status = ?", status)
		default:
			return fiber.NewError(fiber.StatusBadRequest, "invalid status filter")
		}
	}

	// Count before pagination
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// Select quotes + case metadata (title/category/status)
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

	return c.JSON(fiber.Map{
		"page":     page,
		"pageSize": size,
		"total":    total,
		"pages":    int(math.Ceil(float64(total) / float64(size))),
		"items":    rows,
	})
}

/* ===================== Client: Quotes by Case (owner) ===================== */

// For owner view: list all quotes under a case
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

	// Load case ownership + status + accepted quote
	var cs struct {
		ID              uuid.UUID
		ClientID        uuid.UUID
		Status          models.CaseStatus
		AcceptedQuoteID uuid.UUID
	}
	if err := h.db.
		Model(&models.Case{}).
		Select("id, client_id, status, accepted_quote_id").
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

	// Fetch quotes for this case (all statuses)
	q := h.db.Model(&models.Quote{}).Where("case_id = ?", cs.ID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	rows := make([]caseQuoteItem, 0, size)
	if err := q.
		Order("created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Scan(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// Redaction rules for the owner:
	// - OPEN or CANCELLED → redact all notes
	// - ENGAGED or CLOSED → show accepted note in full; redact the rest
	switch cs.Status {
	case models.CaseOpen, models.CaseCancelled:
		for i := range rows {
			rows[i].Note = sanitize.RedactPII(rows[i].Note)
		}
	case models.CaseEngaged, models.CaseClosed:
		for i := range rows {
			if rows[i].ID != cs.AcceptedQuoteID {
				rows[i].Note = sanitize.RedactPII(rows[i].Note)
			}
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
