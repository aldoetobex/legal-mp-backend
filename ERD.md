# ERD (Entity Relationship Diagram)

> Back to **[How it Works](HOW_IT_WORKS.md)** or **[README](README.md)**.

```mermaid
erDiagram
  USERS ||--o{ CASES : "client_id"
  USERS ||--o{ QUOTES : "lawyer_id"
  CASES ||--o{ QUOTES : "case_id"
  CASES ||--o{ CASE_FILES : "case_id"
  CASES ||--o{ CASE_HISTORIES : "case_id"
  CASES ||--o| PAYMENTS : "case_id"
  QUOTES ||--o| PAYMENTS : "accepted_quote_id (via case)"
  USERS ||--o| CASES : "accepted_lawyer_id (nullable)"

  USERS {
    uuid id PK
    text email UNIQUE
    text password_hash
    text role  "client|lawyer"
    text name
    text jurisdiction
    text bar_number
    timestamptz created_at
  }

  CASES {
    uuid id PK
    uuid client_id FK -> USERS.id
    text title
    text category
    text description
    text status "open|engaged|closed|cancelled"
    uuid accepted_quote_id NULL
    uuid accepted_lawyer_id NULL
    timestamptz created_at
    timestamptz engaged_at NULL
  }

  QUOTES {
    uuid id PK
    uuid case_id FK -> CASES.id
    uuid lawyer_id FK -> USERS.id
    int  amount_cents
    int  days
    text note
    text status "proposed|accepted|rejected"
    timestamptz created_at
    timestamptz updated_at
  }

  CASE_FILES {
    uuid id PK
    uuid case_id FK -> CASES.id
    text key        "object storage path"
    text mime
    int  size
    text original_name "masked in API responses"
    timestamptz created_at
  }

  CASE_HISTORIES {
    uuid id PK
    uuid case_id FK -> CASES.id
    uuid actor_id FK -> USERS.id
    text action      "created|cancelled|closed|accepted..."
    text reason
    text old_status
    text new_status
    timestamptz created_at
  }

  PAYMENTS {
    uuid id PK
    uuid case_id FK -> CASES.id
    text provider     "stripe"
    text provider_ref "payment intent id"
    int  amount_cents
    text currency
    text status       "succeeded|failed|..."
    timestamptz created_at
  }
```