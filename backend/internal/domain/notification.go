package domain

import (
	"errors"
	"strings"
	"time"
)

// NotificationType identifies a user-facing notification kind persisted for the dashboard.
type NotificationType string

const (
	// NotificationNeedsInput means an agent session is waiting for user input.
	NotificationNeedsInput NotificationType = "needs_input"
	// NotificationReadyToMerge means a PR has no known merge blockers.
	NotificationReadyToMerge NotificationType = "ready_to_merge"
	// NotificationPRMerged means a tracked PR was merged.
	NotificationPRMerged NotificationType = "pr_merged"
	// NotificationPRClosedUnmerged means a tracked PR closed without merging.
	NotificationPRClosedUnmerged NotificationType = "pr_closed_unmerged"
	// NotificationLowQuota means a subscription harness is nearing its known quota window.
	NotificationLowQuota NotificationType = "low_quota"
)

// Valid reports whether t is one of the v1 notification kinds.
func (t NotificationType) Valid() bool {
	switch t {
	case NotificationNeedsInput, NotificationReadyToMerge, NotificationPRMerged, NotificationPRClosedUnmerged, NotificationLowQuota:
		return true
	default:
		return false
	}
}

// NotificationStatus is the read state for a stored notification.
type NotificationStatus string

const (
	// NotificationUnread marks a notification that has not been acknowledged.
	NotificationUnread NotificationStatus = "unread"
	// NotificationRead marks a notification that has been acknowledged.
	NotificationRead NotificationStatus = "read"
)

// Valid reports whether s is a supported notification read state.
func (s NotificationStatus) Valid() bool {
	switch s {
	case NotificationUnread, NotificationRead:
		return true
	default:
		return false
	}
}

// NotificationRecord is the durable notification persistence shape.
type NotificationRecord struct {
	ID        string
	SessionID SessionID
	ProjectID ProjectID
	PRURL     string
	DedupeKey string
	Type      NotificationType
	Title     string
	Body      string
	Status    NotificationStatus
	CreatedAt time.Time
}

var (
	// ErrInvalidNotificationType reports an unknown notification type.
	ErrInvalidNotificationType = errors.New("invalid notification type")
	// ErrInvalidNotificationStatus reports an unknown notification status.
	ErrInvalidNotificationStatus = errors.New("invalid notification status")
	// ErrInvalidNotificationRecord reports a missing required notification field.
	ErrInvalidNotificationRecord = errors.New("invalid notification record")
)

// Validate checks the required fields and enum values for a stored notification.
func (r NotificationRecord) Validate() error {
	if r.Type == NotificationLowQuota {
		if r.DedupeKey == "" || r.Title == "" || r.CreatedAt.IsZero() {
			return ErrInvalidNotificationRecord
		}
	} else if r.SessionID == "" || r.ProjectID == "" || r.Title == "" || r.CreatedAt.IsZero() {
		return ErrInvalidNotificationRecord
	}
	if !r.Type.Valid() {
		return ErrInvalidNotificationType
	}
	if !r.Status.Valid() {
		return ErrInvalidNotificationStatus
	}
	return nil
}

// NotificationDedupeKey derives the canonical unread-dedupe key for a
// notification record. An explicit key wins so producers can scope alerts to
// provider-specific subjects such as quota windows.
func NotificationDedupeKey(r NotificationRecord) string {
	if r.DedupeKey != "" {
		return r.DedupeKey
	}
	var parts []string
	if r.ProjectID != "" {
		parts = append(parts, "project", string(r.ProjectID))
	}
	if r.SessionID != "" {
		parts = append(parts, "session", string(r.SessionID))
	}
	if r.PRURL != "" {
		parts = append(parts, "pr", r.PRURL)
	}
	return strings.Join(parts, ":")
}
