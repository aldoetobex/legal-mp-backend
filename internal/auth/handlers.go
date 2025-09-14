package auth

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/pkg/models"
	"github.com/aldoetobex/legal-mp-backend/pkg/validation"
)

/* ================================ DTOs ================================= */

// Request body for /signup
type SignupRequest struct {
	Role     string `json:"role" validate:"required,oneof=client lawyer"`
	Name     string `json:"name" validate:"required,min=2,max=80"`
	Email    string `json:"email" validate:"required,email,max=120"`
	Password string `json:"password" validate:"required,min=6,max=72"`
	// Optional for lawyers
	Jurisdiction string `json:"jurisdiction" validate:"omitempty,jurisdiction"`
	BarNumber    string `json:"bar_number" validate:"omitempty,barnum"`
}

// Request body for /login
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email,max=60"`
	Password string `json:"password" validate:"required"`
}

// Standard auth response
type AuthResponse struct {
	Token string `json:"token"`
	Role  string `json:"role"`
}

// Profile response for /me
type UserProfileResponse struct {
	ID           uuid.UUID   `json:"id"`
	Email        string      `json:"email"`
	Role         models.Role `json:"role"`
	Name         string      `json:"name"`
	Jurisdiction string      `json:"jurisdiction"`
	BarNumber    string      `json:"bar_number"`
	CreatedAt    time.Time   `json:"created_at"`
}

/* ============================== Handler ================================= */

type Handler struct{ db *gorm.DB }

func NewHandler(db *gorm.DB) *Handler { return &Handler{db: db} }

/* =============================== Signup ================================= */

// @Summary      Sign up
// @Description  Register a new user (client or lawyer)
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

	// Normalize email
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	// Validate request (Laravel-like error shape)
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	// Hash password
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)

	// Create user
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

	// Issue JWT
	token, _ := IssueToken(u.ID.String(), string(u.Role))
	return c.Status(fiber.StatusCreated).JSON(AuthResponse{Token: token, Role: string(u.Role)})
}

/* ================================ Login ================================= */

// @Summary      Login
// @Description  Authenticate and receive a JWT
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

	// Normalize email
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	// Validate request
	if errs, _ := validation.Validate(in); errs != nil {
		return validation.Respond(c, errs)
	}

	// Find user by email
	var u models.User
	if err := h.db.Where("email = ?", in.Email).First(&u).Error; err != nil {
		return fiber.ErrUnauthorized
	}

	// Verify password
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		return fiber.ErrUnauthorized
	}

	// Issue JWT
	token, _ := IssueToken(u.ID.String(), string(u.Role))
	return c.JSON(AuthResponse{Token: token, Role: string(u.Role)})
}

/* ================================= Me =================================== */

// @Summary      Get current user profile
// @Description  Return full profile of the authenticated user
// @Tags         auth
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  models.User
// @Failure      401  {object}  models.ErrorResponse
// @Router       /me [get]
func (h *Handler) Me(c *fiber.Ctx) error {
	userID := c.Locals("userID")
	if userID == nil {
		return fiber.ErrUnauthorized
	}

	// Load user by ID from context (set by auth middleware)
	var u models.User
	if err := h.db.First(&u, "id = ?", userID).Error; err != nil {
		return fiber.ErrUnauthorized
	}

	// Map to a stable public profile shape
	resp := UserProfileResponse{
		ID:           u.ID,
		Email:        u.Email,
		Role:         u.Role,
		Name:         u.Name,
		Jurisdiction: u.Jurisdiction,
		BarNumber:    u.BarNumber,
		CreatedAt:    u.CreatedAt,
	}
	return c.JSON(resp)
}
