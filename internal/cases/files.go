package cases

import (
	"errors"
	"mime"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

// Upload Case Files godoc
// @Summary      Upload multiple case files (PDF/PNG)
// @Description  Client (owner) uploads up to 10 files to Supabase Storage
// @Tags         files
// @Security     BearerAuth
// @Accept       multipart/form-data
// @Produce      json
// @Param        id     path      string   true  "case id (uuid)"
// @Param        files  formData  []file   true  "PDF/PNG (max 10)"
// @Success      201    {array}   map[string]any  "id, key, name, size"
// @Failure      400    {object}  models.ErrorResponse
// @Failure      403    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /cases/{id}/files [post]
func (h *Handler) UploadFile(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	caseID := c.Params("id")

	// Pastikan case milik client
	var cs models.Case
	if err := h.db.Where("id = ? AND client_id = ?", caseID, clientID).First(&cs).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrForbidden
		}
		return fiber.ErrInternalServerError
	}

	form, err := c.MultipartForm()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "multipart form required; use files[]")
	}
	// Swagger UI biasanya kirim dengan key "files" walau kita tulis files[]
	files := form.File["files[]"]
	if len(files) == 0 {
		files = form.File["files"]
	}
	if len(files) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "files are required (use key: files[])")
	}
	if len(files) > 10 {
		return fiber.NewError(fiber.StatusBadRequest, "max 10 files allowed")
	}

	results := make([]fiber.Map, 0, len(files))

	for _, fh := range files {
		res := fiber.Map{
			"name": fh.Filename,
			"size": fh.Size,
		}

		// ---- Validasi per-file
		if fh.Size <= 0 {
			res["error"] = "empty file"
			results = append(results, res)
			continue
		}
		if fh.Size > 10*1024*1024 {
			res["error"] = "max 10MB per file"
			results = append(results, res)
			continue
		}

		ct := fh.Header.Get("Content-Type")
		if ct == "" {
			ct = mime.TypeByExtension(filepath.Ext(fh.Filename))
		}
		switch ct {
		case "application/pdf", "image/png":
			// ok
		default:
			res["error"] = "only PDF or PNG are allowed"
			results = append(results, res)
			continue
		}

		f, err := fh.Open()
		if err != nil {
			res["error"] = "open failed"
			results = append(results, res)
			continue
		}
		defer f.Close()

		// Pakai nama unik agar tidak tabrakan
		key := h.sb.MakeObjectKey(caseID, fh.Filename)

		if err := h.sb.Upload(key, f, ct, fh.Size); err != nil {
			res["error"] = "upload failed"
			results = append(results, res)
			continue
		}

		rec := models.CaseFile{
			CaseID:       cs.ID,
			Key:          key,
			Mime:         ct,
			Size:         int(fh.Size),
			OriginalName: fh.Filename,
		}
		if err := h.db.Create(&rec).Error; err != nil {
			res["error"] = "database error"
			results = append(results, res)
			continue
		}

		res["id"] = rec.ID
		res["key"] = rec.Key
		results = append(results, res)
	}

	// 201 walau ada sebagian gagal; client bisa cek field "error" per item
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"results": results})
}

// Signed Download URL godoc
// @Summary      Get signed URL
// @Description  Client owner or the accepted lawyer obtains a short-lived signed URL
// @Tags         files
// @Security     BearerAuth
// @Produce      json
// @Param        fileID  path string true "file id (uuid)"
// @Success      200  {object}  map[string]any  "url, expires_in, now"
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /files/{fileID}/signed-url [get]
func (h *Handler) SignedDownloadURL(c *fiber.Ctx) error {
	userID := auth.MustUserID(c)
	role := auth.MustRole(c)
	fileID := c.Params("fileID")

	var cf models.CaseFile
	if err := h.db.Preload("Case").First(&cf, "id = ?", fileID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	allowed := false
	// Owner (client)
	if role == string(models.RoleClient) && cf.Case.ClientID.String() == userID {
		allowed = true
	}
	// Accepted lawyer on an engaged case
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

	url, err := h.sb.SignedURL(cf.Key, 60) // seconds
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"url": url, "expires_in": 60, "now": time.Now().UTC()})
}
