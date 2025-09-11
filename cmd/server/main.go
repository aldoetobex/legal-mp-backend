package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"

	"github.com/aldoetobex/legal-mp-backend/pkg/database"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"

	// Tambahan:
	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/internal/cases"
	"github.com/aldoetobex/legal-mp-backend/internal/payments"
	"github.com/aldoetobex/legal-mp-backend/internal/quotes"
	"github.com/aldoetobex/legal-mp-backend/internal/storage"
)

func main() {
	_ = godotenv.Load()

	db := database.Init()
	if err := db.AutoMigrate(
		&models.User{}, &models.Case{}, &models.CaseFile{}, &models.Quote{}, &models.Payment{},
	); err != nil {
		log.Fatal("migration failed:", err)
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			// Default to 500
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		},
	})

	app.Get("/health", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })

	api := app.Group("/api")

	// Auth
	authH := auth.NewHandler(db)
	api.Post("/signup", authH.Signup)
	api.Post("/login", authH.Login)
	api.Get("/me", auth.RequireAuth(), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"userID": c.Locals("userID"), "role": c.Locals("role")})
	})

	// Storage helper
	sb := storage.NewSupabase() // pakai SUPABASE_URL / SUPABASE_SECRET_KEY / SUPABASE_BUCKET

	// Cases
	caseH := cases.NewHandler(db, sb)
	// Client
	api.Post("/cases", auth.RequireAuth(), auth.RequireRole("client"), caseH.Create)
	api.Get("/cases/mine", auth.RequireAuth(), auth.RequireRole("client"), caseH.ListMine)
	api.Get("/cases/:id", auth.RequireAuth(), auth.RequireRole("client"), caseH.GetDetailOwner)
	api.Post("/cases/:id/files", auth.RequireAuth(), auth.RequireRole("client"), caseH.UploadFile)
	// Lawyer
	api.Get("/marketplace", auth.RequireAuth(), auth.RequireRole("lawyer"), caseH.Marketplace)
	api.Get("/files/:fileID/signed-url", auth.RequireAuth(), caseH.SignedDownloadURL)

	quoteH := quotes.NewHandler(db)

	// Lawyer — upsert & my quotes
	api.Post("/quotes", auth.RequireAuth(), auth.RequireRole("lawyer"), quoteH.Upsert)
	api.Get("/quotes/mine", auth.RequireAuth(), auth.RequireRole("lawyer"), quoteH.ListMine)

	// Client — lihat semua quotes untuk case miliknya
	api.Get("/cases/:id/quotes", auth.RequireAuth(), auth.RequireRole("client"), quoteH.ListByCaseForOwner)

	payH := payments.NewHandler(db)

	api.Post("/checkout/:quoteID", auth.RequireAuth(), auth.RequireRole("client"), payH.CreateCheckout)

	// Hanya saat APP_ENV=dev dan PAYMENT_PROVIDER=mock:
	if os.Getenv("APP_ENV") == "dev" && os.Getenv("PAYMENT_PROVIDER") == "mock" {
		api.Post("/payments/mock/complete", payH.MockComplete) // Protected by X-Dev-Secret
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Println("Server running on :" + port)
	log.Fatal(app.Listen(":" + port))
}
