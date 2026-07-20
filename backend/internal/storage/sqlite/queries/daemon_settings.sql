-- name: GetFleetPaused :one
SELECT fleet_paused FROM daemon_settings WHERE id = 1;

-- name: SetFleetPaused :exec
UPDATE daemon_settings SET fleet_paused = ? WHERE id = 1;
