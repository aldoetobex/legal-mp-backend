# How it Works

> Also see the **[ERD](ERD.md)** for the data model.

## Roles

- **Client:** posts cases, uploads files, reviews quotes, accepts & pays.
- **Lawyer:** browses anonymized marketplace, submits/updates one quote per case, gains access only if accepted (engaged).

## Core Flows

### 1) Client
- **Create Case** — title, category, description, upload up to 10 files (PDF/PNG).  
  Files are stored in object storage; filenames are **masked** in API responses (SHA‑1 + original extension).
- **My Cases** — paginated list showing case status and **quote counts**.
- **Review Quotes** — see all quotes (amount, days, note). While the case is **OPEN**, quote `note` text is **PII‑redacted** (emails/phones). Once **ENGAGED**, original notes are visible.
- **Accept & Pay** — pick a quote, Stripe test checkout, server verifies payment and marks the case **ENGAGED** with the accepted quote; other quotes become **REJECTED**. Idempotent “accept” guards ensure exactly one winner.

### 2) Lawyer
- **Marketplace** — shows **OPEN** cases only; no client identity.  
  Description preview is **redacted** (emails/phones). Server‑side filters: `category`, `created_since` (ISO date, Asia/Singapore), plus pagination.
- **Submit/Update Quote** — one active quote per `(case, lawyer)`. Re‑submit updates your own quote until the case is engaged. Attempting to quote a non‑OPEN case returns **409/403**.
- **Access Files** — only after your quote is accepted and case is **ENGAGED**. Downloads use **short‑lived signed URLs**. Otherwise, no access.

## Security & Correctness

- **RBAC / Authorization**
  - Client can see and manage only their own cases/files.
  - Lawyer sees marketplace only; file access is blocked unless **ENGAGED** and **accepted** for that case.
- **File Safety**
  - Accepts only **PDF/PNG**, max **10** files, each ≤ **10MB**.
  - Stored object keys are unguessable; responses **mask original filenames**.
  - Files are never public; downloads go through **signed URLs** with short expiry.
- **PII Redaction**
  - Description previews (marketplace) and quote notes (while OPEN) strip emails/phone numbers.
- **Single‑Winner Accept (Atomic)**
  - Accept endpoint row‑locks the case, marks exactly one quote **ACCEPTED**, rejects others, transitions case to **ENGAGED**. Repeats are idempotent.
- **Server‑Driven Lists**
  - All pagination/filtering happen on the server (no dumping full datasets to the browser).

## Tests

Unit/integration tests exercise the flows above against a Postgres test DB (`TEST_DATABASE_URL`).  
See repository test files under `internal/cases` and `internal/quotes`.

- Case detail redaction & filename masking
- ListMine pagination with quote counts
- Signed URL gating (owner vs accepted lawyer vs random user)
- Marketplace redaction, created_since filter, pagination
- Quote upsert (non‑OPEN forbidden; update your own only)

> For the database schema, see **[ERD](ERD.md)**.