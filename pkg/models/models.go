package models

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleClient Role = "client"
	RoleLawyer Role = "lawyer"
)

type CaseStatus string

const (
	CaseOpen      CaseStatus = "open"
	CaseEngaged   CaseStatus = "engaged"
	CaseClosed    CaseStatus = "closed"
	CaseCancelled CaseStatus = "cancelled"
)

type QuoteStatus string

const (
	QuoteProposed QuoteStatus = "proposed"
	QuoteAccepted QuoteStatus = "accepted"
	QuoteRejected QuoteStatus = "rejected"
)

type PayStatus string

const (
	PayInitiated PayStatus = "initiated"
	PayPaid      PayStatus = "paid"
	PayFailed    PayStatus = "failed"
)

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

type Case struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	ClientID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Title       string    `gorm:"not null"`
	Category    string    `gorm:"not null"`
	Description string
	Status      CaseStatus `gorm:"type:varchar(20);default:'open'"`
	CreatedAt   time.Time

	Files  []CaseFile
	Quotes []Quote

	EngagedAt        *time.Time // <— tambahkan
	AcceptedQuoteID  uuid.UUID  // <— tambahkan
	AcceptedLawyerID uuid.UUID  // <— tambahkan
}

type CaseFile struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CaseID       uuid.UUID `gorm:"type:uuid;not null;index"`
	Key          string    `gorm:"not null"`
	Mime         string    `gorm:"not null"`
	Size         int       `gorm:"not null"`
	OriginalName string
	CreatedAt    time.Time

	// Tambahan: agar Preload("Case") dan cf.Case.* valid
	Case Case `gorm:"foreignKey:CaseID;references:ID"`
}

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

type Payment struct {
	ID                  uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CaseID              uuid.UUID `gorm:"unique;not null"`
	QuoteID             uuid.UUID `gorm:"unique;not null"`
	ClientID            uuid.UUID `gorm:"type:uuid;not null"`
	StripeSessionID     string    `gorm:"unique;not null"`
	StripePaymentIntent string    `gorm:"unique"`
	AmountCents         int       `gorm:"not null"`
	Status              PayStatus `gorm:"type:varchar(20);default:'initiated'"`
	CreatedAt           time.Time
}
