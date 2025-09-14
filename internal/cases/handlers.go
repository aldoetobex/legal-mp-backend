package cases

import (
	"crypto/sha1"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
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
	"github.com/aldoetobex/legal-mp-backend/pkg/utils"
	"github.com/aldoetobex/legal-mp-backend/pkg/validation"
)

// ===== DTOs =====

type CreateCaseRequest struct {
	Title       string `json:"title" validate:"required,min=3,max=120"`
	Category    string `json:"category" validate:"required,max=40"`
	Description string `json:"description" validate:"max=2000"`
}

type ActionRequest struct {
	Comment string `json:"comment" validate:"max=500"` // optional note shown in history
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

// ====== History DTO ======
type CaseHistoryDTO struct {
	ID        uuid.UUID         `json:"id"`
	Action    string            `json:"action"`
	OldStatus models.CaseStatus `json:"old_status"`
	NewStatus models.CaseStatus `json:"new_status"`
	Reason    string            `json:"reason"`
	ActorID   uuid.UUID         `json:"actor_id"`
	CreatedAt time.Time         `json:"created_at"`
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

	// Log: created
	utils.LogCaseHistory(c.Context(), h.db, cs.ID, clientUUID, "created", "", models.CaseOpen, "case created")

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

	// Map ke DTO stabil
	items := make([]CaseListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, CaseListItem{
			ID:        r.ID.String(),
			Title:     r.Title,
			Category:  r.Category,
			Status:    r.Status,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
			Quotes:    r.Quotes,
		})
	}

	return c.JSON(fiber.Map{
		"page":     page,
		"pageSize": size,
		"total":    total,
		"pages":    int(math.Ceil(float64(total) / float64(size))),
		"items":    items, // selalu [] saat kosong
	})
}

// ====== DTO untuk counterpart & detail ======
type PublicUser struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name,omitempty"`
	Email        string    `json:"email,omitempty"`
	Jurisdiction string    `json:"jurisdiction,omitempty"`
	BarNumber    string    `json:"bar_number,omitempty"`
}

type CaseDetailResponse struct {
	models.Case
	AcceptedLawyer *PublicUser `json:"accepted_lawyer,omitempty"`
	Client         *PublicUser `json:"client,omitempty"`
}

// ambil profil publik user
func (h *Handler) fetchPublicUser(uID uuid.UUID, withLawyerFields bool) *PublicUser {
	if uID == uuid.Nil {
		return nil
	}
	var row struct {
		ID           uuid.UUID
		Name         string
		Email        string
		Jurisdiction string
		BarNumber    string
	}
	q := h.db.Model(&models.User{}).Select("id, name, email")
	if withLawyerFields {
		q = q.Select("id, name, email, jurisdiction, bar_number")
	}
	if err := q.First(&row, "id = ?", uID).Error; err != nil {
		return nil
	}
	return &PublicUser{
		ID:           row.ID,
		Name:         row.Name,
		Email:        row.Email,
		Jurisdiction: row.Jurisdiction,
		BarNumber:    row.BarNumber,
	}
}

// GetDetail godoc
// @Summary      Case detail (owner or accepted lawyer)
// @Description  Client owner atau lawyer yang diterima (engaged/closed) dapat melihat detail & files + counterpart
// @Tags         cases
// @Security     BearerAuth
// @Produce      json
// @Param        id   path string true "case id (uuid)"
// @Success      200  {object}  CaseDetailResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /cases/{id} [get]

// helper to hash filename for display
func maskFileName(original string) string {
	ext := filepath.Ext(original)
	sum := sha1.Sum([]byte(original))
	return hex.EncodeToString(sum[:]) + ext
}

