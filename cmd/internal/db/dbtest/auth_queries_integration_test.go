// Purpose: Integration tests for user-related SQLC queries against a real PostgreSQL database.
// These tests protect against SQL syntax errors, constraint violations, and data mapping issues
// that cannot be caught by unit tests with mocked database interfaces.

//go:build integration

package dbtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

// ============================================================================
// Test Suite Setup (single container for all tests via TestMain)
// ============================================================================

// testDB holds shared database resources for all integration tests.
// Initialized once in TestMain, reused across all test functions.
var (
	testDB        *sql.DB
	testContainer *testutil.PostgresContainer
	testQueries   *db.Queries
)

// TestMain sets up a single PostgreSQL container for all tests in this package.
// This avoids creating ~30 containers (one per test function) which causes
// massive slowdown and potential hangs.
func TestMain(m *testing.M) {
	var err error
	testDB, testContainer, err = testutil.SetupTestDatabaseNoT()
	if err != nil {
		log.Fatalf("Failed to setup test database: %v", err)
	}

	err = testutil.RunMigrationsNoT(testDB)
	if err != nil {
		testutil.TeardownTestDatabaseNoT(testDB, testContainer)
		log.Fatalf("Failed to run migrations: %v", err)
	}

	testQueries = db.New(testDB)

	// Run all tests
	code := m.Run()

	// Cleanup
	testutil.TeardownTestDatabaseNoT(testDB, testContainer)
	os.Exit(code)
}

// cleanupUsers truncates users and user_sessions tables between subtests.
func cleanupUsers(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testDB.ExecContext(ctx, "TRUNCATE TABLE user_sessions CASCADE")
	require.NoError(t, err)
	_, err = testDB.ExecContext(ctx, "TRUNCATE TABLE users CASCADE")
	require.NoError(t, err)
}

// createTestUserInDB is a helper that creates a user directly in the DB and returns the result.
func createTestUserInDB(t *testing.T, queries *db.Queries, email, role string, isActive bool) db.CreateUserRow {
	t.Helper()
	user, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:        email,
		PasswordHash: testutil.TestPasswordHash,
		Role:         role,
		IsActive:     isActive,
	})
	require.NoError(t, err)
	return user
}

// validRefreshTokenHash generates a valid 64-char hex hash string for testing.
func validRefreshTokenHash(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(hash[:])
}

// testInet creates a valid pqtype.Inet for testing.
func testInet(ip string) pqtype.Inet {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return pqtype.Inet{}
	}
	bits := 128
	if parsed.To4() != nil {
		bits = 32
	}
	return pqtype.Inet{
		IPNet: net.IPNet{
			IP:   parsed,
			Mask: net.CIDRMask(bits, bits),
		},
		Valid: true,
	}
}

// ============================================================================
// SECTION 1: User CRUD Queries
// ============================================================================

func TestIntegration_CreateUser_Success(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: valid user parameters
	params := db.CreateUserParams{
		Email:        "admin@example.com",
		PasswordHash: testutil.TestPasswordHash,
		Role:         "admin",
		IsActive:     true,
	}

	// When: creating the user
	user, err := queries.CreateUser(context.Background(), params)

	// Then: user is created with correct fields and auto-generated timestamps
	require.NoError(t, err)
	assert.Positive(t, user.ID)
	assert.Equal(t, "admin@example.com", user.Email)
	assert.Equal(t, "admin", user.Role)
	assert.True(t, user.IsActive)
	assert.False(t, user.CreatedAt.IsZero(), "created_at should be set")
	assert.False(t, user.UpdatedAt.IsZero(), "updated_at should be set")
}

func TestIntegration_CreateUser_AllRoles(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: all valid roles
	roles := []string{"admin", "operator", "viewer"}

	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			email := fmt.Sprintf("%s@example.com", role)
			user, err := queries.CreateUser(context.Background(), db.CreateUserParams{
				Email:        email,
				PasswordHash: testutil.TestPasswordHash,
				Role:         role,
				IsActive:     true,
			})

			require.NoError(t, err)
			assert.Equal(t, role, user.Role)
		})
	}
}

