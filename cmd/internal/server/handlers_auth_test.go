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

// findCookie returns a response cookie by name, or nil if not found.
func findCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
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

func TestLoginHandler_Success(t *testing.T) {
	router, mockStore, logger, _ := setupAuthTestServer(t)
	user := testUser()

	// Mock: fetch user by email
	mockStore.EXPECT().
		GetUserAuthByEmail(gomock.Any(), testEmail).
		Return(user, nil)

	// Mock: session creation transaction (callback not executed — result built from pre-ExecTx data)
	mockStore.EXPECT().
		ExecTx(gomock.Any(), gomock.Any()).
		Return(nil)

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
	assert.NotNil(t, findCookie(w, "access_token"), "expected access_token cookie")
	assert.NotNil(t, findCookie(w, "refresh_token"), "expected refresh_token cookie")
	assert.NotNil(t, findCookie(w, "csrf_token"), "expected csrf_token cookie")

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
	assert.NotNil(t, findCookie(w, "access_token"), "expected new access_token cookie")
	assert.NotNil(t, findCookie(w, "refresh_token"), "expected new refresh_token cookie")
	assert.NotNil(t, findCookie(w, "csrf_token"), "expected new csrf_token cookie")
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
	accessCookie := findCookie(w, "access_token")
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
	accessCookie := findCookie(w, "access_token")
	require.NotNil(t, accessCookie, "access_token cookie should be present (cleared)")
	assert.True(t, accessCookie.MaxAge < 0, "access_token should be expired")

	refreshCookie := findCookie(w, "refresh_token")
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
	assert.Contains(t, body["error"], "csrf")
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
	assert.Equal(t, "access_token_expired", body["error"])
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
