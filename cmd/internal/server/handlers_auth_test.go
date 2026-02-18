package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/auth"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

/*
BEHAVIORAL SCENARIOS FOR AUTH HANDLERS (Integration: Handler + Auth Service with mocked DB)

What user problems does this protect us from?
================================================================================
1. Login flow — correct credentials return user info + auth cookies; invalid input returns 400/401
2. Token refresh — valid refresh cookie produces new tokens; missing/invalid cookie returns 401
3. Logout — session is revoked, auth cookies are cleared; works even without refresh cookie
4. CSRF protection — state-changing endpoints (logout) require CSRF token
5. Auth middleware — protected endpoints (me) reject unauthenticated requests

NOTE: POST /api/auth/register is NOT implemented in the current codebase.
      User creation is handled via CLI tool (cmd/createadmin) and admin API.
      Tests below cover all existing auth endpoints: login, refresh, logout, me.

Approach:
  - Auth handlers delegate to auth.Service, which uses db.Store.
  - We mock db.Store (gomock) and test handlers through HTTP (httptest).
  - For ExecTx callbacks, we use go-sqlmock–backed *db.Queries (same pattern as lot_service_test).
  - This tests the full handler → service → (mocked) DB path.
*/

// =============================================================================
// CONSTANTS AND TEST FIXTURES
// =============================================================================

const (
	testJWTSecret = "test-secret-key-minimum-32-chars-long-for-tests"
	testPassword  = "password123"
	testEmail     = "user@example.com"
)

// testPasswordHash is a bcrypt hash of testPassword, generated once in TestMain.
var testPasswordHash string

func TestMain(m *testing.M) {
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		log.Fatalf("failed to generate test password hash: %v", err)
	}
	testPasswordHash = string(hash)
	os.Exit(m.Run())
}

// testConfig returns a Config with test-appropriate auth settings.
func testConfig() *config.Config {
	isDebug := true
	return &config.Config{
		IsDebug: &isDebug,
		Auth: config.AuthConfig{
			JWTSecret:         testJWTSecret,
			AccessTokenTTL:    15 * time.Minute,
			RefreshTokenTTL:   7 * 24 * time.Hour,
			CookieAccessName:  "access_token",
			CookieRefreshName: "refresh_token",
			CookieDomain:      "",
			CookieSecure:      false,
			CookieHttpOnly:    true,
			CookieSameSite:    "lax",
		},
	}
}