func TestIntegration_CreateUser_DuplicateEmail(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an existing user
	createTestUserInDB(t, queries, "dup@example.com", "admin", true)

	// When: creating another user with the same email
	_, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:        "dup@example.com",
		PasswordHash: testutil.TestPasswordHash,
		Role:         "viewer",
		IsActive:     true,
	})

	// Then: unique constraint violation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uq_users_email")
}

func TestIntegration_CreateUser_InvalidRole(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// When: creating a user with an invalid role
	_, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:        "bad-role@example.com",
		PasswordHash: testutil.TestPasswordHash,
		Role:         "superadmin",
		IsActive:     true,
	})

	// Then: check constraint violation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chk_users_role")
}

func TestIntegration_CreateUser_EmailNormalizationConstraint(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: DB enforces CHECK (email = LOWER(BTRIM(email)))
	testCases := []struct {
		name  string
		email string
	}{
		{"uppercase", "Admin@Example.COM"},
		{"leading_whitespace", " admin@example.com"},
		{"trailing_whitespace", "admin@example.com "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := queries.CreateUser(context.Background(), db.CreateUserParams{
				Email:        tc.email,
				PasswordHash: testutil.TestPasswordHash,
				Role:         "viewer",
				IsActive:     true,
			})

			// Then: constraint violation because email is not normalized
			require.Error(t, err, "email %q should be rejected by DB constraint", tc.email)
			assert.Contains(t, err.Error(), "chk_users_email_normalized")
		})
	}
}

func TestIntegration_CreateUser_InactiveByDefault(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// When: creating a user with is_active = false
	user := createTestUserInDB(t, queries, "inactive@example.com", "viewer", false)

	// Then: the user is inactive
	assert.False(t, user.IsActive)
}

// ============================================================================
// SECTION 2: GetUserAuthByEmail
// ============================================================================

func TestIntegration_GetUserAuthByEmail_Success(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an existing user
	created := createTestUserInDB(t, queries, "auth@example.com", "admin", true)

	// When: retrieving user by email
	user, err := queries.GetUserAuthByEmail(context.Background(), "auth@example.com")

	// Then: full user record is returned including password_hash
	require.NoError(t, err)
	assert.Equal(t, created.ID, user.ID)
	assert.Equal(t, "auth@example.com", user.Email)
	assert.Equal(t, testutil.TestPasswordHash, user.PasswordHash)
	assert.Equal(t, "admin", user.Role)
	assert.True(t, user.IsActive)
	assert.False(t, user.LastLoginAt.Valid, "last_login_at should be NULL for new user")
}

