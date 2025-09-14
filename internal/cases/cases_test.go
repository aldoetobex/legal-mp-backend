package cases

import (
	"encoding/json"
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
	"github.com/aldoetobex/legal-mp-backend/pkg/sanitize"
)

/* ========= helpers ========= */

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

// injectAuth: set locals biar MustUserID / MustRole ketemu.
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

// ---- ganti newTestApp: taruh /mine sebelum /:id
func newTestApp(h *Handler, userID uuid.UUID, role string) *fiber.App {
	app := fiber.New()
	app.Use(injectAuth(userID, role))

	// ⚠️ Daftarkan rute statis lebih dulu agar tidak “dimakan” oleh :id
	app.Get("/api/cases/mine", h.ListMine)
	app.Get("/api/marketplace", h.Marketplace)

	// Files (dipakai oleh test Signed URL / Upload)
	app.Post("/api/cases/:id/files", h.UploadFile)
	app.Get("/api/files/:fileID/signed-url", h.SignedDownloadURL)
	app.Delete("/api/files/:fileID", h.DeleteFile)

	// Terakhir: rute dengan param
	app.Get("/api/cases/:id", h.GetDetail)

	// Create dipakai test validasi
	app.Post("/api/cases", h.Create)

	return app
}

type seedResult struct {
	ClientID uuid.UUID
	LawyerID uuid.UUID
	CaseID   uuid.UUID
}

func seedCase(t *testing.T, db *gorm.DB, status models.CaseStatus) seedResult {
	clientID := uuid.New()
	lawyerID := uuid.New()
	caseID := uuid.New()

	if err := db.Create(&models.User{
		ID:    clientID,
		Email: "client_" + clientID.String()[:8] + "@x.com",
		Role:  models.RoleClient,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.User{
		ID:    lawyerID,
		Email: "lawyer_" + lawyerID.String()[:8] + "@x.com",
		Role:  models.RoleLawyer,
	}).Error; err != nil {
		t.Fatal(err)
	}

	cs := models.Case{
		ID:        caseID,
		ClientID:  clientID,
		Title:     "Test Case",
		Category:  "Cat",
		Status:    status,
		CreatedAt: time.Now(),
	}
	if err := db.Create(&cs).Error; err != nil {
		t.Fatal(err)
	}

	return seedResult{ClientID: clientID, LawyerID: lawyerID, CaseID: caseID}
}

// makeCase: benar2 INSERT satu case untuk client tertentu dan mengembalikan ID-nya.
// gunakan createdAt supaya urutan DESC deterministic terhadap handler kamu.
func makeCase(t *testing.T, tx *gorm.DB, clientID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	cs := models.Case{
		ID:        id,
		ClientID:  clientID,
		Title:     "Case " + id.String()[0:6],
		Category:  "Cat",
		Status:    models.CaseOpen,
		CreatedAt: createdAt,
	}
	if err := tx.Create(&cs).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func addQuote(t *testing.T, tx *gorm.DB, caseID, lawyerID uuid.UUID, note string) models.Quote {
	q := models.Quote{
		CaseID: caseID, LawyerID: lawyerID,
		AmountCents: 500, Days: 2, Note: note,
		Status: models.QuoteProposed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := tx.Create(&q).Error; err != nil {
		t.Fatal(err)
	}
	return q
}

/* ========= TESTS ========= */

func Test_Client_SeesRedactedNotes_WhenCaseOpen(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		seed := seedCase(t, tx, models.CaseOpen)
		addQuote(t, tx, seed.CaseID, seed.LawyerID, "email test@example.com phone 08123456789")

		h := NewHandler(tx, nil)
		app := newTestApp(h, seed.ClientID, string(models.RoleClient))

		req := httptest.NewRequest("GET", "/api/cases/"+seed.CaseID.String(), nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}

		var body struct{ models.Case }
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if len(body.Quotes) != 1 {
			t.Fatalf("want 1 quote, got %d", len(body.Quotes))
		}
		got := body.Quotes[0].Note
		if strings.Contains(got, "0812") || strings.Contains(got, "@") {
			t.Fatalf("note not redacted: %q", got)
		}
	})
}

func Test_Client_SeesOriginalNotes_WhenEngaged(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		seed := seedCase(t, tx, models.CaseEngaged)
		addQuote(t, tx, seed.CaseID, seed.LawyerID, "email test@example.com phone 08123456789")

		h := NewHandler(tx, nil)
		app := newTestApp(h, seed.ClientID, string(models.RoleClient))

		req := httptest.NewRequest("GET", "/api/cases/"+seed.CaseID.String(), nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}

		var body struct{ models.Case }
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if !strings.Contains(body.Quotes[0].Note, "08123456789") {
			t.Fatalf("note should be original, got: %q", body.Quotes[0].Note)
		}
	})
}

