// @title           Mini Legal Marketplace API
// @version         1.0
// @description     API for a mini legal marketplace: clients post cases, lawyers submit quotes, clients accept & pay, and lawyers access files via signed URLs.
// @contact.name    Aldo Rifki Putra
// @contact.email   aldoetobex@gmail.com
// @BasePath        /api
// @schemes         http
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     Format: Bearer <token>
package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"

	"github.com/aldoetobex/legal-mp-backend/pkg/database"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"

	// Docs
	_ "github.com/aldoetobex/legal-mp-backend/docs" // change module path to match your repo
	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/internal/cases"
	"github.com/aldoetobex/legal-mp-backend/internal/payments"
	"github.com/aldoetobex/legal-mp-backend/internal/quotes"
	"github.com/aldoetobex/legal-mp-backend/internal/storage"
	fiberSwagger "github.com/gofiber/swagger"
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
		ErrorHandler: auth.ErrorHandler, // or your middleware package
	})

	app.Get("/health", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })

	api := app.Group("/api")

	app.Get("/swagger/*", fiberSwagger.HandlerDefault)

	// Auth
	authH := auth.NewHandler(db)
	api.Post("/signup", authH.Signup)
	api.Post("/login", authH.Login)
	api.Get("/me", auth.RequireAuth(), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"userID": c.Locals("userID"), "role": c.Locals("role")})
	})

	// Storage helper
	sb := storage.NewSupabase() // uses SUPABASE_URL / SUPABASE_SECRET_KEY / SUPABASE_BUCKET

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

	// Quotes
	quoteH := quotes.NewHandler(db)
	// Lawyer — upsert & my quotes
	api.Post("/quotes", auth.RequireAuth(), auth.RequireRole("lawyer"), quoteH.Upsert)
	api.Get("/quotes/mine", auth.RequireAuth(), auth.RequireRole("lawyer"), quoteH.ListMine)
	// Client — view all quotes for their case
	api.Get("/cases/:id/quotes", auth.RequireAuth(), auth.RequireRole("client"), quoteH.ListByCaseForOwner)

	// Payments
	payH := payments.NewHandler(db)
	api.Post("/checkout/:quoteID", auth.RequireAuth(), auth.RequireRole("client"), payH.CreateCheckout)

	// Stripe Webhook (server-only, no auth)
	api.Post("/payments/stripe/webhook", payH.StripeWebhook)

	// Only in dev mode with mock payment provider
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
