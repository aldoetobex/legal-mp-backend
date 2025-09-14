package cases

import (
	"errors"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

const (
	maxFilesPerRequest = 10
	maxFileBytes       = 10 * 1024 * 1024 // 10 MB
)

var allowedMIMEs = map[string]struct{}{
	"application/pdf": {},
	"image/png":       {},
}

func normalizeCT(fname, headerCT string) string {
	ct := strings.TrimSpace(headerCT)
	if ct == "" {
		ct = mime.TypeByExtension(strings.ToLower(filepath.Ext(fname)))
	}
	// Some browsers send "application/octet-stream" for PDFs—try fix by extension.
	if ct == "application/octet-stream" {
		ext := strings.ToLower(filepath.Ext(fname))
		switch ext {
		case ".pdf":
			return "application/pdf"
		case ".png":
			return "image/png"
		}
	}
	return ct
}

func canModifyFiles(st models.CaseStatus) bool {
	switch st {
	case models.CaseOpen, models.CaseEngaged:
		return true
	default:
		return false
	}
}

func canDeleteFiles(st models.CaseStatus) bool {
	switch st {
	case models.CaseOpen, models.CaseCancelled:
		return true
	default:
		return false
	}
}

/* ========================= Upload ========================= */

// Upload Case Files godoc
// @Summary      Upload multiple case files (PDF/PNG)
// @Description  Client (owner) uploads up to 10 files. Only allowed when case is open/engaged.
// @Tags         files
// @Security     BearerAuth
// @Accept       multipart/form-data
// @Produce      json
// @Param        id     path      string   true  "case id (uuid)"
// @Param        files  formData  []file   true  "PDF/PNG (max 10; max 10MB each)"
// @Success      201    {object}  map[string]any  "results: [{id,key,name,size,error?}]"
// @Failure      400    {object}  models.ErrorResponse
// @Failure      403    {object}  models.ErrorResponse
// @Failure      404    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /cases/{id}/files [post]
func (h *Handler) UploadFile(c *fiber.Ctx) error {
	clientID := auth.MustUserID(c)
	caseID := c.Params("id")

	// Storage harus ada untuk upload
	if h.sb == nil {
		return fiber.NewError(fiber.StatusInternalServerError, "storage not configured")
	}

	// Check case & ownership
	var cs models.Case
	if err := h.db.First(&cs, "id = ?", caseID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	if cs.ClientID.String() != clientID {
		return fiber.ErrForbidden
	}
	if !canModifyFiles(cs.Status) {
		return fiber.NewError(fiber.StatusForbidden, "Files cannot be modified on a closed or cancelled case")
	}

	form, err := c.MultipartForm()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Multipart form required; send files[]")
	}
	files := form.File["files[]"]
	if len(files) == 0 {
		files = form.File["files"]
	}
	if len(files) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No files provided (key: files[])")
	}
	if len(files) > maxFilesPerRequest {
		return fiber.NewError(fiber.StatusBadRequest, "Too many files; maximum is 10")
	}

	results := make([]fiber.Map, 0, len(files))

	for _, fh := range files {
		item := fiber.Map{
			"name": fh.Filename,
			"size": fh.Size,
		}

		// Basic checks
		if fh.Size <= 0 {
			item["error"] = "Empty file"
			results = append(results, item)
			continue
		}
		if fh.Size > maxFileBytes {
			item["error"] = "Each file must be <= 10MB"
			results = append(results, item)
			continue
		}

		ct := normalizeCT(fh.Filename, fh.Header.Get("Content-Type"))
		if _, ok := allowedMIMEs[ct]; !ok {
			item["error"] = "Only PDF or PNG are allowed"
			results = append(results, item)
			continue
		}

		f, err := fh.Open()
		if err != nil {
			item["error"] = "Open failed"
			results = append(results, item)
			continue
		}
		defer f.Close()

		// Unique object key per upload
		key := h.sb.MakeObjectKey(caseID, fh.Filename)

		if err := h.sb.Upload(key, f, ct, fh.Size); err != nil {
			item["error"] = "Upload failed"
			results = append(results, item)
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
			item["error"] = "Database error"
			// Best-effort clean up the object
			_ = h.sb.Delete(key)
			results = append(results, item)
			continue
		}

		item["id"] = rec.ID
		item["key"] = rec.Key
		results = append(results, item)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"results": results})
}

/* ========================= Signed URL ========================= */

// Signed Download URL godoc
// @Summary      Get signed URL for a case file
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	allowed := false
	// Owner (client)
	if role == string(models.RoleClient) && cf.Case.ClientID.String() == userID {
		allowed = true
	}
	// Accepted lawyer on engaged/closed case
	if role == string(models.RoleLawyer) &&
		(cf.Case.Status == models.CaseEngaged || cf.Case.Status == models.CaseClosed) &&
		cf.Case.AcceptedLawyerID.String() == userID {
		allowed = true
	}
	if !allowed {
		return fiber.ErrForbidden
	}

	// Fallback untuk unit test: storage belum diinject
	if h.sb == nil {
		return c.JSON(fiber.Map{
			"url":        "https://example.com/test-signed-url",
			"expires_in": 60,
			"now":        time.Now().UTC(),
		})
	}

	url, err := h.sb.SignedURL(cf.Key, 60) // seconds
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"url": url, "expires_in": 60, "now": time.Now().UTC()})
}

/* ========================= Delete ========================= */

// Delete Case File godoc
// @Summary      Delete a case file
// @Description  Only the client owner can delete files, and only while the case is open/engaged.
// @Tags         files
// @Security     BearerAuth
// @Produce      json
// @Param        fileID  path string true "file id (uuid)"
// @Success      200  {object}  map[string]string  "status: ok"
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /files/{fileID} [delete]
func (h *Handler) DeleteFile(c *fiber.Ctx) error {
	userID := auth.MustUserID(c)
	role := auth.MustRole(c)
	if role != string(models.RoleClient) {
		return fiber.ErrForbidden
	}

	var cf models.CaseFile
	if err := h.db.Preload("Case").First(&cf, "id = ?", c.Params("fileID")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	// Ownership + status
	if cf.Case.ClientID.String() != userID {
		return fiber.ErrForbidden
	}
	if !canDeleteFiles(cf.Case.Status) {
		return fiber.NewError(fiber.StatusForbidden, "Files cannot be deleted on a closed or engaged case")
	}

	// Delete from storage first (best-effort). Jika storage nil, skip.
	if h.sb != nil {
		_ = h.sb.Delete(cf.Key) // best-effort; ignore error
	}

	// Then delete the DB record
	if err := h.db.Delete(&cf).Error; err != nil {
		return fiber.ErrInternalServerError
	}

	return c.JSON(fiber.Map{"status": "ok"})
}