func Test_FileNameIsSHA1Masked_OnGetDetail(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		seed := seedCase(t, tx, models.CaseOpen)
		f := models.CaseFile{
			CaseID: seed.CaseID,
			Key:    "case/" + seed.CaseID.String() + "/secret.pdf",
			Mime:   "application/pdf", Size: 123, OriginalName: "My-Payslip.pdf",
			CreatedAt: time.Now(),
		}
		if err := tx.Create(&f).Error; err != nil {
			t.Fatal(err)
		}

		h := NewHandler(tx, nil)
		app := newTestApp(h, seed.ClientID, string(models.RoleClient))

		req := httptest.NewRequest("GET", "/api/cases/"+seed.CaseID.String(), nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}

		var body struct{ models.Case }
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if len(body.Files) != 1 {
			t.Fatalf("want 1 file")
		}
		if body.Files[0].OriginalName == "My-Payslip.pdf" {
			t.Fatalf("filename should be masked in response")
		}
		if !strings.HasSuffix(body.Files[0].OriginalName, ".pdf") {
			t.Fatalf("masked name should keep extension, got %q", body.Files[0].OriginalName)
		}
	})
}

func Test_ListMine_Pagination_And_QuoteCounts(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		// user
		clientID := uuid.New()
		if err := tx.Create(&models.User{ID: clientID, Email: "c_" + clientID.String()[:6] + "@x.com", Role: models.RoleClient}).Error; err != nil {
			t.Fatal(err)
		}
		lawyerID := uuid.New()
		if err := tx.Create(&models.User{ID: lawyerID, Email: "l_" + lawyerID.String()[:6] + "@x.com", Role: models.RoleLawyer}).Error; err != nil {
			t.Fatal(err)
		}

		// bikin 3 case milik client (urutan dibuat deterministik: c3 terbaru)
		now := time.Now()
		c1 := makeCase(t, tx, clientID, now.Add(-3*time.Minute))
		c2 := makeCase(t, tx, clientID, now.Add(-2*time.Minute))
		c3 := makeCase(t, tx, clientID, now.Add(-1*time.Minute)) // newest

		// quote:
		// c1: 2 quotes
		addQuote(t, tx, c1, lawyerID, "Q1")
		addQuote(t, tx, c1, uuid.New(), "Q2")
		// c2: 1 quote
		addQuote(t, tx, c2, lawyerID, "Q3")
		// c3: 0 quote

		h := NewHandler(tx, nil)
		app := newTestApp(h, clientID, string(models.RoleClient))

		// pageSize=2 -> harusnya mengembalikan 2 item pertama berdasar created_at DESC: c3, c2
		req := httptest.NewRequest("GET", "/api/cases/mine?page=1&pageSize=2", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("got %d", resp.StatusCode)
		}

		var out struct {
			Page     int `json:"page"`
			PageSize int `json:"pageSize"`
			Total    int `json:"total"`
			Pages    int `json:"pages"`
			Items    []struct {
				ID     string `json:"id"`
				Quotes int64  `json:"quotes"`
			} `json:"items"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)

		if out.Total != 3 {
			t.Fatalf("want total=3, got %d", out.Total)
		}
		if len(out.Items) != 2 {
			t.Fatalf("want 2 items on first page, got %d", len(out.Items))
		}

		// karena DESC: item[0] = c3 (0 quotes), item[1] = c2 (1 quote)
		if out.Items[0].ID != c3.String() || out.Items[0].Quotes != 0 {
			t.Fatalf("page item[0] should be c3 with 0 quotes, got %#v", out.Items[0])
		}
		if out.Items[1].ID != c2.String() || out.Items[1].Quotes != 1 {
			t.Fatalf("page item[1] should be c2 with 1 quote, got %#v", out.Items[1])
		}

		// cek halaman ke-2 harus c1 (2 quotes)
		req2 := httptest.NewRequest("GET", "/api/cases/mine?page=2&pageSize=2", nil)
		resp2, _ := app.Test(req2)
		if resp2.StatusCode != 200 {
			t.Fatalf("got %d", resp2.StatusCode)
		}
		var out2 struct {
			Items []struct {
				ID     string `json:"id"`
				Quotes int64  `json:"quotes"`
			} `json:"items"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&out2)
		if len(out2.Items) != 1 || out2.Items[0].ID != c1.String() || out2.Items[0].Quotes != 2 {
			t.Fatalf("page 2 should return c1 with 2 quotes, got %#v", out2.Items)
		}
	})
}

