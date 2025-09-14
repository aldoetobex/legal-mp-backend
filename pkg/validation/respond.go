package validation

import "github.com/gofiber/fiber/v2"

// Respond writes a 400 Bad Request JSON response
// using Laravel-style validation error format.
func Respond(c *fiber.Ctx, errs map[string][]string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"message": "Validation failed",
		"errors":  errs,
	})
}
