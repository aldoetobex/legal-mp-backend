package auth

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	jwt.RegisteredClaims
}

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