// testUser returns a db.User with a valid bcrypt password hash for testPassword.
func testUser() db.User {
	now := time.Now()
	return db.User{
		ID:           1,
		Email:        testEmail,
		PasswordHash: testPasswordHash,
		Role:         "user",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// =============================================================================
// SETUP AND HELPERS
// =============================================================================

// setupAuthTestServer creates a gin.Engine with auth routes wired to a mocked store.
// Returns the router, mock store, logger, and config.
func setupAuthTestServer(t *testing.T) (*gin.Engine, *db.MockStore, *testutil.MockLogger, *config.Config) {
	t.Helper()

	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()
	cfg := testConfig()

	authService := auth.NewService(mockStore, cfg, logger)

	gin.SetMode(gin.TestMode)
	router := gin.New()

	server := &Server{
		store:       mockStore,
		logger:      logger,
		authService: authService,
		config:      cfg,
	}

	// Register routes matching production layout
	v1 := router.Group("/api/v1")
	{
		v1.POST("/auth/login", server.loginHandler)
		v1.POST("/auth/refresh", server.refreshHandler)
		v1.POST("/auth/logout", CsrfMiddleware(), server.logoutHandler)

		protected := v1.Group("/")
		protected.Use(AuthMiddleware(cfg, mockStore, logger))
		protected.Use(CsrfMiddleware())
		{
			protected.GET("/auth/me", server.meHandler)
		}
	}

	return router, mockStore, logger, cfg
}

// makeJSONRequest creates an HTTP request with a JSON-encoded body.
func makeJSONRequest(t *testing.T, method, path string, body interface{}) *http.Request {
	t.Helper()
	jsonBody, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(method, path, bytes.NewBuffer(jsonBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// makeTestAccessToken creates a valid JWT access token signed with testJWTSecret.
func makeTestAccessToken(t *testing.T, userID int64, role string) string {
	t.Helper()
	now := time.Now()
	claims := auth.JWTClaims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "tenders-go",
			Subject:   fmt.Sprintf("%d", userID),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)
	return signed
}

// makeTestRefreshToken returns a deterministic valid refresh token (64 hex chars)
// and its SHA-256 hash (used by the auth service for DB lookup).
func makeTestRefreshToken() (token string, hash string) {
	token = strings.Repeat("ab", 32) // 64 hex chars
	h := sha256.Sum256([]byte(token))
	hash = hex.EncodeToString(h[:])
	return
}

// parseBody parses JSON response body into a map.
func parseBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err, "failed to parse response body: %s", w.Body.String())
	return body
}

// addCSRF adds csrf_token cookie and X-CSRF-Token header to a request.
func addCSRF(req *http.Request, csrfToken string) {
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	req.Header.Set(csrfHeaderName, csrfToken)
}

// --- sqlmock helpers (same pattern as lot_service_test.go) ---

var (
	sessionColumns  = []string{"id", "user_id", "refresh_token_hash", "created_at", "expires_at", "revoked_at"}
	userByIDColumns = []string{"id", "email", "role", "is_active", "last_login_at", "created_at", "updated_at"}
)

// newMockQueries creates a sqlmock-backed *db.Queries for use inside ExecTx DoAndReturn.
func newMockQueries(t *testing.T) (sqlmock.Sqlmock, *db.Queries, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	q := db.New(sqlDB)
	cleanup := func() {
		assert.NoError(t, mock.ExpectationsWereMet(), "sqlmock: unmet expectations")
		sqlDB.Close()
	}
	return mock, q, cleanup
}

// execTxDoAndReturn returns a DoAndReturn func that executes the ExecTx callback
// with a sqlmock-backed *db.Queries after setupFn configures expectations.
func execTxDoAndReturn(t *testing.T, setupFn func(mock sqlmock.Sqlmock)) func(ctx context.Context, fn func(*db.Queries) error) error {
	t.Helper()
	return func(ctx context.Context, fn func(*db.Queries) error) error {
		mock, q, cleanup := newMockQueries(t)
		defer cleanup()
		setupFn(mock)
		return fn(q)
	}
}

// =============================================================================
// LOGIN HANDLER TESTS
// =============================================================================

// performSuccessfulLogin sets up mocks for a successful login flow (GetUserAuthByEmail +
// ExecTx with session creation and last_login update), sends a POST /api/v1/auth/login,
// and asserts HTTP 200. Returns the recorder for further assertions.
func performSuccessfulLogin(t *testing.T, router *gin.Engine, mockStore *db.MockStore) *httptest.ResponseRecorder {
	t.Helper()
	user := testUser()

	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), testEmail).
		Return(user, nil)

	now := time.Now()
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("INSERT INTO user_sessions").
				WithArgs(user.ID, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), user.ID, "hash", now, now.Add(7*24*time.Hour), nil))
			mock.ExpectExec("UPDATE users").
				WithArgs(user.ID).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    testEmail,
		Password: testPassword,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	return w
}