func (h *Handler) GetDetail(c *fiber.Ctx) error {
	id := c.Params("id")
	userID := auth.MustUserID(c)
	role, _ := c.Locals("role").(string)

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

	if cs.Files == nil {
		cs.Files = []models.CaseFile{}
	}
	if cs.Quotes == nil {
		cs.Quotes = []models.Quote{}
	}

	// 🔒 Mask filenames in the response
	safeFiles := make([]models.CaseFile, len(cs.Files))
	for i, f := range cs.Files {
		f.OriginalName = maskFileName(f.OriginalName)
		safeFiles[i] = f
	}
	cs.Files = safeFiles

	switch role {
	case string(models.RoleClient):
		if cs.ClientID.String() != userID {
			return fiber.ErrForbidden
		}

		// - OPEN: semua note disensor
		// - ENGAGED/CLOSED: note dari acceptedQuote tampil apa adanya, sisanya disensor
		if len(cs.Quotes) > 0 {
			safeQuotes := make([]models.Quote, len(cs.Quotes))
			switch cs.Status {
			case models.CaseOpen:
				for i, q := range cs.Quotes {
					q.Note = sanitize.RedactPII(q.Note)
					safeQuotes[i] = q
				}
			case models.CaseEngaged, models.CaseClosed:
				for i, q := range cs.Quotes {
					if q.ID != cs.AcceptedQuoteID {
						q.Note = sanitize.RedactPII(q.Note) // atau kosongkan: q.Note = ""
					}
					safeQuotes[i] = q
				}
			default:
				// fallback konservatif: sensor semua
				for i, q := range cs.Quotes {
					q.Note = sanitize.RedactPII(q.Note)
					safeQuotes[i] = q
				}
			}
			cs.Quotes = safeQuotes
		}

		resp := CaseDetailResponse{Case: cs}
		if (cs.Status == models.CaseEngaged || cs.Status == models.CaseClosed) && cs.AcceptedLawyerID != uuid.Nil {
			resp.AcceptedLawyer = h.fetchPublicUser(cs.AcceptedLawyerID, true)
		}
		return c.JSON(resp)

	case string(models.RoleLawyer):
		if (cs.Status != models.CaseEngaged && cs.Status != models.CaseClosed) || cs.AcceptedLawyerID.String() != userID {
			return fiber.ErrForbidden
		}

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

		resp := CaseDetailResponse{
			Case:   cs,
			Client: h.fetchPublicUser(cs.ClientID, false),
		}
		return c.JSON(resp)

	default:
		return fiber.ErrForbidden
	}
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

func appLocation() *time.Location {
	// bisa dibaca dari env, default Asia/Singapore
	if tz := os.Getenv("APP_TZ"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if loc, err := time.LoadLocation("Asia/Singapore"); err == nil {
		return loc
	}
	// Fallback keras bila tzdb tidak ada di container
	return time.FixedZone("SGT", 8*60*60) // UTC+8
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

	// Parse created_since in Asia/Singapore, simpan sebagai UTC untuk konsistensi dengan DB
	var sinceUTC *time.Time
	if createdSince != "" {
		loc := appLocation() // <- tidak mungkin nil
		// "2006-01-02" mewakili tengah malam lokal tanggal tsb
		if localMidnight, err := time.ParseInLocation("2006-01-02", createdSince, loc); err == nil {
			u := localMidnight.UTC()
			sinceUTC = &u
		}
	}

	dbq := h.db.Model(&models.Case{}).Where("status = ?", models.CaseOpen)
	if category != "" {
		dbq = dbq.Where("category = ?", category)
	}
	if sinceUTC != nil {
		dbq = dbq.Where("created_at >= ?", *sinceUTC)
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

	// Ambil semua case_id yang ada di halaman ini
	caseIDs := make([]uuid.UUID, 0, len(list))
	for _, cs := range list {
		caseIDs = append(caseIDs, cs.ID)
	}

	// Cek mana yang sudah di-quote oleh lawyer (tanpa N+1)
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

// Cancel Case godoc
// @Summary      Cancel case
// @Description  Client cancels their own case (only if still open)
// @Tags         cases
// @Security     BearerAuth
// @Accept       json
// @Param        id       path  string         true "case id (uuid)"
// @Param        payload  body  ActionRequest  false "Optional comment"
// @Success      200  {object}  map[string]string  "status"
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      409  {object}  models.ErrorResponse
// @Router       /cases/{id}/cancel [post]
func (h *Handler) Cancel(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	id := c.Params("id")

	// Parse optional comment
	var in ActionRequest
	_ = c.BodyParser(&in)
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	var cs models.Case
	if err := h.db.First(&cs, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	if cs.ClientID.String() != clientID {
		return fiber.ErrForbidden
	}
	if cs.Status != models.CaseOpen {
		return fiber.NewError(fiber.StatusConflict, "case cannot be cancelled")
	}

	old := cs.Status
	if err := h.db.Model(&cs).Update("status", models.CaseCancelled).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	// Log history
	utils.LogCaseHistory(c.Context(), h.db, cs.ID, uuid.MustParse(clientID), "cancelled", old, models.CaseCancelled, strings.TrimSpace(in.Comment))

	return c.JSON(fiber.Map{"status": "cancelled"})
}

// Close Case godoc
// @Summary      Close case
// @Description  Client closes their own case (only if engaged)
// @Tags         cases
// @Security     BearerAuth
// @Accept       json
// @Param        id       path  string         true  "case id (uuid)"
// @Param        payload  body  ActionRequest  false "Optional comment"
// @Success      200  {object}  map[string]string  "status"
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      409  {object}  models.ErrorResponse
// @Router       /cases/{id}/close [post]
func (h *Handler) Close(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	id := c.Params("id")

	// Parse optional comment
	var in ActionRequest
	_ = c.BodyParser(&in)
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	var cs models.Case
	if err := h.db.First(&cs, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	if cs.ClientID.String() != clientID {
		return fiber.ErrForbidden
	}
	if cs.Status != models.CaseEngaged {
		return fiber.NewError(fiber.StatusConflict, "only engaged cases can be closed")
	}

	old := cs.Status
	if err := h.db.Model(&cs).Update("status", models.CaseClosed).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	// Log history
	utils.LogCaseHistory(c.Context(), h.db, cs.ID, uuid.MustParse(clientID), "closed", old, models.CaseClosed, strings.TrimSpace(in.Comment))

	return c.JSON(fiber.Map{"status": "closed"})
}

// === Shared helper ===

// List Case History godoc
// @Summary      Case history
// @Description  Riwayat perubahan case (owner & accepted lawyer)
// @Tags         cases
// @Security     BearerAuth
// @Produce      json
// @Param        id   path string true "case id (uuid)"
// @Success      200  {array}  CaseHistoryDTO
// @Failure      401  {object} models.ErrorResponse
// @Failure      403  {object} models.ErrorResponse
// @Failure      404  {object} models.ErrorResponse
// @Router       /cases/{id}/history [get]
func (h *Handler) ListHistory(c *fiber.Ctx) error {
	id := c.Params("id")
	userID := auth.MustUserID(c)
	role, _ := c.Locals("role").(string)

	var cs models.Case
	if err := h.db.Select("id, client_id, status, accepted_lawyer_id").
		First(&cs, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	// Authorization
	switch role {
	case string(models.RoleClient):
		if cs.ClientID.String() != userID {
			return fiber.ErrForbidden
		}
	case string(models.RoleLawyer):
		if cs.AcceptedLawyerID.String() != userID {
			return fiber.ErrForbidden
		}
	default:
		return fiber.ErrForbidden
	}

	var rows []models.CaseHistory
	if err := h.db.
		Where("case_id = ?", cs.ID).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	out := make([]CaseHistoryDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, CaseHistoryDTO{
			ID:        r.ID,
			Action:    r.Action,
			OldStatus: r.OldStatus,
			NewStatus: r.NewStatus,
			Reason:    r.Reason,
			ActorID:   r.ActorID,
			CreatedAt: r.CreatedAt,
		})
	}
	return c.JSON(out)
}
