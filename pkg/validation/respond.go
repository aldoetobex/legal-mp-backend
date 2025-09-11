package validation

import "github.com/gofiber/fiber/v2"

// Tulis respons 400 dengan format Laravel-style
func Respond(c *fiber.Ctx, errs map[string][]string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"message": "Validation failed",
		"errors":  errs,
	})
}
