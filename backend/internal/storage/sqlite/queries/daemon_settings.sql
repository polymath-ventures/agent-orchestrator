-- name: GetFleetPaused :one
SELECT fleet_paused FROM daemon_settings WHERE id = 1;

-- name: SetFleetPaused :exec
UPDATE daemon_settings SET fleet_paused = ? WHERE id = 1;

-- name: GetPrimeSettingsJSON :one
SELECT prime_settings FROM daemon_settings WHERE id = 1;

-- name: SetPrimeSettingsJSON :exec
UPDATE daemon_settings SET prime_settings = ? WHERE id = 1;

-- name: GetSessionIDGeneration :one
SELECT session_id_generation FROM daemon_settings WHERE id = 1;
