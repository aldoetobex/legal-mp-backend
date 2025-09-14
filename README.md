# Legal Marketplace (Mini)

A small two‑sided app where **Clients** post legal cases (with attachments) and **Lawyers** browse anonymized listings and submit one quote per case. The **Client** accepts and pays; only the **accepted Lawyer** can access the full case and files.

> 📄 See: [How it Works](HOW_IT_WORKS.md) • [ERD](ERD.md)

---

## 🚀 Deployed URL

- **Web:** https://legal-mp-frontend.vercel.app
- **Api:** https://legal-mp-backend-production.up.railway.app/api
- **Swagger:** https://legal-mp-backend-production.up.railway.app/swagger

---

## 🧰 Tech Stack (suggested)

- **Backend:** Go + Fiber, GORM (PostgreSQL)
- **DB:** PostgreSQL
- **Object Storage:** Supabase (S3‑like), signed URLs
- **Payments:** Stripe (test mode)
- **Frontend:** Next.js/React (pages for Client & Lawyer)
- **Auth:** simple bearer/JWT (server reads user id/role from context in this repo’s tests)

---

## ⚙️ Environment (.env)

Create a `.env` from the example below or use the provided **[.env.example](.env.example)** file.

```bash
APP_ENV=dev
PORT=3001

PAYMENT_PROVIDER=stripe

STRIPE_CURRENCY=sgd   # atau sgd
PUBLIC_BASE_URL=http://localhost:3000

# Core
DATABASE_URL=postgres://user:pass@localhost:5432/legalmp?sslmode=disable
TEST_DATABASE_URL=postgres://user:pass@localhost:5432/legalmp_test?sslmode=disable

# Auth
JWT_SECRET=replace_me_with_a_long_random_string

# Object storage (Supabase)
SUPABASE_URL=https://<project>.supabase.co
SUPABASE_BUCKET=case-files
SUPABASE_ANON_KEY=sb_anon_key
SUPABASE_SERVICE_ROLE_KEY=sb_service_role_key

# Payments (Stripe - test)
STRIPE_SECRET_KEY=sk_test_xxx
STRIPE_WEBHOOK_SECRET=whsec_xxx
```

> For local dev, you can point `DATABASE_URL` to a Docker Postgres and run the API on `:8080`.

---

## 👤 Example Accounts (seed / for demo)

- **Client:** `client1@example.com` / `Passw0rd!`
- **Lawyer:** `lawyer1@example.com` / `Passw0rd!`

(Adjust to your seed data. In tests, we generate random emails per run.)

---

## 🧪 Tests (what we cover)

This repo includes **unit/integration tests** against a real Postgres (using `TEST_DATABASE_URL`) to verify the critical logic and security gates.

- **Cases**
  - Client case detail:
    - Redacts PII in quote notes while case is **OPEN**
    - Shows original quote notes when **ENGAGED**
  - File-list response **masks original filenames** with SHA‑1 (extension preserved)
  - `GET /cases/mine` pagination + **quote counts** per case
  - Signed URL access:
    - Client owner ✅
    - Lawyer only if **engaged & accepted** ✅
    - Random user ❌
  - Marketplace (lawyer view):
    - **Redacted** description previews
    - `created_since` filter (Asia/Singapore) + pagination
- **Quotes**
  - Upsert (one active quote per lawyer per case)
  - Cannot quote a **non‑OPEN** case (409/403)
  - Re‑submitting updates your own quote; others’ quotes untouched

> See **[How it Works](HOW_IT_WORKS.md)** for the security model and **[ERD](ERD.md)** for the schema.

Run tests:

```bash
export TEST_DATABASE_URL='postgres://user:pass@localhost:5432/legalmp_test?sslmode=disable'
go test ./internal/... -v
```

---

## 📝 How to Run (quickstart)

1. **Prepare Postgres** (two DBs suggested: app & test)
2. Copy **`.env.example` → `.env`** and fill values
3. **Run API** (example):
   ```bash
   go run ./cmd/server
   ```
4. **Run frontend** (Next.js):
   ```bash
   pnpm dev
   ```
5. Visit the deployed URL or `http://localhost:3000`

---

## 🧭 Project Structure (high‑level)

```
internal/
  cases/     # case handlers: create/list/detail, marketplace, files (upload/signed URL)
  quotes/    # quote upsert & list mine
  auth/      # auth helpers (user id/role from context)
  storage/   # Supabase wrapper for object storage
pkg/
  models/    # GORM models & enums
  sanitize/  # redaction helpers (emails/phones)
  validation/# request validation
```

---

## 📚 More

- **How it works:** [HOW_IT_WORKS.md](HOW_IT_WORKS.md)
- **Data model (ERD):** [ERD.md](ERD.md)