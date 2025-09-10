package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/pkg/database"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

func main() {
	// load .env
	_ = godotenv.Load()

	// init DB
	db := database.Init()

	// migrate models
	if err := db.AutoMigrate(
		&models.User{},
		&models.Case{},
		&models.CaseFile{},
		&models.Quote{},
		&models.Payment{},
	); err != nil {
		log.Fatal("migration failed:", err)
	}

	// init app
	app := fiber.New()

	// healthcheck
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// inside main():
	authH := auth.NewHandler(db)

	api := app.Group("/api")
	api.Post("/signup", authH.Signup)
	api.Post("/login", authH.Login)

	// contoh route proteksi awal:
	api.Get("/me", auth.RequireAuth(), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"userID": c.Locals("userID"), "role": c.Locals("role")})
	})

	// run server
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Println("Server running on :" + port)
	log.Fatal(app.Listen(":" + port))
}
