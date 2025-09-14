package models

import (
	"time"

	"github.com/google/uuid"
)

/* =============================== Enums ================================== */

// Role defines the type of user in the system.
type Role string

const (
	RoleClient Role = "client"
	RoleLawyer Role = "lawyer"
)

// CaseStatus defines lifecycle states for a case.
type CaseStatus string

const (
	CaseOpen      CaseStatus = "open"
	CaseEngaged   CaseStatus = "engaged"
	CaseClosed    CaseStatus = "closed"
	CaseCancelled CaseStatus = "cancelled"
)

// QuoteStatus defines lifecycle states for a quote.
type QuoteStatus string

const (
	QuoteProposed QuoteStatus = "proposed"
	QuoteAccepted QuoteStatus = "accepted"
	QuoteRejected QuoteStatus = "rejected"
)

// PayStatus defines lifecycle states for a payment.
type PayStatus string

const (
	PayInitiated PayStatus = "initiated"
	PayPaid      PayStatus = "paid"
	PayFailed    PayStatus = "failed"
)

/* =============================== Entities =============================== */

// User represents a client or lawyer.
type User struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Email        string    `gorm:"uniqueIndex;not null"`
	PasswordHash string    `gorm:"not null"`
	Role         Role      `gorm:"type:varchar(20);not null"`
	Name         string
	Jurisdiction string
	BarNumber    string
	CreatedAt    time.Time
}

// Case represents a legal case created by a client.
type Case struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	ClientID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Title       string    `gorm:"not null"`
	Category    string    `gorm:"not null"`
	Description string
	Status      CaseStatus `gorm:"type:varchar(20);default:'open'"`
	CreatedAt   time.Time

	// Relations
	Files  []CaseFile
	Quotes []Quote

	// Metadata for engaged case
	EngagedAt        *time.Time
	AcceptedQuoteID  uuid.UUID
	AcceptedLawyerID uuid.UUID
}

// CaseFile represents a file uploaded to a case.
type CaseFile struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CaseID       uuid.UUID `gorm:"type:uuid;not null;index"`
	Key          string    `gorm:"not null"`
	Mime         string    `gorm:"not null"`
	Size         int       `gorm:"not null"`
	OriginalName string
	CreatedAt    time.Time

	// Relation back to case
	Case Case `gorm:"foreignKey:CaseID;references:ID"`
}

// Quote represents a lawyer’s proposal for a case.
type Quote struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CaseID      uuid.UUID `gorm:"type:uuid;not null;index:idx_case_lawyer,unique"`
	LawyerID    uuid.UUID `gorm:"type:uuid;not null;index:idx_case_lawyer,unique"`
	AmountCents int       `gorm:"not null"`
	Days        int       `gorm:"not null"`
	Note        string
	Status      QuoteStatus `gorm:"type:varchar(20);default:'proposed'"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Payment represents a payment attempt for a case’s accepted quote.
type Payment struct {
	ID                  uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CaseID              uuid.UUID `gorm:"type:uuid;not null"`
	QuoteID             uuid.UUID `gorm:"type:uuid;not null"`
	ClientID            uuid.UUID `gorm:"type:uuid;not null"`
	StripeSessionID     *string   `gorm:"uniqueIndex:ux_pay_session_filled"` // Stripe Checkout session (optional)
	StripePaymentIntent *string   `gorm:"uniqueIndex:ux_pay_intent_filled"`  // Stripe PaymentIntent (optional)
	AmountCents         int       `gorm:"not null"`                          // stored in cents to avoid float issues
	Status              PayStatus `gorm:"type:varchar(20);default:'initiated'"`
	CreatedAt           time.Time `gorm:"not null;default:now()"`
	UpdatedAt           time.Time `gorm:"not null;default:now()"`
}

// CaseHistory is an audit log entry for important case changes.
type CaseHistory struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CaseID    uuid.UUID  `gorm:"type:uuid;not null;index"`
	ActorID   uuid.UUID  `gorm:"type:uuid;not null;index"`  // who performed the action (client/lawyer/system)
	Action    string     `gorm:"type:varchar(50);not null"` // e.g. created, quote_submitted, accepted_quote, paid, cancelled, closed
	OldStatus CaseStatus `gorm:"type:varchar(20)"`
	NewStatus CaseStatus `gorm:"type:varchar(20)"`
	Reason    string     `gorm:"type:text"` // optional explanation/comment
	CreatedAt time.Time  `gorm:"autoCreateTime"`
}
