package auth

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/aldoetobex/legal-mp-backend/pkg/models"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// func ErrorHandler(c *fiber.Ctx, err error) error {
// 	if fe, ok := err.(*fiber.Error); ok {
// 		return c.Status(fe.Code).JSON(models.ErrorResponse{
// 			Error:   true,
// 			Message: fe.Message,
// 		})
// 	}
// 	return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
// 		Error:   true,
// 		Message: "Internal Server Error",
// 	})
// }

func IssueToken(userID, role string) (string, error) {
	claims := &Claims{
		Sub:  userID,
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(os.Getenv("JWT_SECRET")))
}

func RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			return fiber.ErrUnauthorized
		}
		tokenStr := strings.TrimPrefix(h, "Bearer ")
		token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
			return []byte(os.Getenv("JWT_SECRET")), nil
		})
		if err != nil || !token.Valid {
			return fiber.ErrUnauthorized
		}
		claims, ok := token.Claims.(*Claims)
		if !ok {
			return fiber.ErrUnauthorized
		}
		c.Locals("userID", claims.Sub)
		c.Locals("role", claims.Role)
		return c.Next()
	}
}

func MustUserID(c *fiber.Ctx) string {
	if v := c.Locals("userID"); v != nil {
		return v.(string)
	}
	panic(errors.New("user not in context"))
}

func MustRole(c *fiber.Ctx) string {
	if v := c.Locals("role"); v != nil {
		return v.(string)
	}
	panic(errors.New("role not in context"))
}

func RequireRole(role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if MustRole(c) != role {
			return fiber.ErrForbidden
		}
		return c.Next()
	}
}

func httpCodeToString(code int) string {
	switch code {
	case fiber.StatusBadRequest:
		return "BAD_REQUEST"
	case fiber.StatusUnauthorized:
		return "UNAUTHORIZED"
	case fiber.StatusForbidden:
		return "FORBIDDEN"
	case fiber.StatusNotFound:
		return "NOT_FOUND"
	case fiber.StatusConflict:
		return "CONFLICT"
	case fiber.StatusUnprocessableEntity:
		return "UNPROCESSABLE_ENTITY"
	case fiber.StatusRequestEntityTooLarge:
		return "PAYLOAD_TOO_LARGE"
	default:
		return "INTERNAL_SERVER_ERROR"
	}
}

// ErrorHandler is a global Fiber error handler to return a consistent JSON shape.
func ErrorHandler(c *fiber.Ctx, err error) error {
	// default values
	code := fiber.StatusInternalServerError
	msg := "Internal Server Error"

	// Fiber errors keep status code
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
		if strings.TrimSpace(e.Message) != "" {
			msg = e.Message
		} else {
			msg = fiber.ErrInternalServerError.Message
			switch code {
			case fiber.StatusBadRequest:
				msg = fiber.ErrBadRequest.Message
			case fiber.StatusUnauthorized:
				msg = fiber.ErrUnauthorized.Message
			case fiber.StatusForbidden:
				msg = fiber.ErrForbidden.Message
			case fiber.StatusNotFound:
				msg = fiber.ErrNotFound.Message
			case fiber.StatusConflict:
				msg = fiber.ErrConflict.Message
			}
		}
	}

	return c.Status(code).JSON(models.ErrorResponse{
		Code:    httpCodeToString(code),
		Error:   true,
		Message: msg,
	})
}
