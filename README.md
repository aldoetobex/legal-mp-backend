# ⚖️ Legal Marketplace - Backend (Go + Fiber)

A small backend service for a legal two-sided marketplace:

-   **Clients** post legal cases (with attachments).\
-   **Lawyers** browse anonymized listings and submit **one quote per
    case**.\
-   **Client** accepts a quote and pays.\
-   Only the **accepted Lawyer** can access the full case and files.

> 📄 See: [How it Works](HOW_IT_WORKS.md) • [ERD](ERD.md)

------------------------------------------------------------------------

## 🚀 Deployed URL (demo)

-   **API:** https://legal-mp-backend-production.up.railway.app/api\
-   **Swagger docs:**
    https://legal-mp-backend-production.up.railway.app/swagger

------------------------------------------------------------------------

## 🧰 Tech Stack

-   **Language:** Go 1.22+\
-   **Framework:** Fiber\
-   **ORM:** GORM (PostgreSQL)\
-   **Database:** PostgreSQL (optimized for Supabase)\
-   **Storage:** Supabase (S3-like, signed URLs)\
-   **Payments:** Stripe (test mode)\
-   **Auth:** JWT / Bearer token (simulated in tests)\
-   **Deployment:** Railway (backend), Supabase (database + storage)

------------------------------------------------------------------------

## ⚙️ Environment Variables

Create a `.env` file based on **[.env.example](.env.example)**.

Example:

``` bash
APP_ENV=dev
PORT=3001

PAYMENT_PROVIDER=stripe

STRIPE_CURRENCY=sgd
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

------------------------------------------------------------------------

## 👩‍💻 Prerequisites

1.  **Install Go**\
    Make sure you have Go 1.22 or newer:

    ``` bash
    go version
    ```

    If not installed, download from <https://go.dev/dl>.

2.  **Install PostgreSQL**\
    Ensure Postgres is running locally or via Docker.

    -   One DB for app (`DATABASE_URL`)\
    -   One DB for tests (`TEST_DATABASE_URL`)

3.  **Stripe account (test mode)** if you want to test payments.

------------------------------------------------------------------------

## ▶️ How to Run

1.  **Clone the repo**

    ``` bash
    git clone https://github.com/aldoetobex/legal-mp-backend.git
    cd legal-mp-backend
    ```

2.  **Setup environment**

    ``` bash
    cp .env.example .env
    # edit .env with your Postgres, Stripe, Supabase credentials
    ```

3.  **Install dependencies**

    ``` bash
    go mod tidy
    ```

4.  **Run the server**

    ``` bash
    go run ./cmd/server
    ```

    By default the API runs on <http://localhost:3001>.

------------------------------------------------------------------------

## 🧪 Run Tests

This project includes **unit & integration tests** (using
`TEST_DATABASE_URL`).

1.  Ensure your **test database** exists.\

2.  Run tests with:

    ``` bash
    export TEST_DATABASE_URL='postgres://user:pass@localhost:5432/legalmp_test?sslmode=disable'
    go test ./internal/... -v
    ```

------------------------------------------------------------------------

## 🧭 Project Structure

    cmd/
      server/         # Entry point for API server
    internal/
      cases/          # Case handlers (create, list, detail, file upload)
      quotes/         # Quote upsert & listing
      payments/       # Stripe & mock payment flow
      auth/           # JWT / auth helpers
      storage/        # Supabase wrapper (signed URLs, upload, delete)
    pkg/
      models/         # GORM models & enums
      sanitize/       # Redaction helpers (emails, phones)
      validation/     # Request validation helpers
      utils/          # Shared utilities (logging, case history)

------------------------------------------------------------------------

## 👤 Example Accounts (for demo)

-   **Client:** `client1@example.com` / `Passw0rd!`\
-   **Lawyer:** `lawyer1@example.com` / `Passw0rd!`

*(adjust to your seed data; in tests, random users are generated
automatically).*

------------------------------------------------------------------------

## 📚 More

-   **How it works:** [HOW_IT_WORKS.md](HOW_IT_WORKS.md)\
-   **Database ERD:** [ERD.md](ERD.md)
