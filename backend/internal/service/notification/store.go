package notification

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Store is the notification service's read persistence surface.
type Store interface {
	ListUnreadNotifications(ctx context.Context, limit int) ([]domain.NotificationRecord, error)
	ListUnreadNotificationsBefore(ctx context.Context, before time.Time, beforeID string, limit int) ([]domain.NotificationRecord, error)
	MarkNotificationRead(ctx context.Context, id string) (domain.NotificationRecord, bool, error)
	MarkAllNotificationsRead(ctx context.Context) ([]domain.NotificationRecord, error)
}
