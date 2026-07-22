-- name: CreateNotification :one
INSERT INTO notifications (
    id, session_id, project_id, pr_url, dedupe_key, type, title, body, status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListUnreadNotifications :many
SELECT *
FROM notifications
WHERE status = 'unread'
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListUnreadNotificationsBefore :many
SELECT *
FROM notifications
WHERE status = 'unread'
  AND (created_at < ? OR (created_at = ? AND id < ?))
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: MarkNotificationRead :one
UPDATE notifications
SET status = 'read'
WHERE id = ? AND status = 'unread'
RETURNING *;

-- name: MarkAllNotificationsRead :many
UPDATE notifications
SET status = 'read'
WHERE status = 'unread'
RETURNING *;

-- name: GetUnreadNotificationByDedupe :one
SELECT *
FROM notifications
WHERE type = ? AND dedupe_key = ? AND status = 'unread'
LIMIT 1;