func TestLoginHandler_Success(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)
	user := testUser()

	// Mock: fetch user by email
	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), testEmail).
		Return(user, nil)

	// Mock: session creation transaction — exercises the CreateUserSession INSERT + UpdateUserLastLogin
	now := time.Now()
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// CreateUserSession: INSERT with (user_id, refresh_token_hash, user_agent, expires_at, ip_address)
			mock.ExpectQuery("INSERT INTO user_sessions").
				WithArgs(user.ID, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), user.ID, "hash", now, now.Add(7*24*time.Hour), nil))

			// UpdateUserLastLogin: UPDATE users SET last_login_at
			mock.ExpectExec("UPDATE users").
				WithArgs(user.ID).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    testEmail,
		Password: testPassword,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Assert HTTP 200 with user info
	assert.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	userResp, ok := body["user"].(map[string]interface{})
	require.True(t, ok, "expected 'user' object in response")
	assert.Equal(t, float64(user.ID), userResp["id"])
	assert.Equal(t, user.Email, userResp["email"])
	assert.Equal(t, user.Role, userResp["role"])

	// Assert auth cookies are set
	assert.NotNil(t, testutil.FindResponseCookie(w, "access_token"), "expected access_token cookie")
	assert.NotNil(t, testutil.FindResponseCookie(w, "refresh_token"), "expected refresh_token cookie")
	assert.NotNil(t, testutil.FindResponseCookie(w, "csrf_token"), "expected csrf_token cookie")

	// Assert logger: successful login recorded by auth service
	testutil.AssertLogEntry(t, logger, testutil.LevelInfo, "successful login")
}

func TestLoginHandler_ValidationErrors(t *testing.T) {
	// Router is created once — validation sub-tests don't touch mutable state (no DB calls).
	router, _, _, _ := setupAuthTestServer(t)

	tests := []struct {
		name string
		body interface{}
	}{
		{"empty body", map[string]interface{}{}},
		{"missing email", map[string]interface{}{"password": "password123"}},
		{"missing password", map[string]interface{}{"email": "user@example.com"}},
		{"invalid email format", map[string]interface{}{"email": "not-email", "password": "password123"}},
		{"password too short", map[string]interface{}{"email": "user@example.com", "password": "12345"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", tt.body)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			body := parseBody(t, w)
			_, hasError := body["error"]
			assert.True(t, hasError, "expected 'error' field in response")
		})
	}
}