func newTestAppFiles(h *Handler, userID uuid.UUID, role string) *fiber.App {
	app := fiber.New()
	app.Use(injectAuth(userID, role))
	app.Get("/files/:fileID/signed-url", h.SignedDownloadURL)
	return app
}

type seed struct {
	ClientID, LawyerID, CaseID uuid.UUID
	FileID                     uuid.UUID
}

func seedEngagedWithFile(t *testing.T, tx *gorm.DB) seed {
	clientID, lawyerID, caseID := uuid.New(), uuid.New(), uuid.New()
	emailC := "c_" + uuid.NewString()[:8] + "@x.com"
	emailL := "l_" + uuid.NewString()[:8] + "@x.com"
	_ = tx.Create(&models.User{ID: clientID, Email: emailC, Role: models.RoleClient}).Error
	_ = tx.Create(&models.User{ID: lawyerID, Email: emailL, Role: models.RoleLawyer}).Error
	cs := models.Case{ID: caseID, ClientID: clientID, Status: models.CaseEngaged, AcceptedLawyerID: lawyerID}
	_ = tx.Create(&cs).Error
	f := models.CaseFile{CaseID: caseID, Key: "case/" + caseID.String() + "/a.pdf", Mime: "application/pdf", Size: 1, OriginalName: "a.pdf", CreatedAt: time.Now()}
	_ = tx.Create(&f).Error
	return seed{clientID, lawyerID, caseID, f.ID}
}

/* tests */
func Test_SignedURL_ClientOwner_OK(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		seed := seedCase(t, tx, models.CaseOpen)

		// simpan file
		f := models.CaseFile{
			CaseID:       seed.CaseID,
			Key:          "case/" + seed.CaseID.String() + "/doc.pdf",
			Mime:         "application/pdf",
			Size:         123,
			OriginalName: "Secret.pdf",
			CreatedAt:    time.Now(),
		}
		if err := tx.Create(&f).Error; err != nil {
			t.Fatal(err)
		}

		h := NewHandler(tx, nil) // sb=nil → fallback URL dummy
		app := newTestApp(h, seed.ClientID, string(models.RoleClient))

		req := httptest.NewRequest("GET", "/api/files/"+f.ID.String()+"/signed-url", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if _, ok := out["url"]; !ok {
			t.Fatalf("missing url in response: %#v", out)
		}
	})
}

func Test_SignedURL_LawyerOnlyIfAccepted_OK(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		s := seedEngagedWithFile(t, tx)
		app := newTestAppFiles(NewHandler(tx, nil), s.LawyerID, string(models.RoleLawyer))

		req := httptest.NewRequest("GET", "/files/"+s.FileID.String()+"/signed-url", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
	})
}

func Test_SignedURL_RandomUser_Forbidden(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		s := seedEngagedWithFile(t, tx)
		random := uuid.New()
		_ = tx.Create(&models.User{ID: random, Email: "r@t", Role: models.RoleClient}).Error

		app := newTestAppFiles(NewHandler(tx, nil), random, string(models.RoleClient))
		req := httptest.NewRequest("GET", "/files/"+s.FileID.String()+"/signed-url", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 403 {
			t.Fatalf("want 403, got %d", resp.StatusCode)
		}
	})
}

func seedOpenCase(t *testing.T, tx *gorm.DB, desc string, createdAt time.Time) uuid.UUID {
	clientID := uuid.New()
	email := "c_" + uuid.NewString()[:8] + "@x.com"
	_ = tx.Create(&models.User{ID: clientID, Email: email, Role: models.RoleClient}).Error
	cs := models.Case{
		ID: uuid.New(), ClientID: clientID,
		Title: "T", Category: "Employment", Description: desc,
		Status: models.CaseOpen, CreatedAt: createdAt,
	}
	_ = tx.Create(&cs).Error
	return cs.ID
}

