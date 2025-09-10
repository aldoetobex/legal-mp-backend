package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

type Handler struct{ db *gorm.DB }

func NewHandler(db *gorm.DB) *Handler { return &Handler{db: db} }

type signupReq struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	Role         string `json:"role"` // "client" or "lawyer"
	Name         string `json:"name"`
	Jurisdiction string `json:"jurisdiction"`
	BarNumber    string `json:"barNumber"`
}

func (h *Handler) Signup(c *fiber.Ctx) error {
	var in signupReq
	if err := c.BodyParser(&in); err != nil {
		return fiber.ErrBadRequest
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email == "" || len(in.Password) < 8 {
		return fiber.ErrBadRequest
	}
	if in.Role != string(models.RoleClient) && in.Role != string(models.RoleLawyer) {
		return fiber.ErrBadRequest
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	u := models.User{
		Email: in.Email, PasswordHash: string(hash),
		Role: models.Role(in.Role), Name: in.Name,
		Jurisdiction: in.Jurisdiction, BarNumber: in.BarNumber,
	}
	if err := h.db.Create(&u).Error; err != nil {
		return fiber.NewError(fiber.StatusConflict, "email already exists")
	}
	token, _ := IssueToken(u.ID.String(), string(u.Role))
	return c.JSON(fiber.Map{"token": token, "role": u.Role})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(c *fiber.Ctx) error {
	var in loginReq
	if err := c.BodyParser(&in); err != nil {
		return fiber.ErrBadRequest
	}
	var u models.User
	if err := h.db.Where("email = ?", strings.ToLower(in.Email)).First(&u).Error; err != nil {
		return fiber.ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		return fiber.ErrUnauthorized
	}
	token, _ := IssueToken(u.ID.String(), string(u.Role))
	return c.JSON(fiber.Map{"token": token, "role": u.Role})
}