func TestIntegration_GetUserAuthByEmail_NotFound(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// When: querying for non-existent email
	_, err := queries.GetUserAuthByEmail(context.Background(), "nonexistent@example.com")

	// Then: sql.ErrNoRows
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestIntegration_GetUserAuthByEmail_CaseSensitive(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with lowercase email
	createTestUserInDB(t, queries, "user@example.com", "viewer", true)

	// When: querying with different case
	_, err := queries.GetUserAuthByEmail(context.Background(), "User@Example.com")

	// Then: not found (DB stores lowercase, query is case-sensitive by default)
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

// ============================================================================
// SECTION 3: GetUserByID
// ============================================================================

func TestIntegration_GetUserByID_Success(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an existing user
	created := createTestUserInDB(t, queries, "byid@example.com", "operator", true)

	// When: retrieving by ID
	user, err := queries.GetUserByID(context.Background(), created.ID)

	// Then: user returned WITHOUT password_hash (different return type)
	require.NoError(t, err)
	assert.Equal(t, created.ID, user.ID)
	assert.Equal(t, "byid@example.com", user.Email)
	assert.Equal(t, "operator", user.Role)
	assert.True(t, user.IsActive)
}

func TestIntegration_GetUserByID_NotFound(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// When: querying for non-existent ID
	_, err := queries.GetUserByID(context.Background(), 999999)

	// Then: sql.ErrNoRows
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

// ============================================================================
// SECTION 4: ListUsers
// ============================================================================

func TestIntegration_ListUsers_Pagination(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: 5 users created (ordered by created_at DESC in output)
	for i := 1; i <= 5; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		createTestUserInDB(t, queries, email, "viewer", true)
		// Small delay to ensure distinct created_at for ordering
		time.Sleep(10 * time.Millisecond)
	}

	// When: listing first page (limit=2, offset=0)
	page1, err := queries.ListUsers(context.Background(), db.ListUsersParams{
		Limit:  2,
		Offset: 0,
	})

	// Then: 2 users returned, most recent first
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.Equal(t, "user5@example.com", page1[0].Email) // Most recent
	assert.Equal(t, "user4@example.com", page1[1].Email)

	// When: listing second page
	page2, err := queries.ListUsers(context.Background(), db.ListUsersParams{
		Limit:  2,
		Offset: 2,
	})
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.Equal(t, "user3@example.com", page2[0].Email)
	assert.Equal(t, "user2@example.com", page2[1].Email)

	// When: listing last page
	page3, err := queries.ListUsers(context.Background(), db.ListUsersParams{
		Limit:  2,
		Offset: 4,
	})
	require.NoError(t, err)
	assert.Len(t, page3, 1) // Only 1 user left
	assert.Equal(t, "user1@example.com", page3[0].Email)
}

func TestIntegration_ListUsers_EmptyResult(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// When: listing with no users in DB
	users, err := queries.ListUsers(context.Background(), db.ListUsersParams{
		Limit:  10,
		Offset: 0,
	})

	// Then: empty slice, not nil
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestIntegration_ListUsers_OffsetBeyondTotal(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: 2 users
	createTestUserInDB(t, queries, "a@example.com", "viewer", true)
	createTestUserInDB(t, queries, "b@example.com", "viewer", true)

	// When: offset way beyond total
	users, err := queries.ListUsers(context.Background(), db.ListUsersParams{
		Limit:  10,
		Offset: 100,
	})

	// Then: empty result
	require.NoError(t, err)
	assert.Empty(t, users)
}

// ============================================================================
// SECTION 5: UpdateUser operations
// ============================================================================

func TestIntegration_UpdateUserLastLogin(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with NULL last_login_at
	created := createTestUserInDB(t, queries, "login@example.com", "admin", true)

	beforeUpdate, err := queries.GetUserByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.False(t, beforeUpdate.LastLoginAt.Valid, "last_login_at should be NULL initially")

	// When: updating last login
	err = queries.UpdateUserLastLogin(context.Background(), created.ID)
	require.NoError(t, err)

	// Then: last_login_at is set and updated_at is refreshed
	afterUpdate, err := queries.GetUserByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.True(t, afterUpdate.LastLoginAt.Valid, "last_login_at should be set after update")
	assert.True(t, afterUpdate.UpdatedAt.After(beforeUpdate.UpdatedAt) || afterUpdate.UpdatedAt.Equal(beforeUpdate.UpdatedAt),
		"updated_at should be refreshed")
}

func TestIntegration_UpdateUserRole(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with viewer role
	created := createTestUserInDB(t, queries, "role@example.com", "viewer", true)

	// When: promoting to admin
	err := queries.UpdateUserRole(context.Background(), db.UpdateUserRoleParams{
		Role: "admin",
		ID:   created.ID,
	})
	require.NoError(t, err)

	// Then: role is updated
	updated, err := queries.GetUserByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "admin", updated.Role)
}

func TestIntegration_UpdateUserRole_InvalidRole(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an existing user
	created := createTestUserInDB(t, queries, "badrole@example.com", "viewer", true)

	// When: updating to invalid role
	err := queries.UpdateUserRole(context.Background(), db.UpdateUserRoleParams{
		Role: "superadmin",
		ID:   created.ID,
	})

	// Then: check constraint violation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chk_users_role")
}

func TestIntegration_UpdateUserPassword(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an existing user
	created := createTestUserInDB(t, queries, "pwd@example.com", "admin", true)
	newHash := "$2a$10$abcdefghijklmnopqrstuvwxyz012345678901234567890123456"

	// When: updating password
	err := queries.UpdateUserPassword(context.Background(), db.UpdateUserPasswordParams{
		PasswordHash: newHash,
		ID:           created.ID,
	})
	require.NoError(t, err)

	// Then: password hash is updated
	user, err := queries.GetUserAuthByEmail(context.Background(), "pwd@example.com")
	require.NoError(t, err)
	assert.Equal(t, newHash, user.PasswordHash)
}

func TestIntegration_UpdateUserActiveStatus(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an active user
	created := createTestUserInDB(t, queries, "active@example.com", "admin", true)

	// When: deactivating the user
	err := queries.UpdateUserActiveStatus(context.Background(), db.UpdateUserActiveStatusParams{
		IsActive: false,
		ID:       created.ID,
	})
	require.NoError(t, err)

	// Then: user is inactive
	user, err := queries.GetUserByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.False(t, user.IsActive)

	// When: reactivating the user
	err = queries.UpdateUserActiveStatus(context.Background(), db.UpdateUserActiveStatusParams{
		IsActive: true,
		ID:       created.ID,
	})
	require.NoError(t, err)

	// Then: user is active again
	user, err = queries.GetUserByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.True(t, user.IsActive)
}

// ============================================================================
// SECTION 6: User Sessions
// ============================================================================

func TestIntegration_CreateUserSession_Success(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an existing user
	user := createTestUserInDB(t, queries, "session@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("token-1")

	// When: creating a session
	session, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{String: "Mozilla/5.0 TestBrowser", Valid: true},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("192.168.1.1"),
	})

	// Then: session is created with correct fields
	require.NoError(t, err)
	assert.Positive(t, session.ID)
	assert.Equal(t, user.ID, session.UserID)
	assert.Equal(t, tokenHash, session.RefreshTokenHash)
	assert.False(t, session.CreatedAt.IsZero())
	assert.False(t, session.RevokedAt.Valid, "new session should not be revoked")
}

func TestIntegration_CreateUserSession_NullUserAgent(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user
	user := createTestUserInDB(t, queries, "null-ua@example.com", "viewer", true)

	// When: creating session with NULL user agent
	session, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: validRefreshTokenHash("null-ua-token"),
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})

	// Then: session is created successfully
	require.NoError(t, err)
	assert.Positive(t, session.ID)
}