/* tests */
func Test_Marketplace_RedactsPreview(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		// lawyer user (viewer)
		lawyer := uuid.New()
		_ = tx.Create(&models.User{ID: lawyer, Email: "l@t", Role: models.RoleLawyer}).Error

		// case dgn PII
		_ = seedOpenCase(t, tx, "Hubungi saya di test@example.com 08123456789", time.Now())

		app := newTestApp(NewHandler(tx, nil), lawyer, string(models.RoleLawyer))
		req := httptest.NewRequest("GET", "/api/marketplace?page=1&pageSize=5", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("got %d", resp.StatusCode)
		}

		var out struct {
			Items []struct{ Preview string } `json:"items"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if len(out.Items) == 0 {
			t.Fatalf("no items")
		}
		if out.Items[0].Preview == "" || out.Items[0].Preview == sanitize.Summary("Hubungi saya di test@example.com 08123456789", 240) {
			t.Fatalf("preview should be redacted, got: %q", out.Items[0].Preview)
		}
	})
}

func Test_Marketplace_FilterCreatedSince_And_Pagination(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		lawyer := uuid.New()
		_ = tx.Create(&models.User{ID: lawyer, Email: "l2@t", Role: models.RoleLawyer}).Error

		// 2 kasus lama (8 hari lalu), 1 kasus baru (hari ini)
		eightDays := time.Now().AddDate(0, 0, -8)
		_ = seedOpenCase(t, tx, "old 1", eightDays)
		_ = seedOpenCase(t, tx, "old 2", eightDays)
		_ = seedOpenCase(t, tx, "new 1", time.Now())

		app := newTestApp(NewHandler(tx, nil), lawyer, string(models.RoleLawyer))

		// filter created_since = 7 hari lalu (Asia/Singapore)
		since := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
		req := httptest.NewRequest("GET", "/api/marketplace?page=1&pageSize=1&created_since="+since, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("got %d", resp.StatusCode)
		}

		var out struct {
			Total int64 `json:"total"`
			Items []any `json:"items"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)

		// seharusnya hanya kasus baru yang lewat filter 7 hari (total 1) dan dipotong pageSize=1
		if out.Total != 1 {
			t.Fatalf("want total=1 after filter, got %d", out.Total)
		}
		if len(out.Items) != 1 {
			t.Fatalf("want pageSize=1, got %d items", len(out.Items))
		}
	})
}

