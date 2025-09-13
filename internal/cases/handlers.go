package cases

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/internal/storage"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
	"github.com/aldoetobex/legal-mp-backend/pkg/sanitize"
	"github.com/aldoetobex/legal-mp-backend/pkg/validation"
)

// ===== DTOs =====

type CreateCaseRequest struct {
	Title       string `json:"title" validate:"required,max=120"`
	Category    string `json:"category" validate:"required,max=40"`
	Description string `json:"description" validate:"max=2000"`
}

type CaseListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Category  string `json:"category"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	Quotes    int64  `json:"quotes"`
}

type PageCases struct {
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Total    int64          `json:"total"`
	Pages    int            `json:"pages"`
	Items    []CaseListItem `json:"items"`
}

type Handler struct {
	db *gorm.DB
	sb *storage.Supabase
}

func NewHandler(db *gorm.DB, sb *storage.Supabase) *Handler {
	return &Handler{db: db, sb: sb}
}

// Create Case godoc
// @Summary      Create case
// @Description  Client creates a new case
// @Tags         cases
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        payload  body  CreateCaseRequest  true  "Case payload"
// @Success      201  {object}  map[string]string  "id"
// @Failure      400  {object}  models.ValidationErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /cases [post]
func (h *Handler) Create(c *fiber.Ctx) error {
	var in CreateCaseRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json")
	}
	// Validation (Laravel-style response)
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	clientUUID, _ := uuid.Parse(auth.MustUserID(c))
	cs := models.Case{
		ClientID:    clientUUID,
		Title:       strings.TrimSpace(in.Title),
		Category:    strings.TrimSpace(in.Category),
		Description: strings.TrimSpace(in.Description),
		Status:      models.CaseOpen,
	}
	if err := h.db.Create(&cs).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": cs.ID})
}

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

type caseWithCounts struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Quotes    int64     `json:"quotes"`
}

// List My Cases godoc
// @Summary      List my cases
// @Description  Client lists their own cases (paginated)
// @Tags         cases
// @Security     BearerAuth
// @Produce      json
// @Param        page      query int false "page"
// @Param        pageSize  query int false "pageSize"
// @Success      200  {object}  PageCases
// @Failure      401  {object}  models.ErrorResponse
// @Router       /cases/mine [get]
func (h *Handler) ListMine(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	page, size := parsePage(c)

	// Total count
	var total int64
	if err := h.db.Model(&models.Case{}).
		Where("client_id = ?", clientID).
		Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// Data + quotes count
	rows := make([]caseWithCounts, 0, size)
	if err := h.db.
		Table("cases").
		Select(`cases.id, cases.title, cases.category, cases.status, cases.created_at,
          COUNT(quotes.id) AS quotes`).
		Joins("LEFT JOIN quotes ON quotes.case_id = cases.id").
		Where("cases.client_id = ?", clientID).
		Group("cases.id").
		Order("cases.created_at DESC").
		Offset((page - 1) * size).Limit(size).
		Scan(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	if rows == nil {
		rows = []caseWithCounts{}
	}

	return c.JSON(fiber.Map{
		"page": page, "pageSize": size, "total": total,
		"pages": int(math.Ceil(float64(total) / float64(size))),
		"items": rows, // selalu [] saat kosong
	})
}

// GetDetail godoc
// @Summary      Case detail (owner or accepted lawyer)
// @Description  Client owner atau lawyer yang diterima (engaged) dapat melihat detail & files
// @Tags         cases
// @Security     BearerAuth
// @Produce      json
// @Param        id   path string true "case id (uuid)"
// @Success      200  {object}  models.Case
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /cases/{id} [get]
func (h *Handler) GetDetail(c *fiber.Ctx) error {
	id := c.Params("id")
	userID := auth.MustUserID(c)
	role, _ := c.Locals("role").(string) // di-set oleh middleware auth

	// Ambil case + relasi
	var cs models.Case
	if err := h.db.
		Preload("Files", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC") }).
		Preload("Quotes", func(db *gorm.DB) *gorm.DB { return db.Order("created_at DESC") }).
		First(&cs, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	// Authorization:
	switch role {
	case string(models.RoleClient):
		// hanya owner
		if cs.ClientID.String() != userID {
			return fiber.ErrForbidden
		}
	case string(models.RoleLawyer):
		// hanya lawyer yang diterima saat status engaged
		if cs.Status != models.CaseEngaged || cs.AcceptedLawyerID.String() != userID {
			return fiber.ErrForbidden
		}
		// Opsional: batasi quotes yang dikirim ke FE agar tidak membocorkan kompetitor
		// kirim hanya quote yang diterima (miliknya sendiri)
		if cs.AcceptedQuoteID != uuid.Nil {
			var q models.Quote
			if err := h.db.First(&q, "id = ?", cs.AcceptedQuoteID).Error; err == nil {
				cs.Quotes = []models.Quote{q}
			} else {
				cs.Quotes = []models.Quote{}
			}
		} else {
			cs.Quotes = []models.Quote{}
		}
	default:
		return fiber.ErrForbidden
	}

	// Normalisasi slice agar tidak null
	if cs.Files == nil {
		cs.Files = []models.CaseFile{}
	}
	if cs.Quotes == nil {
		cs.Quotes = []models.Quote{}
	}

	return c.JSON(cs)
}

// ====== Marketplace (anonymized) ======

// DTO khusus marketplace (supaya tidak bentrok dengan PageCases milik owner)
type MarketCaseItem struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	Category   string    `json:"category"`
	CreatedAt  time.Time `json:"created_at"`
	Preview    string    `json:"preview"`
	HasMyQuote bool      `json:"has_my_quote"` // FE bisa dipakai untuk disable tombol submit
}

type PageMarketCases struct {
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
	Total    int64            `json:"total"`
	Pages    int              `json:"pages"`
	Items    []MarketCaseItem `json:"items"`
}

// Marketplace godoc
// @Summary      Marketplace (anonymized)
// @Description  Lawyer browses OPEN cases (server-side filters & pagination; no client identity)
// @Tags         marketplace
// @Security     BearerAuth
// @Produce      json
// @Param        page          query int    false "page"
// @Param        pageSize      query int    false "pageSize"
// @Param        category      query string false "category"
// @Param        created_since query string false "YYYY-MM-DD (Asia/Singapore)"
// @Success      200  {object}  PageMarketCases
// @Failure      401  {object}  models.ErrorResponse
// @Router       /marketplace [get]
func (h *Handler) Marketplace(c *fiber.Ctx) error {
	lawyerID := auth.MustUserID(c) // dipakai untuk HasMyQuote
	page, size := parsePage(c)
	category := strings.TrimSpace(c.Query("category"))
	createdSince := c.Query("created_since") // ISO date (YYYY-MM-DD)

	var since *time.Time
	if createdSince != "" {
		if t, err := time.Parse("2006-01-02", createdSince); err == nil {
			loc, _ := time.LoadLocation("Asia/Singapore")
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			since = &t
		}
	}

	dbq := h.db.Model(&models.Case{}).Where("status = ?", models.CaseOpen)
	if category != "" {
		dbq = dbq.Where("category = ?", category)
	}
	if since != nil {
		dbq = dbq.Where("created_at >= ?", *since)
	}

	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	var list []models.Case
	if err := dbq.Order("created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&list).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// Ambil semua case_id yang sudah pernah di-quote oleh lawyer ini,
	// dibatasi hanya pada case yang tampil di halaman ini (IN (?)) -> efisien & mencegah N+1.
	caseIDs := make([]uuid.UUID, 0, len(list))
	for _, cs := range list {
		caseIDs = append(caseIDs, cs.ID)
	}

	quotedMap := map[uuid.UUID]bool{}
	if len(caseIDs) > 0 {
		var quotedIDs []uuid.UUID
		if err := h.db.
			Model(&models.Quote{}).
			Where("lawyer_id = ? AND case_id IN ?", lawyerID, caseIDs).
			Pluck("DISTINCT case_id", &quotedIDs).Error; err != nil {
			return fiber.ErrInternalServerError
		}
		for _, qid := range quotedIDs {
			quotedMap[qid] = true
		}
	}

	items := make([]MarketCaseItem, 0, len(list))
	for _, cs := range list {
		preview := sanitize.Summary(sanitize.RedactPII(cs.Description), 240)

		items = append(items, MarketCaseItem{
			ID:         cs.ID,
			Title:      cs.Title,
			Category:   cs.Category,
			CreatedAt:  cs.CreatedAt,
			Preview:    preview,
			HasMyQuote: quotedMap[cs.ID],
		})
	}

	// Normalisasi
	if items == nil {
		items = []MarketCaseItem{}
	}

	return c.JSON(PageMarketCases{
		Page:     page,
		PageSize: size,
		Total:    total,
		Pages:    int(math.Ceil(float64(total) / float64(size))),
		Items:    items,
	})
}