func TestIntegration_CreateUserSession_InvalidTokenHashFormat(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user
	user := createTestUserInDB(t, queries, "bad-hash@example.com", "viewer", true)

	// When: creating session with invalid token hash (not 64 hex chars)
	_, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: "not-a-valid-hash",
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})

	// Then: check constraint violation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chk_user_sessions_refresh_hash_hex")
}

func TestIntegration_CreateUserSession_DuplicateTokenHash(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with an existing session
	user := createTestUserInDB(t, queries, "dup-token@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("dup-token")

	_, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	// When: creating another session with the same token hash
	_, err = queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(48 * time.Hour),
		IpAddress:        testInet("10.0.0.2"),
	})

	// Then: unique constraint violation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uq_user_sessions_refresh_hash")
}

func TestIntegration_CreateUserSession_ExpiresBeforeCreated(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user
	user := createTestUserInDB(t, queries, "expired-session@example.com", "viewer", true)

	// When: creating session with expires_at in the past (before created_at)
	_, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: validRefreshTokenHash("past-expiry"),
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(-1 * time.Hour), // in the past
		IpAddress:        testInet("10.0.0.1"),
	})

	// Then: check constraint expires_at > created_at
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chk_user_sessions_expires_at")
}

func TestIntegration_CreateUserSession_CascadeDeleteOnUser(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with a session
	user := createTestUserInDB(t, queries, "cascade@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("cascade-token")

	session, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	// When: deleting the user directly (CASCADE should remove sessions)
	_, err = testDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	require.NoError(t, err)

	// Then: session is also deleted
	_, err = queries.GetActiveSessionByRefreshHash(context.Background(), tokenHash)
	assert.ErrorIs(t, err, sql.ErrNoRows, "session should be deleted via CASCADE")

	// Double check with raw query
	var count int
	err = testDB.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM user_sessions WHERE id = $1", session.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ============================================================================
// SECTION 7: Session Retrieval
// ============================================================================

func TestIntegration_GetActiveSessionByRefreshHash_Success(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with an active session
	user := createTestUserInDB(t, queries, "get-session@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("active-session")

	created, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{String: "TestBrowser", Valid: true},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("192.168.1.100"),
	})
	require.NoError(t, err)

	// When: retrieving session by hash
	session, err := queries.GetActiveSessionByRefreshHash(context.Background(), tokenHash)

	// Then: session is returned
	require.NoError(t, err)
	assert.Equal(t, created.ID, session.ID)
	assert.Equal(t, user.ID, session.UserID)
	assert.Equal(t, tokenHash, session.RefreshTokenHash)
	assert.False(t, session.RevokedAt.Valid)
}

func TestIntegration_GetActiveSessionByRefreshHash_NotFound(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// When: querying non-existent hash
	_, err := queries.GetActiveSessionByRefreshHash(context.Background(),
		validRefreshTokenHash("nonexistent"))

	// Then: sql.ErrNoRows
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestIntegration_GetActiveSessionByRefreshHash_RevokedSession(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a revoked session
	user := createTestUserInDB(t, queries, "revoked@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("revoked-session")

	created, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	err = queries.RevokeSessionByID(context.Background(), created.ID)
	require.NoError(t, err)

	// When: trying to get the revoked session
	_, err = queries.GetActiveSessionByRefreshHash(context.Background(), tokenHash)

	// Then: not found (revoked sessions are excluded by WHERE clause)
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestIntegration_GetActiveSessionByRefreshHashForUpdate(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an active session
	user := createTestUserInDB(t, queries, "for-update@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("for-update-token")

	created, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	// When: retrieving with FOR UPDATE (within a transaction)
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	txQueries := db.New(tx)
	session, err := txQueries.GetActiveSessionByRefreshHashForUpdate(context.Background(), tokenHash)

	// Then: session returned (same data as non-locking version)
	require.NoError(t, err)
	assert.Equal(t, created.ID, session.ID)
	assert.Equal(t, user.ID, session.UserID)
}

func TestIntegration_GetActiveSessionsByUserID(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with 3 sessions (2 active, 1 revoked)
	user := createTestUserInDB(t, queries, "multi-session@example.com", "admin", true)

	s1, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: validRefreshTokenHash("session-1"),
		UserAgent:        sql.NullString{String: "Chrome", Valid: true},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("192.168.1.1"),
	})
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	_, err = queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: validRefreshTokenHash("session-2"),
		UserAgent:        sql.NullString{String: "Firefox", Valid: true},
		ExpiresAt:        time.Now().Add(48 * time.Hour),
		IpAddress:        testInet("192.168.1.2"),
	})
	require.NoError(t, err)

	// Revoke session 1
	err = queries.RevokeSessionByID(context.Background(), s1.ID)
	require.NoError(t, err)

	// Session 3 (active)
	_, err = queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: validRefreshTokenHash("session-3"),
		UserAgent:        sql.NullString{String: "Safari", Valid: true},
		ExpiresAt:        time.Now().Add(72 * time.Hour),
		IpAddress:        testInet("192.168.1.3"),
	})
	require.NoError(t, err)

	// When: listing active sessions
	sessions, err := queries.GetActiveSessionsByUserID(context.Background(), user.ID)

	// Then: only 2 active sessions returned (session-1 was revoked), ordered by created_at DESC
	require.NoError(t, err)
	assert.Len(t, sessions, 2)
	// Most recent first
	assert.Equal(t, "Safari", sessions[0].UserAgent.String)
	assert.Equal(t, "Firefox", sessions[1].UserAgent.String)
}

