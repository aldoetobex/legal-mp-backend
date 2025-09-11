package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/pkg/models"
	"github.com/aldoetobex/legal-mp-backend/pkg/validation"
)

// DTOs untuk Swagger
type SignupRequest struct {
	Email        string `json:"email" validate:"required,email,max=60"`
	Password     string `json:"password" validate:"required,min=8"`
	Role         string `json:"role" validate:"required,oneof=client lawyer"`
	Name         string `json:"name" validate:"max=80"`
	Jurisdiction string `json:"jurisdiction" validate:"max=40"`
	BarNumber    string `json:"bar_number" validate:"max=40"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email,max=60"`
	Password string `json:"password" validate:"required"`
}

type AuthResponse struct {
	Token string `json:"token"`
	Role  string `json:"role"`
}

type Handler struct{ db *gorm.DB }

func NewHandler(db *gorm.DB) *Handler { return &Handler{db: db} }

// Signup godoc
// @Summary      Sign up
// @Description  Register user baru (client atau lawyer)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        payload  body  SignupRequest  true  "Signup payload"
// @Success      201      {object}  AuthResponse
// @Failure      400      {object}  models.ValidationErrorResponse
// @Failure      409      {object}  models.ErrorResponse  "email already exists"
// @Router       /signup [post]
func (h *Handler) Signup(c *fiber.Ctx) error {
	var in SignupRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.ErrBadRequest
	}
	// normalisasi kecil
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	// validasi ala Laravel
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	u := models.User{
		Email:        in.Email,
		PasswordHash: string(hash),
		Role:         models.Role(in.Role),
		Name:         in.Name,
		Jurisdiction: in.Jurisdiction,
		BarNumber:    in.BarNumber,
	}
	if err := h.db.Create(&u).Error; err != nil {
		return fiber.NewError(fiber.StatusConflict, "email already exists")
	}

	token, _ := IssueToken(u.ID.String(), string(u.Role))
	return c.Status(fiber.StatusCreated).JSON(AuthResponse{Token: token, Role: string(u.Role)})
}

// Login godoc
// @Summary      Login
// @Description  Login dan dapatkan JWT
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        payload  body  LoginRequest  true  "Login payload"
// @Success      200      {object}  AuthResponse
// @Failure      400      {object}  models.ValidationErrorResponse
// @Failure      401      {object}  models.ErrorResponse
// @Router       /login [post]
func (h *Handler) Login(c *fiber.Ctx) error {
	var in LoginRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.ErrBadRequest
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	var u models.User
	if err := h.db.Where("email = ?", in.Email).First(&u).Error; err != nil {
		return fiber.ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		return fiber.ErrUnauthorized
	}

	token, _ := IssueToken(u.ID.String(), string(u.Role))
	return c.JSON(AuthResponse{Token: token, Role: string(u.Role)})
}