func TestLoginHandler_MalformedJSON(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{invalid`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), testEmail).
		Return(testUser(), nil)

	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    testEmail,
		Password: "wrongpassword",
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "invalid email or password", body["error"])

	// Assert logger: wrong password attempt logged as Warn by auth service
	testutil.AssertLogEntry(t, logger, testutil.LevelWarn, "failed login attempt")
	testutil.AssertLogEntry(t, logger, testutil.LevelWarn, "invalid password")
}

func TestLoginHandler_UserNotFound(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), "unknown@example.com").
		Return(db.User{}, sql.ErrNoRows)

	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    "unknown@example.com",
		Password: testPassword,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "invalid email or password", body["error"])

	// Assert logger: non-existent email logged as Warn by auth service
	testutil.AssertLogEntry(t, logger, testutil.LevelWarn, "non-existent email")
}

func TestLoginHandler_InactiveUser(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	user := testUser()
	user.IsActive = false

	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), testEmail).
		Return(user, nil)

	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    testEmail,
		Password: testPassword,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "invalid email or password", body["error"])

	// Assert logger: inactive user login attempt logged as Warn by auth service
	testutil.AssertLogEntry(t, logger, testutil.LevelWarn, "inactive user")
}

func TestLoginHandler_DBError(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), testEmail).
		Return(db.User{}, fmt.Errorf("connection refused"))

	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    testEmail,
		Password: testPassword,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "internal server error", body["error"])

	// Assert logger: handler logs Error with attached error
	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "login failed")
}

func TestLoginHandler_SessionCreationFailed(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), testEmail).
		Return(testUser(), nil)

	mockStore.EXPECT().
		ExecTx(gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("database is locked"))

	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    testEmail,
		Password: testPassword,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "internal server error", body["error"])

	// Assert logger: auth service logs session creation error + handler logs "login failed"
	testutil.AssertLogEntry(t, logger, testutil.LevelError, "failed to create session")
	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "login failed")
}

// =============================================================================
// REFRESH HANDLER TESTS
// =============================================================================

func TestRefreshHandler_Success(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)
	now := time.Now()

	refreshToken, refreshHash := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// 1. Fetch active session by refresh hash
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(refreshHash).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), int64(1), refreshHash, now, now.Add(7*24*time.Hour), nil))

			// 2. Revoke old session
			mock.ExpectExec("UPDATE user_sessions SET revoked_at").
				WithArgs(int64(1)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			// 3. Create new session (new random refresh token — use AnyArg)
			mock.ExpectQuery("INSERT INTO user_sessions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(2), int64(1), "new-hash", now, now.Add(7*24*time.Hour), nil))

			// 4. Get user info for access token generation
			mock.ExpectQuery("SELECT .+ FROM users WHERE id").
				WithArgs(int64(1)).
				WillReturnRows(sqlmock.NewRows(userByIDColumns).
					AddRow(int64(1), testEmail, "user", true, nil, now, now))
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "tokens refreshed successfully", body["message"])

	// New cookies should be set
	assert.NotNil(t, testutil.FindResponseCookie(w, "access_token"), "expected new access_token cookie")
	assert.NotNil(t, testutil.FindResponseCookie(w, "refresh_token"), "expected new refresh_token cookie")
	assert.NotNil(t, testutil.FindResponseCookie(w, "csrf_token"), "expected new csrf_token cookie")
}

func TestRefreshHandler_NoCookie(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "refresh token not found", body["error"])
}

func TestRefreshHandler_InvalidTokenFormat(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	// Token not 64 hex chars — format validation fails before any DB call
	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "invalid-short-token"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "invalid or expired refresh token", body["error"])

	// clearAuthCookies always sets access_token with MaxAge = -1
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	require.NotNil(t, accessCookie, "expected access_token cookie to be present (cleared)")
	assert.True(t, accessCookie.MaxAge < 0, "access_token should be cleared (MaxAge < 0)")
}

func TestRefreshHandler_SessionNotFound(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)

	refreshToken, _ := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// Session not found in DB
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(sqlmock.AnyArg()).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "invalid or expired refresh token", body["error"])
}

func TestRefreshHandler_DBError(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	refreshToken, _ := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(sqlmock.AnyArg()).
				WillReturnError(fmt.Errorf("connection reset"))
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "internal server error", body["error"])

	// Assert logger: handler logs Error with attached error
	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "refresh failed")
}

// =============================================================================
// LOGOUT HANDLER TESTS
// =============================================================================

func TestLogoutHandler_Success(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)

	refreshToken, refreshHash := makeTestRefreshToken()
	csrfToken := "test-csrf-token-32chars-minimum-value"

	// Mock: revoke session by refresh hash
	mockStore.EXPECT().
		RevokeSessionByRefreshHash(gomock.Any(), refreshHash).
		Return(nil)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	addCSRF(req, csrfToken)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "logged out successfully", body["message"])

	// Auth cookies should be cleared (MaxAge < 0)
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	require.NotNil(t, accessCookie, "access_token cookie should be present (cleared)")
	assert.True(t, accessCookie.MaxAge < 0, "access_token should be expired")

	refreshCookie := testutil.FindResponseCookie(w, "refresh_token")
	require.NotNil(t, refreshCookie, "refresh_token cookie should be present (cleared)")
	assert.True(t, refreshCookie.MaxAge < 0, "refresh_token should be expired")
}

func TestLogoutHandler_NoCookie(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	csrfToken := "test-csrf-token-32chars-minimum-value"

	// No refresh_token cookie — handler clears cookies and returns 200 without DB call
	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	require.NoError(t, err)
	addCSRF(req, csrfToken)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "logged out successfully", body["message"])
}

func TestLogoutHandler_MissingCSRF(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	refreshToken, _ := makeTestRefreshToken()

	// No CSRF token — middleware blocks the request
	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "csrf_token_missing", body["error"])
}

func TestLogoutHandler_CSRFMismatch(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	refreshToken, _ := makeTestRefreshToken()

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "cookie-value"})
	req.Header.Set(csrfHeaderName, "different-header-value")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "csrf_invalid", body["error"])
}

func TestLogoutHandler_ServiceError(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	refreshToken, refreshHash := makeTestRefreshToken()
	csrfToken := "test-csrf-token-32chars-minimum-value"

	// Mock: RevokeSessionByRefreshHash fails — handler still returns 200
	mockStore.EXPECT().
		RevokeSessionByRefreshHash(gomock.Any(), refreshHash).
		Return(fmt.Errorf("database error"))

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	addCSRF(req, csrfToken)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Logout is resilient — returns 200 even on service error
	assert.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "logged out successfully", body["message"])

	// Assert logger: error is logged even though response is 200
	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "logout failed")
}

// =============================================================================
// ME HANDLER TESTS
// =============================================================================

func TestMeHandler_Success(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)
	now := time.Now()

	accessToken := makeTestAccessToken(t, 1, "user")

	// Mock: GetUserByID (called by meHandler after AuthMiddleware sets user_id)
	mockStore.EXPECT().
		GetUserByID(gomock.Any(), int64(1)).
		Return(db.GetUserByIDRow{
			ID:        1,
			Email:     testEmail,
			Role:      "user",
			IsActive:  true,
			CreatedAt: now,
			UpdatedAt: now,
		}, nil)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: accessToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	userResp, ok := body["user"].(map[string]interface{})
	require.True(t, ok, "expected 'user' object in response")
	assert.Equal(t, float64(1), userResp["id"])
	assert.Equal(t, testEmail, userResp["email"])
	assert.Equal(t, "user", userResp["role"])
}

func TestMeHandler_NoAuth(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	// No access_token cookie → AuthMiddleware rejects with 401
	req, err := http.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMeHandler_InvalidToken(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "invalid.jwt.token"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "access_token_invalid", body["error"])
}

func TestMeHandler_DBError(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)

	accessToken := makeTestAccessToken(t, 1, "user")

	mockStore.EXPECT().
		GetUserByID(gomock.Any(), int64(1)).
		Return(db.GetUserByIDRow{}, fmt.Errorf("database unreachable"))

	req, err := http.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: accessToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "internal server error", body["error"])

	// Assert logger: handler logs Error with attached error
	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "failed to get user")
}

// =============================================================================
// CRITICAL MISSING TESTS — SECURITY, COOKIE ATTRIBUTES, EDGE CASES
// =============================================================================

// --- Login Security ---

// TestLoginHandler_TokensNotInResponseBody ensures tokens are ONLY in httpOnly cookies,
// never in the JSON response body. If tokens leak into response body, XSS can steal them.
func TestLoginHandler_TokensNotInResponseBody(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)

	w := performSuccessfulLogin(t, router, mockStore)

	// Response body must NOT contain any token values (XSS risk)
	body := parseBody(t, w)
	testutil.AssertNoTokensInBody(t, body)

	// Tokens MUST be in cookies with correct security flags
	testutil.AssertAuthCookieSecurity(t, w)
}

// TestLoginHandler_CookieSecurityAttributes verifies auth cookies have correct security flags:
// - access_token & refresh_token: HttpOnly=true (prevent XSS from stealing tokens)
// - csrf_token: HttpOnly=false (frontend JS needs to read it for headers)
func TestLoginHandler_CookieSecurityAttributes(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)

	w := performSuccessfulLogin(t, router, mockStore)

	// Verify core security attributes via shared helper
	testutil.AssertAuthCookieSecurity(t, w)

	// Additional attribute checks specific to login
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	assert.Equal(t, "/", accessCookie.Path, "access_token must be available on all paths")
	assert.True(t, accessCookie.MaxAge > 0, "access_token should have positive TTL")

	refreshCookie := testutil.FindResponseCookie(w, "refresh_token")
	assert.Equal(t, "/", refreshCookie.Path, "refresh_token must be available on all paths")
	assert.True(t, refreshCookie.MaxAge > 0, "refresh_token should have positive TTL")
}

// TestLoginHandler_EmailNormalization verifies that email is case-insensitive.
// The auth service normalizes email to lowercase before DB lookup.
// If this breaks, users with "User@Example.COM" won't be able to log in.
func TestLoginHandler_EmailNormalization(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)
	user := testUser()

	// Auth service normalizes to lowercase → DB lookup uses "user@example.com"
	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), "user@example.com").
		Return(user, nil)

	now := time.Now()
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("INSERT INTO user_sessions").
				WithArgs(user.ID, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), user.ID, "hash", now, now.Add(7*24*time.Hour), nil))
			mock.ExpectExec("UPDATE users").
				WithArgs(user.ID).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	// Send email with mixed case — should be normalized
	req := makeJSONRequest(t, http.MethodPost, "/api/v1/auth/login", LoginRequest{
		Email:    "User@Example.COM",
		Password: testPassword,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "login must succeed with mixed-case email")
}

// --- Me Endpoint Security ---

// TestMeHandler_ExpiredToken verifies that an expired (but properly signed) JWT:
// 1. Returns 401
// 2. Sets X-Auth-Error: access_token_expired header (for frontend auto-refresh)
// 3. Clears the access cookie (prevents "sticky 401")
// 4. Does NOT clear the refresh cookie (client can still call /auth/refresh)
//
// This is critical: if middleware doesn't signal the frontend properly,
// the client gets stuck in a 401 loop unable to auto-refresh.
func TestMeHandler_ExpiredToken(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	// Create a JWT that expired 1 minute ago (properly signed, just expired)
	now := time.Now()
	claims := auth.JWTClaims{
		UserID: 1,
		Role:   "user",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now.Add(-16 * time.Minute)),
			NotBefore: jwt.NewNumericDate(now.Add(-16 * time.Minute)),
			Issuer:    "tenders-go",
			Subject:   "1",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expiredToken, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: expiredToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 401 Unauthorized
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// X-Auth-Error header MUST be set for frontend auto-refresh logic
	assert.Equal(t, "access_token_expired", w.Header().Get("X-Auth-Error"),
		"middleware must set X-Auth-Error header so frontend can trigger auto-refresh")

	// Access cookie MUST be cleared (prevents browser re-sending expired cookie → infinite 401)
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	require.NotNil(t, accessCookie, "access_token cookie must be present (with cleared value)")
	assert.True(t, accessCookie.MaxAge < 0, "access_token cookie must be expired/cleared")

	// Refresh cookie must NOT be cleared — middleware only clears access cookie,
	// allowing client to call /auth/refresh to get a new access token
	refreshCookie := testutil.FindResponseCookie(w, "refresh_token")
	assert.Nil(t, refreshCookie, "refresh_token cookie must NOT be set/cleared by middleware")
}

// TestMeHandler_WrongSigningKey verifies that a JWT signed with a different key is rejected.
// This protects against tokens forged with a compromised key from another service.
func TestMeHandler_WrongSigningKey(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	// Create a valid-looking JWT signed with a DIFFERENT key
	now := time.Now()
	claims := auth.JWTClaims{
		UserID: 1,
		Role:   "admin", // attacker might try to elevate privileges
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "tenders-go",
			Subject:   "1",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	wrongKeyToken, err := token.SignedString([]byte("completely-different-secret-key-that-attacker-has"))
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: wrongKeyToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Wrong signing key is an invalid token, not an expired one
	assert.Equal(t, "access_token_invalid", w.Header().Get("X-Auth-Error"))

	// Access cookie cleared
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	require.NotNil(t, accessCookie)
	assert.True(t, accessCookie.MaxAge < 0)
}

// --- Refresh Security & Transaction Integrity ---

// TestRefreshHandler_ExpiredSessionTimeMismatch tests defense-in-depth:
// DB returns a session row (simulating DB/app time skew), but ExpiresAt is in the past.
// The Go-side time.Now().After(session.ExpiresAt) check must catch this.
// Without this check, an expired session could be reused if DB and app clocks differ.
func TestRefreshHandler_ExpiredSessionTimeMismatch(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)

	refreshToken, refreshHash := makeTestRefreshToken()
	pastTime := time.Now().Add(-1 * time.Hour)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// DB returns a session with ExpiresAt in the past (time skew scenario)
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(refreshHash).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), int64(1), refreshHash,
						pastTime.Add(-7*24*time.Hour), // created_at
						pastTime,                      // expires_at (in the past!)
						nil))                          // not revoked
			// No further expectations — Go-side check should reject before any more queries
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Must be rejected as expired
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "invalid or expired refresh token", body["error"])

	// Cookies must be cleared (same as session-not-found path)
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	require.NotNil(t, accessCookie, "access_token cookie must be cleared")
	assert.True(t, accessCookie.MaxAge < 0)
}

// TestRefreshHandler_CookiesClearedOnAuthError verifies that when refresh fails
// with an auth error (session not found), cookies are actually cleared.
// The existing TestRefreshHandler_SessionNotFound only checks status code.
func TestRefreshHandler_CookiesClearedOnAuthError(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)

	refreshToken, _ := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(sqlmock.AnyArg()).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// ALL auth cookies must be cleared to prevent stuck client state
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	require.NotNil(t, accessCookie, "access_token cookie must be present (cleared)")
	assert.True(t, accessCookie.MaxAge < 0, "access_token must have expired MaxAge")

	refreshCookie := testutil.FindResponseCookie(w, "refresh_token")
	require.NotNil(t, refreshCookie, "refresh_token cookie must be present (cleared)")
	assert.True(t, refreshCookie.MaxAge < 0, "refresh_token must have expired MaxAge")

	csrfCookie := testutil.FindResponseCookie(w, "csrf_token")
	require.NotNil(t, csrfCookie, "csrf_token cookie must be present (cleared)")
	assert.True(t, csrfCookie.MaxAge < 0, "csrf_token must have expired MaxAge")
}

// TestRefreshHandler_RevokeOldSessionFails verifies transaction atomicity:
// if revoking the old session fails, no new session is created.
// This prevents session leakage where old and new sessions both exist.
func TestRefreshHandler_RevokeOldSessionFails(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)
	now := time.Now()

	refreshToken, refreshHash := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// 1. Fetch session — succeeds
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(refreshHash).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), int64(1), refreshHash, now, now.Add(7*24*time.Hour), nil))

			// 2. Revoke old session — FAILS (e.g., deadlock)
			mock.ExpectExec("UPDATE user_sessions SET revoked_at").
				WithArgs(int64(1)).
				WillReturnError(fmt.Errorf("deadlock detected"))

			// Steps 3-6 should NOT be reached
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Must be 500 (internal error, not auth error)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "internal server error", body["error"])

	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "refresh failed")
}

// TestRefreshHandler_CreateNewSessionFails verifies that if creating a new session
// fails after the old session was revoked, the entire transaction rolls back.
// Without tx rollback, user would be left with no valid session (locked out).
func TestRefreshHandler_CreateNewSessionFails(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)
	now := time.Now()

	refreshToken, refreshHash := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// 1. Fetch session — success
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(refreshHash).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), int64(1), refreshHash, now, now.Add(7*24*time.Hour), nil))

			// 2. Revoke old session — success
			mock.ExpectExec("UPDATE user_sessions SET revoked_at").
				WithArgs(int64(1)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			// 3. Create new session — FAILS (e.g., constraint violation)
			mock.ExpectQuery("INSERT INTO user_sessions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnError(fmt.Errorf("unique_violation: duplicate refresh_token_hash"))
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "internal server error", body["error"])

	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "refresh failed")
}

// TestRefreshHandler_GetUserFailsInsideTx verifies error handling when user info
// lookup fails during refresh (e.g., user was deleted between session creation and lookup).
func TestRefreshHandler_GetUserFailsInsideTx(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)
	now := time.Now()

	refreshToken, refreshHash := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// 1. Fetch session — success
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(refreshHash).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), int64(1), refreshHash, now, now.Add(7*24*time.Hour), nil))

			// 2. Revoke old session — success
			mock.ExpectExec("UPDATE user_sessions SET revoked_at").
				WithArgs(int64(1)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			// 3. Create new session — success
			mock.ExpectQuery("INSERT INTO user_sessions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(2), int64(1), "new-hash", now, now.Add(7*24*time.Hour), nil))

			// 4. Get user — FAILS (user deleted or DB error)
			mock.ExpectQuery("SELECT .+ FROM users WHERE id").
				WithArgs(int64(1)).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "internal server error", body["error"])

	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "refresh failed")
}

// TestRefreshHandler_CookiesNotClearedOnInternalError verifies that 500 errors
// do NOT clear cookies. This is important because:
// - Temporary DB issues shouldn't force full logout
// - Client retains refresh token and can retry
func TestRefreshHandler_CookiesNotClearedOnInternalError(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)
	now := time.Now()

	refreshToken, refreshHash := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// Session found but revoke fails (temporary DB error)
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(refreshHash).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), int64(1), refreshHash, now, now.Add(7*24*time.Hour), nil))

			mock.ExpectExec("UPDATE user_sessions SET revoked_at").
				WithArgs(int64(1)).
				WillReturnError(fmt.Errorf("connection reset by peer"))
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Cookies must NOT be cleared on 500 — client should be able to retry
	// No Set-Cookie header for auth cookies should be present
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "access_token" || cookie.Name == "refresh_token" || cookie.Name == "csrf_token" {
			t.Errorf("cookie %s should NOT be set on 500 error (prevents unnecessary logout)", cookie.Name)
		}
	}
}

// TestRefreshHandler_CookieSecurityAttributes verifies that new cookies set after
// successful refresh have correct security flags (same requirements as login).
func TestRefreshHandler_CookieSecurityAttributes(t *testing.T) {
	router, mockStore, _, _ := setupAuthTestServer(t)
	now := time.Now()

	refreshToken, refreshHash := makeTestRefreshToken()

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_hash").
				WithArgs(refreshHash).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(1), int64(1), refreshHash, now, now.Add(7*24*time.Hour), nil))

			mock.ExpectExec("UPDATE user_sessions SET revoked_at").
				WithArgs(int64(1)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			mock.ExpectQuery("INSERT INTO user_sessions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(sessionColumns).
					AddRow(int64(2), int64(1), "new-hash", now, now.Add(7*24*time.Hour), nil))

			mock.ExpectQuery("SELECT .+ FROM users WHERE id").
				WithArgs(int64(1)).
				WillReturnRows(sqlmock.NewRows(userByIDColumns).
					AddRow(int64(1), testEmail, "user", true, nil, now, now))
		}),
	)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	// Verify all cookie security flags are correct after refresh too
	testutil.AssertAuthCookieSecurity(t, w)
}

// --- Logout Edge Cases ---

// TestLogoutHandler_InvalidRefreshTokenFormat verifies that if the refresh_token cookie
// exists but has invalid format (not 64 hex chars), the handler still returns 200
// and clears cookies. The service returns ErrInvalidToken which is logged but not exposed.
func TestLogoutHandler_InvalidRefreshTokenFormat(t *testing.T) {
	router, _, logger, _ := setupAuthTestServer(t)

	csrfToken := "test-csrf-token-32chars-minimum-value"

	// No DB mock needed — service rejects the token format before any DB call
	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "not-64-hex-chars"})
	addCSRF(req, csrfToken)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Logout must be resilient — returns 200 even with invalid token format
	assert.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "logged out successfully", body["message"])

	// Cookies must still be cleared
	accessCookie := testutil.FindResponseCookie(w, "access_token")
	require.NotNil(t, accessCookie, "access_token cookie must be cleared")
	assert.True(t, accessCookie.MaxAge < 0, "access_token must be expired")

	// Error should be logged
	testutil.AssertLogEntryWithError(t, logger, testutil.LevelError, "logout failed")
}

// --- CSRF Edge Cases ---

// TestCSRF_HeaderMissingCookiePresent verifies that when the CSRF cookie exists
// but the X-CSRF-Token header is missing, the middleware returns a different error code
// ("csrf_header_missing") than when the cookie is missing ("csrf_token_missing").
// This distinction helps frontend debugging.
func TestCSRF_HeaderMissingCookiePresent(t *testing.T) {
	router, _, _, _ := setupAuthTestServer(t)

	refreshToken, _ := makeTestRefreshToken()

	req, err := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	// Set CSRF cookie but NOT the X-CSRF-Token header
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "some-csrf-token-value"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, "csrf_header_missing", body["error"],
		"must distinguish between missing cookie and missing header for frontend debugging")
}