func TestIntegration_GetActiveSessionsByUserID_NoSessions(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with no sessions
	user := createTestUserInDB(t, queries, "no-sessions@example.com", "viewer", true)

	// When: listing sessions
	sessions, err := queries.GetActiveSessionsByUserID(context.Background(), user.ID)

	// Then: empty slice
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

// ============================================================================
// SECTION 8: Session Revocation
// ============================================================================

func TestIntegration_RevokeSessionByID(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an active session
	user := createTestUserInDB(t, queries, "revoke-id@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("revoke-by-id")

	session, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	// When: revoking by ID
	err = queries.RevokeSessionByID(context.Background(), session.ID)
	require.NoError(t, err)

	// Then: session is no longer active
	_, err = queries.GetActiveSessionByRefreshHash(context.Background(), tokenHash)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// Verify revoked_at is set
	var revokedAt sql.NullTime
	err = testDB.QueryRowContext(context.Background(),
		"SELECT revoked_at FROM user_sessions WHERE id = $1", session.ID).Scan(&revokedAt)
	require.NoError(t, err)
	assert.True(t, revokedAt.Valid, "revoked_at should be set")
}

func TestIntegration_RevokeSessionByID_AlreadyRevoked(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a revoked session
	user := createTestUserInDB(t, queries, "double-revoke@example.com", "admin", true)

	session, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: validRefreshTokenHash("double-revoke"),
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	err = queries.RevokeSessionByID(context.Background(), session.ID)
	require.NoError(t, err)

	// When: revoking again (idempotent — WHERE revoked_at IS NULL won't match)
	err = queries.RevokeSessionByID(context.Background(), session.ID)

	// Then: no error (no rows affected, but exec doesn't fail)
	assert.NoError(t, err)
}

func TestIntegration_RevokeSessionByRefreshHash(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: an active session
	user := createTestUserInDB(t, queries, "revoke-hash@example.com", "admin", true)
	tokenHash := validRefreshTokenHash("revoke-by-hash")

	_, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: tokenHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	// When: revoking by refresh hash
	err = queries.RevokeSessionByRefreshHash(context.Background(), tokenHash)
	require.NoError(t, err)

	// Then: session is no longer active
	_, err = queries.GetActiveSessionByRefreshHash(context.Background(), tokenHash)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestIntegration_RevokeAllActiveSessionsByUserID(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with 3 active sessions
	user := createTestUserInDB(t, queries, "revoke-all@example.com", "admin", true)

	for i := 1; i <= 3; i++ {
		_, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
			UserID:           user.ID,
			RefreshTokenHash: validRefreshTokenHash(fmt.Sprintf("revoke-all-%d", i)),
			UserAgent:        sql.NullString{Valid: false},
			ExpiresAt:        time.Now().Add(24 * time.Hour),
			IpAddress:        testInet("10.0.0.1"),
		})
		require.NoError(t, err)
	}

	// Verify 3 active sessions
	sessions, err := queries.GetActiveSessionsByUserID(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Len(t, sessions, 3)

	// When: revoking all sessions
	err = queries.RevokeAllActiveSessionsByUserID(context.Background(), user.ID)
	require.NoError(t, err)

	// Then: no active sessions remain
	sessions, err = queries.GetActiveSessionsByUserID(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

// ============================================================================
// SECTION 9: DeleteExpiredSessions
// ============================================================================

func TestIntegration_DeleteExpiredSessions(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries

	// Given: a user with 2 sessions — one expired (via direct SQL), one active
	user := createTestUserInDB(t, queries, "expire@example.com", "admin", true)

	// Active session (expires in the future)
	activeHash := validRefreshTokenHash("active-session-for-expire")
	_, err := queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: activeHash,
		UserAgent:        sql.NullString{Valid: false},
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		IpAddress:        testInet("10.0.0.1"),
	})
	require.NoError(t, err)

	// Insert an already-expired session via raw SQL (can't create with expires_at in the past via SQLC due to constraint)
	expiredHash := validRefreshTokenHash("expired-session-for-expire")
	_, err = testDB.ExecContext(context.Background(),
		`INSERT INTO user_sessions (user_id, refresh_token_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4)`,
		user.ID, expiredHash, time.Now().Add(-1*time.Hour), time.Now().Add(-2*time.Hour))
	require.NoError(t, err)

	// Verify we have 2 sessions total
	var totalCount int
	err = testDB.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM user_sessions WHERE user_id = $1", user.ID).Scan(&totalCount)
	require.NoError(t, err)
	assert.Equal(t, 2, totalCount)

	// When: deleting expired sessions (those with expires_at <= now)
	err = queries.DeleteExpiredSessions(context.Background(), time.Now())
	require.NoError(t, err)

	// Then: only the active session remains
	err = testDB.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM user_sessions WHERE user_id = $1", user.ID).Scan(&totalCount)
	require.NoError(t, err)
	assert.Equal(t, 1, totalCount)

	// Verify the active session still exists
	_, err = queries.GetActiveSessionByRefreshHash(context.Background(), activeHash)
	require.NoError(t, err)
}

// ============================================================================
// SECTION 10: Transactional Integrity (ExecTx)
// ============================================================================

func TestIntegration_ExecTx_CreateUserAndSession(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries
	store := db.NewStore(testDB)

	var createdUserID int64
	tokenHash := validRefreshTokenHash("tx-session")

	// When: creating user + session in a single transaction
	err := store.ExecTx(context.Background(), func(q *db.Queries) error {
		user, err := q.CreateUser(context.Background(), db.CreateUserParams{
			Email:        "tx-user@example.com",
			PasswordHash: testutil.TestPasswordHash,
			Role:         "operator",
			IsActive:     true,
		})
		if err != nil {
			return err
		}
		createdUserID = user.ID

		_, err = q.CreateUserSession(context.Background(), db.CreateUserSessionParams{
			UserID:           user.ID,
			RefreshTokenHash: tokenHash,
			UserAgent:        sql.NullString{String: "TxBrowser", Valid: true},
			ExpiresAt:        time.Now().Add(24 * time.Hour),
			IpAddress:        testInet("172.16.0.1"),
		})
		return err
	})

	// Then: both user and session exist
	require.NoError(t, err)

	user, err := queries.GetUserByID(context.Background(), createdUserID)
	require.NoError(t, err)
	assert.Equal(t, "tx-user@example.com", user.Email)

	session, err := queries.GetActiveSessionByRefreshHash(context.Background(), tokenHash)
	require.NoError(t, err)
	assert.Equal(t, createdUserID, session.UserID)
}

func TestIntegration_ExecTx_RollbackOnError(t *testing.T) {
	cleanupUsers(t)
	queries := testQueries
	store := db.NewStore(testDB)

	tokenHash := validRefreshTokenHash("rollback-session")

	// When: transaction fails midway (user created, session fails)
	err := store.ExecTx(context.Background(), func(q *db.Queries) error {
		_, err := q.CreateUser(context.Background(), db.CreateUserParams{
			Email:        "rollback@example.com",
			PasswordHash: testutil.TestPasswordHash,
			Role:         "operator",
			IsActive:     true,
		})
		if err != nil {
			return err
		}

		// Force failure: invalid token hash format
		_, err = q.CreateUserSession(context.Background(), db.CreateUserSessionParams{
			UserID:           999999, // Will reference user created above but wrong ID
			RefreshTokenHash: "invalid-hash-format",
			UserAgent:        sql.NullString{Valid: false},
			ExpiresAt:        time.Now().Add(24 * time.Hour),
			IpAddress:        testInet("10.0.0.1"),
		})
		return err
	})

	// Then: transaction rolled back, user should NOT exist
	require.Error(t, err)

	_, err = queries.GetUserAuthByEmail(context.Background(), "rollback@example.com")
	assert.ErrorIs(t, err, sql.ErrNoRows, "user should not exist after rollback")

	// Session should also not exist
	_, err = queries.GetActiveSessionByRefreshHash(context.Background(), tokenHash)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
