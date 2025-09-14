// internal/quotes/quotes_test.go
package quotes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/aldoetobex/legal-mp-backend/pkg/models"
)

/* ===== helpers ===== */

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	_ = godotenv.Load()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is empty")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Case{}, &models.CaseFile{},
		&models.CaseHistory{}, &models.Quote{}, &models.Payment{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Bersihin SETELAH test selesai, bukan di awal/tengah
	t.Cleanup(func() {
		sql := `
TRUNCATE TABLE
	payments,
	case_histories,
	case_files,
	quotes,
	cases,
	users
RESTART IDENTITY CASCADE`
		if err := db.Exec(sql).Error; err != nil {
			t.Logf("truncate failed (ignored): %v", err)
		}
	})

	return db
}

func withTx(t *testing.T, db *gorm.DB, fn func(tx *gorm.DB)) {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback().Error
			panic(r)
		}
	}()
	fn(tx)
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

type seedOut struct {
	ClientID uuid.UUID
	LawyerID uuid.UUID
	CaseID   uuid.UUID
}

func seedCase(t *testing.T, tx *gorm.DB, status models.CaseStatus) seedOut {
	t.Helper()
	clientID := uuid.New()
	lawyerID := uuid.New()

	cEmail := fmt.Sprintf("c+%s@test.local", uuid.NewString())
	lEmail := fmt.Sprintf("l+%s@test.local", uuid.NewString())

	if err := tx.Create(&models.User{ID: clientID, Email: cEmail, Role: models.RoleClient}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&models.User{ID: lawyerID, Email: lEmail, Role: models.RoleLawyer}).Error; err != nil {
		t.Fatal(err)
	}

	cs := models.Case{
		ID:          uuid.New(),
		ClientID:    clientID,
		Title:       "T",
		Category:    "Cat",
		Description: "D",
		Status:      status,
		CreatedAt:   time.Now(),
	}
	if err := tx.Create(&cs).Error; err != nil {
		t.Fatal(err)
	}

	return seedOut{ClientID: clientID, LawyerID: lawyerID, CaseID: cs.ID}
}

// versi tanpa tx untuk test yang butuh commit (dipakai di Upsert test)
func seedCaseNoTx(t *testing.T, db *gorm.DB, status models.CaseStatus) seedOut {
	return seedCase(t, db, status)
}

// injectAuth: pasang berbagai key supaya MustUserID/MustRole bisa kebaca
func injectAuth(userID uuid.UUID, role string) fiber.Handler {
	id := userID.String()
	return func(c *fiber.Ctx) error {
		c.Locals("user_id", id)
		c.Locals("userID", id)
		c.Locals("userId", id)
		c.Locals("uid", id)
		c.Locals("role", role)
		c.Locals("user_role", role)
		c.Locals("user", struct {
			ID   string
			Role string
		}{ID: id, Role: role})
		return c.Next()
	}
}

func newTestApp(h *Handler, userID uuid.UUID, role string) *fiber.App {
	app := fiber.New()
	app.Use(injectAuth(userID, role))
	app.Post("/api/quotes", h.Upsert)
	app.Get("/api/quotes/mine", h.ListMine)
	return app
}

/* ================== TESTS ================== */

// ---- FIXED: seed pakai DB langsung (commit), handler juga pakai DB yang sama
func Test_UpsertQuote_UpdatesExistingNotCreateNew(t *testing.T) {
	db := openTestDB(t)

	seed := seedCaseNoTx(t, db, models.CaseOpen) // <— no tx, jadi committed

	hq := NewHandler(db) // <— handler pakai db (bukan tx)
	app := newTestApp(hq, seed.LawyerID, string(models.RoleLawyer))

	body1 := `{"case_id":"` + seed.CaseID.String() + `","amount_cents":5000,"days":5,"note":"A"}`
	req1 := httptest.NewRequest("POST", "/api/quotes", strings.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	resp1, _ := app.Test(req1)
	if resp1.StatusCode != 201 {
		t.Fatalf("upsert-1 got %d", resp1.StatusCode)
	}

	body2 := `{"case_id":"` + seed.CaseID.String() + `","amount_cents":7000,"days":7,"note":"B"}`
	req2 := httptest.NewRequest("POST", "/api/quotes", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 201 {
		t.Fatalf("upsert-2 got %d", resp2.StatusCode)
	}

	var cnt int64
	if err := db.Model(&models.Quote{}).
		Where("case_id = ? AND lawyer_id = ?", seed.CaseID, seed.LawyerID).
		Count(&cnt).Error; err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("want 1 row, got %d", cnt)
	}

	var q models.Quote
	if err := db.First(&q, "case_id = ? AND lawyer_id = ?", seed.CaseID, seed.LawyerID).Error; err != nil {
		t.Fatal(err)
	}
	if q.AmountCents != 7000 || q.Days != 7 || q.Note != "B" {
		t.Fatalf("not updated: %+v", q)
	}
}

func Test_ListMine_ReturnsOnlyMyQuotes(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		s1 := seedCase(t, tx, models.CaseOpen)
		s2 := seedCase(t, tx, models.CaseOpen)

		_ = tx.Create(&models.Quote{
			CaseID:      s1.CaseID,
			LawyerID:    s1.LawyerID,
			AmountCents: 1000, Days: 1, Note: "A",
			Status: models.QuoteProposed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}).Error

		lawyerB := uuid.New()
		lbEmail := fmt.Sprintf("lb+%s@test.local", uuid.NewString())
		_ = tx.Create(&models.User{ID: lawyerB, Email: lbEmail, Role: models.RoleLawyer}).Error
		_ = tx.Create(&models.Quote{
			CaseID:      s2.CaseID,
			LawyerID:    lawyerB,
			AmountCents: 2000, Days: 2, Note: "B",
			Status: models.QuoteProposed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}).Error

		hq := NewHandler(tx)
		app := newTestApp(hq, s1.LawyerID, string(models.RoleLawyer))

		req := httptest.NewRequest("GET", "/api/quotes/mine?status=proposed&page=1&pageSize=50", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("got %d", resp.StatusCode)
		}

		var out struct {
			Items []struct {
				Note string `json:"note"`
			} `json:"items"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)

		if len(out.Items) != 1 || out.Items[0].Note != "A" {
			t.Fatalf("expected 1 my quote, got %+v", out.Items)
		}
	})
}
func Test_UpsertQuote_Forbidden_WhenCaseNotOpen(t *testing.T) {
	db := openTestDB(t)

	// Coba untuk tiga status non-OPEN
	for _, st := range []models.CaseStatus{
		models.CaseEngaged,
		models.CaseClosed,
		models.CaseCancelled,
	} {
		withTx(t, db, func(tx *gorm.DB) {
			seed := seedCase(t, tx, st)

			h := NewHandler(tx)
			app := newTestApp(h, seed.LawyerID, string(models.RoleLawyer))

			body := `{"case_id":"` + seed.CaseID.String() + `","amount_cents":12345,"days":3,"note":"try"}`
			req := httptest.NewRequest("POST", "/api/quotes", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := app.Test(req)

			if resp.StatusCode != 409 && resp.StatusCode != 403 {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status for %s must be 409/403, got %d. body=%s", st, resp.StatusCode, string(b))
			}
		})
	}
}
