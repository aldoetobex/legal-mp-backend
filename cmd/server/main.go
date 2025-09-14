// @title           Mini Legal Marketplace API
// @version         1.0
// @description     API for a mini legal marketplace: clients post cases, lawyers submit quotes, clients accept & pay, and lawyers access files via signed URLs.
// @contact.name    Aldo Rifki Putra
// @contact.email   aldoetobex@gmail.com
// @BasePath        /api
// @schemes         https http
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     Format: Bearer <token>
package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/joho/godotenv"

	"github.com/aldoetobex/legal-mp-backend/pkg/database"
	"github.com/aldoetobex/legal-mp-backend/pkg/models"

	// Swagger docs (adjust module path if needed)
	_ "github.com/aldoetobex/legal-mp-backend/docs"

	"github.com/aldoetobex/legal-mp-backend/internal/auth"
	"github.com/aldoetobex/legal-mp-backend/internal/cases"
	"github.com/aldoetobex/legal-mp-backend/internal/payments"
	"github.com/aldoetobex/legal-mp-backend/internal/quotes"
	"github.com/aldoetobex/legal-mp-backend/internal/storage"
	fiberSwagger "github.com/gofiber/swagger"
)

func main() {
	// Load .env (no-op if file missing)
	_ = godotenv.Load()

	// Initialize DB and run migrations (idempotent)
	db := database.Init()
	if err := db.AutoMigrate(
		&models.User{},
		&models.Case{},
		&models.CaseFile{},
		&models.Quote{},
		&models.Payment{},
		&models.CaseHistory{},
	); err != nil {
		log.Fatal("migration failed:", err)
	}

	// Create Fiber app with a centralized error handler
	app := fiber.New(fiber.Config{
		ErrorHandler: auth.ErrorHandler,
	})

	// CORS: allow one or more frontend origins (comma-separated)
	// Example: http://localhost:3000,https://your-frontend.vercel.app
	allowed := os.Getenv("FRONTEND_ORIGIN")
	if allowed == "" {
		// Developer-friendly default
		allowed = "http://localhost:3000,https://legal-mp-frontend.vercel.app"
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowed,
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Authorization,Content-Type",
		AllowCredentials: true,
		MaxAge:           600,
	}))

	// Respond to preflight quickly (useful behind strict proxies)
	app.Options("/*", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	// Basic health check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Swagger UI
	app.Get("/swagger/*", fiberSwagger.HandlerDefault)

	// API group
	api := app.Group("/api")

	/* ============================ Auth ============================ */
	authH := auth.NewHandler(db)
	api.Post("/signup", authH.Signup)
	api.Post("/login", authH.Login)
	api.Get("/me", auth.RequireAuth(), authH.Me)

	/* ============================ Storage ============================ */
	// Uses SUPABASE_URL / SUPABASE_SECRET_KEY / SUPABASE_BUCKET
	sb := storage.NewSupabase()

	/* ============================ Cases ============================ */
	caseH := cases.NewHandler(db, sb)

	// Client endpoints
	api.Post("/cases", auth.RequireAuth(), auth.RequireRole("client"), caseH.Create)
	api.Get("/cases/mine", auth.RequireAuth(), auth.RequireRole("client"), caseH.ListMine)
	api.Get("/cases/:id", auth.RequireAuth(), caseH.GetDetail)
	api.Post("/cases/:id/files", auth.RequireAuth(), auth.RequireRole("client"), caseH.UploadFile)
	api.Get("/cases/:id/history", auth.RequireAuth(), caseH.ListHistory)
	api.Post("/cases/:id/cancel", auth.RequireAuth(), auth.RequireRole("client"), caseH.Cancel)
	api.Post("/cases/:id/close", auth.RequireAuth(), auth.RequireRole("client"), caseH.Close)

	// Lawyer endpoints
	api.Get("/marketplace", auth.RequireAuth(), auth.RequireRole("lawyer"), caseH.Marketplace)
	api.Get("/files/:fileID/signed-url", auth.RequireAuth(), caseH.SignedDownloadURL)
	api.Delete("/files/:fileID", auth.RequireAuth(), auth.RequireRole("client"), caseH.DeleteFile)

	/* ============================ Quotes ============================ */
	quoteH := quotes.NewHandler(db)

	// Lawyer: create/update quote & list mine
	api.Post("/quotes", auth.RequireAuth(), auth.RequireRole("lawyer"), quoteH.Upsert)
	api.Get("/quotes/mine", auth.RequireAuth(), auth.RequireRole("lawyer"), quoteH.ListMine)

	// Client: list all quotes for own case
	api.Get("/cases/:id/quotes", auth.RequireAuth(), quoteH.ListByCaseForOwner)

	/* ============================ Payments ============================ */
	payH := payments.NewHandler(db)

	// Client: start checkout for a selected quote
	api.Post("/checkout/:quoteID", auth.RequireAuth(), auth.RequireRole("client"), payH.CreateCheckout)

	// Stripe webhook (server → server). No auth; verify via Stripe signature.
	api.Post("/payments/stripe/webhook", payH.StripeWebhook)

	// Dev-only mock payment completion (guarded by X-Dev-Secret)
	if os.Getenv("APP_ENV") == "dev" && os.Getenv("PAYMENT_PROVIDER") == "mock" {
		api.Post("/payments/mock/complete", payH.MockComplete)
	}

	/* ============================ Server ============================ */
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Println("Server running on :" + port)
	log.Fatal(app.Listen(":" + port))
}
