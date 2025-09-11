package cases

import (
	"mime"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

func (h *Handler) UploadFile(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	caseID := c.Params("id")

	var cs models.Case
	if err := h.db.Where("id = ? AND client_id = ?", caseID, clientID).First(&cs).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fiber.ErrForbidden
		}
		return fiber.ErrInternalServerError
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "file is required")
	}
	if fh.Size <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "empty file")
	}
	if fh.Size > 10*1024*1024 {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "max 10MB")
	}

	ct := fh.Header.Get("Content-Type")
	if ct == "" {
		ct = mime.TypeByExtension(filepath.Ext(fh.Filename))
	}
	if ct != "application/pdf" && ct != "image/png" {
		return fiber.NewError(fiber.StatusBadRequest, "only PDF or PNG")
	}

	f, err := fh.Open()
	if err != nil {
		return fiber.ErrInternalServerError
	}
	defer f.Close()

	key := h.sb.MakeObjectKey(caseID, fh.Filename)
	if err := h.sb.Upload(key, f, ct, fh.Size); err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "upload failed")
	}

	rec := models.CaseFile{
		CaseID: cs.ID, Key: key, Mime: ct, Size: int(fh.Size), OriginalName: fh.Filename,
	}
	if err := h.db.Create(&rec).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": rec.ID, "key": rec.Key})
}

func (h *Handler) SignedDownloadURL(c *fiber.Ctx) error {
	userID := auth.MustUserID(c)
	role := auth.MustRole(c)
	fileID := c.Params("fileID")

	var cf models.CaseFile
	if err := h.db.Preload("Case").First(&cf, "id = ?", fileID).Error; err != nil {
		return fiber.ErrNotFound
	}

	allowed := false
	if role == string(models.RoleClient) && cf.Case.ClientID.String() == userID {
		allowed = true
	}
	if role == string(models.RoleLawyer) && cf.Case.Status == models.CaseEngaged {
		var cnt int64
		h.db.Model(&models.Quote{}).
			Where("case_id = ? AND lawyer_id = ? AND status = ?", cf.CaseID, userID, models.QuoteAccepted).
			Count(&cnt)
		if cnt > 0 {
			allowed = true
		}
	}
	if !allowed {
		return fiber.ErrForbidden
	}

	url, err := h.sb.SignedURL(cf.Key, 60)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"url": url, "expires_in": 60, "now": time.Now().UTC()})
}
