package cases

import (
	"math"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/internal/storage"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

type Handler struct {
	db *gorm.DB
	sb *storage.Supabase
}

func NewHandler(db *gorm.DB, sb *storage.Supabase) *Handler {
	return &Handler{db: db, sb: sb}
}

type createReq struct {
	Title       string `json:"title"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

func (h *Handler) Create(c *fiber.Ctx) error {
	var in createReq
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json")
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Category = strings.TrimSpace(in.Category)
	if in.Title == "" || in.Category == "" {
		return fiber.NewError(fiber.StatusBadRequest, "title & category required")
	}

	clientID, _ := uuid.Parse(auth.MustUserID(c))
	cs := models.Case{
		ClientID:    clientID,
		Title:       in.Title,
		Category:    in.Category,
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

func (h *Handler) ListMine(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	page, size := parsePage(c)

	// Hitung total
	var total int64
	if err := h.db.Model(&models.Case{}).
		Where("client_id = ?", clientID).
		Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	// Ambil data + jumlah quote
	var rows []caseWithCounts
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

	return c.JSON(fiber.Map{
		"page": page, "pageSize": size, "total": total,
		"pages": int(math.Ceil(float64(total) / float64(size))),
		"items": rows,
	})
}

func (h *Handler) GetDetailOwner(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	id := c.Params("id")

	var cs models.Case
	if err := h.db.Where("id = ? AND client_id = ?", id, clientID).
		Preload("Files").Preload("Quotes").First(&cs).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	return c.JSON(cs)
}

// ====== Marketplace ======

var reEmail = regexp.MustCompile(`([A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})`)
var rePhone = regexp.MustCompile(`(\+?\d[\d\s\-$begin:math:text$$end:math:text$]{6,}\d)`)

type marketItem struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"created_at"`
	Preview   string    `json:"preview"`
}

func redact(s string) string {
	// Redaksi email dan nomor telp
	s = reEmail.ReplaceAllString(s, "[redacted]")
	// Hindari false-positive email parser
	if _, err := mail.ParseAddress(s); err == nil {
		s = "[redacted]"
	}
	return rePhone.ReplaceAllString(s, "[redacted]")
}

func (h *Handler) Marketplace(c *fiber.Ctx) error {
	page, size := parsePage(c)
	category := strings.TrimSpace(c.Query("category"))
	createdSince := c.Query("created_since") // ISO date
	var since *time.Time
	if createdSince != "" {
		if t, err := time.Parse("2006-01-02", createdSince); err == nil {
			// Interpretasi Asia/Singapore (UTC+8)
			loc, _ := time.LoadLocation("Asia/Singapore")
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			since = &t
		}
	}

	dbq := h.db.Model(&models.Case{}).
		Where("status = ?", models.CaseOpen)
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
		Offset((page - 1) * size).Limit(size).
		Find(&list).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	items := make([]marketItem, 0, len(list))
	for _, cs := range list {
		preview := cs.Description
		if len(preview) > 240 {
			preview = preview[:240] + "..."
		}
		items = append(items, marketItem{
			ID: cs.ID, Title: cs.Title, Category: cs.Category, CreatedAt: cs.CreatedAt,
			Preview: redact(preview),
		})
	}

	return c.JSON(fiber.Map{
		"page": page, "pageSize": size, "total": total, "pages": int(math.Ceil(float64(total) / float64(size))),
		"items": items,
	})
}
