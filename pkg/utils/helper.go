package utils

import (
	"context"
	"time"

	"github.com/aldoetobex/legal-mp-backend/pkg/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LogCaseHistory inserts an audit record into case_histories.
// Used to track important status changes and actions on a case.
// Errors are ignored on purpose (best-effort logging).
func LogCaseHistory(
	ctx context.Context,
	db *gorm.DB,
	caseID, actorID uuid.UUID,
	action string,
	oldS, newS models.CaseStatus,
	reason string,
) {
	_ = db.WithContext(ctx).Create(&models.CaseHistory{
		CaseID:    caseID,
		ActorID:   actorID,
		Action:    action,
		OldStatus: oldS,
		NewStatus: newS,
		Reason:    reason,
		CreatedAt: time.Now(),
	}).Error
}
