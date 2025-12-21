-- name: GetUserAuthByEmail :one
SELECT id, email, password_hash, role, is_active, last_login_at, created_at, updated_at
FROM users
WHERE email = $1
LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash, role, is_active)
VALUES ($1, $2, $3, $4)
RETURNING id, email, role, is_active, created_at, updated_at;

-- name: UpdateUserLastLogin :exec
UPDATE users
SET last_login_at = now(), updated_at = now()
WHERE id = $1;

-- name: CreateUserSession :one
INSERT INTO user_sessions (user_id, refresh_token_hash, user_agent, ip_address, expires_at)
VALUES ($1, $2, $3, $4::inet, $5)
RETURNING id, user_id, refresh_token_hash, created_at, expires_at, revoked_at;

-- name: GetActiveSessionByRefreshHash :one
SELECT id, user_id, refresh_token_hash, created_at, expires_at, revoked_at
FROM user_sessions
WHERE refresh_token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now()
LIMIT 1;

-- name: RevokeSessionByID :exec
UPDATE user_sessions
SET revoked_at = now()
WHERE id = $1
  AND revoked_at IS NULL;

-- name: RevokeAllActiveSessionsByUserID :exec
UPDATE user_sessions
SET revoked_at = now()
WHERE user_id = $1
  AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :exec
DELETE FROM user_sessions
WHERE expires_at <= $1;

-- name: GetUserByID :one
SELECT id, email, role, is_active, last_login_at, created_at, updated_at
FROM users
WHERE id = $1
LIMIT 1;

-- name: ListUsers :many
SELECT id, email, role, is_active, last_login_at, created_at, updated_at
FROM users
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: UpdateUserRole :exec
UPDATE users
SET role = $1, updated_at = now()
WHERE id = $2;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $1, updated_at = now()
WHERE id = $2;

-- name: UpdateUserActiveStatus :exec
UPDATE users
SET is_active = $1, updated_at = now()
WHERE id = $2;

-- name: GetActiveSessionsByUserID :many
SELECT id, user_agent, ip_address, created_at, expires_at
FROM user_sessions
WHERE user_id = $1
  AND revoked_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC;

-- name: RevokeSessionByRefreshHash :exec
UPDATE user_sessions
SET revoked_at = now()
WHERE refresh_token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: GetActiveSessionByRefreshHashForUpdate :one
SELECT id, user_id, refresh_token_hash, created_at, expires_at, revoked_at
FROM user_sessions
WHERE refresh_token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now()
FOR UPDATE;