func Test_Marketplace_Redaction_HasMyQuote_CreatedSince(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		lawyer := uuid.New()
		_ = tx.Create(&models.User{ID: lawyer, Email: "lw_" + lawyer.String()[:6] + "@x.com", Role: models.RoleLawyer})

		// case A: yesterday (punya preview ber-PII)
		ownerA := uuid.New()
		_ = tx.Create(&models.User{ID: ownerA, Email: "oa_" + ownerA.String()[:6] + "@x.com", Role: models.RoleClient})
		csA := models.Case{
			ID:          uuid.New(),
			ClientID:    ownerA,
			Title:       "Case A",
			Category:    "Cat",
			Description: "Hub saya di test@example.com atau 08123456789",
			Status:      models.CaseOpen,
			CreatedAt:   time.Now().Add(-24 * time.Hour),
		}
		_ = tx.Create(&csA).Error

		// case B: today; lawyer sudah quote
		ownerB := uuid.New()
		_ = tx.Create(&models.User{ID: ownerB, Email: "ob_" + ownerB.String()[:6] + "@x.com", Role: models.RoleClient})
		csB := models.Case{
			ID:          uuid.New(),
			ClientID:    ownerB,
			Title:       "Case B",
			Category:    "Cat",
			Description: "No PII here",
			Status:      models.CaseOpen,
			CreatedAt:   time.Now(),
		}
		_ = tx.Create(&csB).Error
		_ = tx.Create(&models.Quote{
			CaseID: csB.ID, LawyerID: lawyer,
			AmountCents: 1000, Days: 1, Note: "yo",
			Status: models.QuoteProposed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}).Error

		h := NewHandler(tx, nil)
		app := newTestApp(h, lawyer, string(models.RoleLawyer))

		// a) tanpa filter → A dan B muncul; A.preview harus ter-redact; B.has_my_quote = true
		req1 := httptest.NewRequest("GET", "/api/marketplace?page=1&pageSize=50", nil)
		resp1, _ := app.Test(req1)
		if resp1.StatusCode != 200 {
			t.Fatalf("marketplace got %d", resp1.StatusCode)
		}
		var out1 struct {
			Items []struct {
				ID         string `json:"id"`
				Preview    string `json:"preview"`
				HasMyQuote bool   `json:"has_my_quote"`
			} `json:"items"`
		}
		_ = json.NewDecoder(resp1.Body).Decode(&out1)
		if len(out1.Items) < 2 {
			t.Fatalf("want >=2 items, got %d", len(out1.Items))
		}
		// cek redaksi
		for _, it := range out1.Items {
			if it.ID == csA.ID.String() {
				if strings.Contains(it.Preview, "@") || strings.Contains(it.Preview, "0812") {
					t.Fatalf("preview not redacted: %q", it.Preview)
				}
			}
			if it.ID == csB.ID.String() && !it.HasMyQuote {
				t.Fatalf("has_my_quote should be true for B")
			}
		}

		// b) filter created_since = today → hanya B
		today := time.Now().In(time.FixedZone("Asia/Singapore", 8*3600)).Format("2006-01-02")
		req2 := httptest.NewRequest("GET", "/api/marketplace?created_since="+today, nil)
		resp2, _ := app.Test(req2)
		if resp2.StatusCode != 200 {
			t.Fatalf("marketplace filter got %d", resp2.StatusCode)
		}
		var out2 struct {
			Items []struct{ ID string } `json:"items"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&out2)

		onlyB := len(out2.Items) == 1 && out2.Items[0].ID == csB.ID.String()
		if !onlyB {
			t.Fatalf("filter created_since should return only Case B, got %#v", out2.Items)
		}
	})
}

func Test_SignedURL_Lawyer_OnlyWhenEngagedAccepted(t *testing.T) {
	db := openTestDB(t)
	withTx(t, db, func(tx *gorm.DB) {
		seed := seedCase(t, tx, models.CaseEngaged)

		// buat accepted quote
		q := models.Quote{
			CaseID: seed.CaseID, LawyerID: seed.LawyerID,
			AmountCents: 1000, Days: 3, Note: "ok",
			Status: models.QuoteAccepted, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := tx.Create(&q).Error; err != nil {
			t.Fatal(err)
		}
		// set accepted ids di case
		if err := tx.Model(&models.Case{}).
			Where("id = ?", seed.CaseID).
			Updates(map[string]any{
				"accepted_quote_id":  q.ID,
				"accepted_lawyer_id": seed.LawyerID,
			}).Error; err != nil {
			t.Fatal(err)
		}

		// file
		f := models.CaseFile{
			CaseID:       seed.CaseID,
			Key:          "case/" + seed.CaseID.String() + "/doc.pdf",
			Mime:         "application/pdf",
			Size:         123,
			OriginalName: "Secret.pdf",
			CreatedAt:    time.Now(),
		}
		if err := tx.Create(&f).Error; err != nil {
			t.Fatal(err)
		}

		h := NewHandler(tx, nil)
		// accepted lawyer → 200
		appOK := newTestApp(h, seed.LawyerID, string(models.RoleLawyer))
		req1 := httptest.NewRequest("GET", "/api/files/"+f.ID.String()+"/signed-url", nil)
		resp1, _ := appOK.Test(req1)
		if resp1.StatusCode != 200 {
			t.Fatalf("accepted lawyer want 200, got %d", resp1.StatusCode)
		}

		// random lawyer → 403
		otherLawyer := uuid.New()
		_ = tx.Create(&models.User{ID: otherLawyer, Email: "oth_" + otherLawyer.String()[:6] + "@x.com", Role: models.RoleLawyer})
		app403 := newTestApp(h, otherLawyer, string(models.RoleLawyer))
		req2 := httptest.NewRequest("GET", "/api/files/"+f.ID.String()+"/signed-url", nil)
		resp2, _ := app403.Test(req2)
		if resp2.StatusCode != 403 {
			t.Fatalf("other lawyer want 403, got %d", resp2.StatusCode)
		}
	})
}
