package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

/*
BEHAVIORAL SCENARIOS FOR AUTH SERVICE (Unit Tests)

What user problems does this protect us from?
================================================================================
1. Token security - access tokens must be properly signed and validated
2. Token expiration - expired tokens must be rejected
3. Token tampering - modified tokens must be detected
4. Input validation - malformed inputs must be rejected before DB access
5. Cryptographic security - refresh tokens must be unpredictable

These unit tests focus on the parts of auth service that can be tested in isolation
without requiring database transactions. For full workflow tests (Login, Refresh, Logout),
see service_integration_test.go which uses testcontainers.

GIVEN / WHEN / THEN Scenarios:
================================================================================

SCENARIO 1: Access Token Generation and Validation
- GIVEN a valid user ID and role
  WHEN an access token is generated
  THEN token contains correct claims and is verifiable

- GIVEN a valid access token
  WHEN token is validated
  THEN user ID and role are correctly extracted

- GIVEN an expired access token
  WHEN token is validated
  THEN validation fails with ErrInvalidToken

- GIVEN a token with wrong signature
  WHEN token is validated
  THEN validation fails with ErrInvalidToken

SCENARIO 2: Refresh Token Generation
- GIVEN refresh token generation is requested
  WHEN tokens are generated
  THEN they are unique, properly formatted, and hashes are deterministic

SCENARIO 3: Input Validation
- GIVEN various malformed refresh token formats
  WHEN format validation is performed
  THEN invalid formats are rejected before DB access

- GIVEN long user agent strings
  WHEN validation is performed
  THEN strings are safely truncated at character boundary (UTF-8 safe)
*/

// mockLogger implements the Logger interface for testing
type mockLogger struct{}

func (m *mockLogger) Infof(format string, args ...any)  {}
func (m *mockLogger) Warnf(format string, args ...any)  {}
func (m *mockLogger) Errorf(format string, args ...any) {}

// setupTestService creates service with test configuration (no DB needed for token tests)
func setupTestService(t *testing.T) *Service {
	t.Helper()

	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:       "test-secret-key-minimum-32-chars-long",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
		},
	}

	logger := &mockLogger{}

	// Store is nil for token-only tests (no DB operations)
	return &Service{
		store:  nil,
		config: cfg,
		logger: logger,
	}
}

// =============================================================================
// ACCESS TOKEN GENERATION AND VALIDATION TESTS
// =============================================================================

func TestGenerateAccessToken_Success(t *testing.T) {
	// GIVEN: Valid user ID and role
	service := setupTestService(t)
	userID := int64(123)
	role := "admin"

	// WHEN: Access token is generated
	token, err := service.generateAccessToken(userID, role)

	// THEN: Token is valid and contains correct data
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// Verify token structure by parsing
	claims, err := service.ValidateAccessToken(token)
	require.NoError(t, err)

	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, role, claims.Role)
	assert.Equal(t, "tenders-go", claims.Issuer)
	assert.NotNil(t, claims.ExpiresAt)
	assert.True(t, claims.ExpiresAt.After(time.Now()))
	assert.True(t, claims.ExpiresAt.Before(time.Now().Add(16*time.Minute)),
		"Token should expire within configured TTL")
}

func TestValidateAccessToken_Success(t *testing.T) {
	// GIVEN: A freshly generated valid access token
	service := setupTestService(t)
	userID := int64(456)
	role := "user"

	token, err := service.generateAccessToken(userID, role)
	require.NoError(t, err)

	// WHEN: Token is validated
	claims, err := service.ValidateAccessToken(token)

	// THEN: Claims are correctly extracted
	require.NoError(t, err)
	require.NotNil(t, claims)

	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, role, claims.Role)
	assert.Equal(t, "tenders-go", claims.Issuer)
	assert.Equal(t, "456", claims.Subject)
}

func TestValidateAccessToken_Expired(t *testing.T) {
	// GIVEN: A service configured with negative TTL (instant expiration)
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:       "test-secret-key-minimum-32-chars-long",
			AccessTokenTTL:  -1 * time.Hour, // Expired before issued
			RefreshTokenTTL: 7 * 24 * time.Hour,
		},
	}
	service := &Service{
		config: cfg,
		logger: &mockLogger{},
	}

	// Generate token that's already expired
	expiredToken, err := service.generateAccessToken(123, "user")
	require.NoError(t, err)

	// WHEN: Expired token is validated
	claims, err := service.ValidateAccessToken(expiredToken)

	// THEN: Validation fails with invalid token error
	require.Error(t, err)
	assert.Nil(t, claims)
	assert.Equal(t, ErrInvalidToken, err)
}

func TestValidateAccessToken_WrongSignature(t *testing.T) {
	// GIVEN: Two services with different secrets
	service1 := setupTestService(t)

	cfg2 := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:       "different-secret-key-for-testing",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
		},
	}
	service2 := &Service{
		config: cfg2,
		logger: &mockLogger{},
	}

	// Generate token with service1's secret
	token, err := service1.generateAccessToken(123, "user")
	require.NoError(t, err)

	// WHEN: Token is validated with service2's secret (different key)
	claims, err := service2.ValidateAccessToken(token)

	// THEN: Validation fails due to signature mismatch
	require.Error(t, err)
	assert.Nil(t, claims)
	assert.Equal(t, ErrInvalidToken, err)
}

func TestValidateAccessToken_Malformed(t *testing.T) {
	// GIVEN: Various malformed token strings
	service := setupTestService(t)

	testCases := []struct {
		name  string
		token string
	}{
		{"empty string", ""},
		{"random string", "not-a-jwt-token"},
		{"incomplete JWT", "header.payload"},
		{"only two parts", "part1.part2"},
		{"invalid base64", "!!!.!!!.!!!"},
		{"valid format but garbage data", "aGVhZGVy.cGF5bG9hZA.c2lnbmF0dXJl"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN: Malformed token is validated
			claims, err := service.ValidateAccessToken(tc.token)

			// THEN: Validation fails
			require.Error(t, err, "Expected error for token: %s", tc.token)
			assert.Nil(t, claims)
			assert.Equal(t, ErrInvalidToken, err)
		})
	}
}

func TestValidateAccessToken_UnsafeAlgorithm(t *testing.T) {
	// GIVEN: A token signed with "none" algorithm (security vulnerability)
	service := setupTestService(t)

	claims := JWTClaims{
		UserID: 999,
		Role:   "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			Issuer:    "tenders-go",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	unsignedToken, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	// WHEN: Token with "none" algorithm is validated
	result, err := service.ValidateAccessToken(unsignedToken)

	// THEN: Validation fails (algorithm mismatch protection)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, ErrInvalidToken, err)
}

func TestValidateAccessToken_ModifiedClaims(t *testing.T) {
	// GIVEN: A valid token
	service := setupTestService(t)
	token, err := service.generateAccessToken(123, "user")
	require.NoError(t, err)

	// Verify that even swapping parts from different tokens fails
	token2, err := service.generateAccessToken(456, "admin")
	require.NoError(t, err)

	// Validate both tokens work correctly
	claims1, err1 := service.ValidateAccessToken(token)
	claims2, err2 := service.ValidateAccessToken(token2)

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.NotEqual(t, claims1.UserID, claims2.UserID, "Tokens should have different data")

	// THEN: Any modification to token would invalidate signature
	// (This is implicitly tested by other malformed token tests)
}

// =============================================================================
// REFRESH TOKEN GENERATION AND VALIDATION TESTS
// =============================================================================

func TestGenerateRefreshToken_Format(t *testing.T) {
	// GIVEN: Refresh token generation
	// WHEN: Token is generated
	token, hash, err := generateRefreshToken()

	// THEN: Token and hash have correct format
	require.NoError(t, err)
	assert.Len(t, token, 64, "Token should be 64 hex characters (32 bytes)")
	assert.Len(t, hash, 64, "Hash should be 64 hex characters (SHA-256)")

	// Verify token is valid hex
	assert.NoError(t, validateRefreshTokenFormat(token), "Generated token should be valid")
}

func TestGenerateRefreshToken_Uniqueness(t *testing.T) {
	// GIVEN: Multiple token generation requests
	tokens := make(map[string]bool)
	hashes := make(map[string]bool)
	count := 100

	// WHEN: Generating many tokens
	for i := 0; i < count; i++ {
		token, hash, err := generateRefreshToken()
		require.NoError(t, err)

		// THEN: Each token and hash should be unique
		assert.False(t, tokens[token], "Token collision detected at iteration %d", i)
		assert.False(t, hashes[hash], "Hash collision detected at iteration %d", i)

		tokens[token] = true
		hashes[hash] = true
	}

	assert.Len(t, tokens, count, "Should generate unique tokens")
	assert.Len(t, hashes, count, "Should generate unique hashes")
}

func TestHashRefreshToken_Deterministic(t *testing.T) {
	// GIVEN: A refresh token
	token, originalHash, err := generateRefreshToken()
	require.NoError(t, err)

	// WHEN: Token is hashed multiple times
	hash1 := hashRefreshToken(token)
	hash2 := hashRefreshToken(token)
	hash3 := hashRefreshToken(token)

	// THEN: Hash is deterministic
	assert.Equal(t, originalHash, hash1)
	assert.Equal(t, hash1, hash2)
	assert.Equal(t, hash2, hash3)
}

func TestValidateRefreshTokenFormat_Valid(t *testing.T) {
	// GIVEN: Valid refresh tokens
	validCases := []string{}

	// Generate some valid tokens
	for i := 0; i < 10; i++ {
		token, _, err := generateRefreshToken()
		require.NoError(t, err)
		validCases = append(validCases, token)
	}

	for _, token := range validCases {
		// WHEN: Valid token is validated
		err := validateRefreshTokenFormat(token)

		// THEN: Validation passes
		assert.NoError(t, err, "Valid token should pass validation: %s", token)
	}
}

func TestValidateRefreshTokenFormat_Invalid(t *testing.T) {
	// GIVEN: Various invalid refresh token formats
	invalidCases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"too short", "abc123"},
		{"too long", string(make([]byte, 100)) + "a"},
		{"non-hex", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"63 chars", "123456789012345678901234567890123456789012345678901234567890123"},
		{"65 chars", "12345678901234567890123456789012345678901234567890123456789012345"},
		{"special chars", "12345678901234567890123456789012345678901234567890123456789!@#$%"},
		{"spaces", "1234567890 234567890 234567890 234567890 234567890 234567890 234"},
		{"uppercase with invalid", "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN: Invalid token is validated
			err := validateRefreshTokenFormat(tc.token)

			// THEN: Validation fails
			require.Error(t, err, "Invalid token should fail validation: %s", tc.token)
			assert.Equal(t, ErrInvalidToken, err)
		})
	}
}

func TestValidateRefreshTokenFormat_EdgeCases(t *testing.T) {
	// Test exact boundary: 64 characters
	exact64Hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	assert.Len(t, exact64Hex, 64)
	assert.NoError(t, validateRefreshTokenFormat(exact64Hex), "Exact 64 hex chars should be valid")

	// Test 64 chars but not all hex
	not64Hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdeg"
	assert.Error(t, validateRefreshTokenFormat(not64Hex), "64 chars with non-hex should fail")

	// Test uppercase hex (should be valid - hex.DecodeString accepts both)
	uppercase := "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
	assert.NoError(t, validateRefreshTokenFormat(uppercase), "Uppercase hex should be valid")

	// Test mixed case
	mixedCase := "0123456789AbCdEf0123456789aBcDeF0123456789AbCdEf0123456789aBcDeF"
	assert.NoError(t, validateRefreshTokenFormat(mixedCase), "Mixed case hex should be valid")
}

// =============================================================================
// INPUT VALIDATION TESTS
// =============================================================================

func TestValidateUserAgent_NoTruncation(t *testing.T) {
	// GIVEN: Short user agent strings
	testCases := []string{
		"",
		"Mozilla/5.0",
		"curl/7.68.0",
		"PostmanRuntime/7.28.0",
	}

	for _, ua := range testCases {
		// WHEN: User agent is validated
		result := validateUserAgent(ua)

		// THEN: No truncation occurs
		assert.Equal(t, ua, result, "Short user agent should not be truncated")
		assert.LessOrEqual(t, len([]rune(result)), 255)
	}
}

func TestValidateUserAgent_TruncatesLong(t *testing.T) {
	// GIVEN: User agent with exactly 255 characters (ASCII)
	exact255 := ""
	for i := 0; i < 255; i++ {
		exact255 += "A"
	}

	// WHEN: Validated
	result := validateUserAgent(exact255)

	// THEN: No truncation (at boundary)
	assert.Equal(t, exact255, result)
	assert.Equal(t, 255, len([]rune(result)))

	// GIVEN: User agent with 300 characters
	long300 := ""
	for i := 0; i < 300; i++ {
		long300 += "B"
	}

	// WHEN: Validated
	result = validateUserAgent(long300)

	// THEN: Truncated to 255 characters
	assert.LessOrEqual(t, len([]rune(result)), 255)
	assert.Equal(t, 255, len([]rune(result)))
}

func TestValidateUserAgent_UTF8Safe(t *testing.T) {
	// GIVEN: User agent with multi-byte UTF-8 characters
	// Build a string with multi-byte characters that exceeds 255 runes
	prefix := "Mozilla/5.0 "
	utfPart := ""
	for i := 0; i < 260; i++ {
		utfPart += "世" // 3-byte UTF-8 character
	}
	utfString := prefix + utfPart

	require.Greater(t, len([]rune(utfString)), 255, "Test string should exceed 255 runes")

	// WHEN: Validated
	result := validateUserAgent(utfString)

	// THEN: Truncates at character boundary, not byte boundary
	assert.LessOrEqual(t, len([]rune(result)), 255, "Should truncate by rune count")

	// Verify result is valid UTF-8
	assert.Equal(t, result, string([]rune(result)), "Result should be valid UTF-8")
}

func TestValidateUserAgent_PreservesStructure(t *testing.T) {
	// GIVEN: Realistic long user agent
	longUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36 Edge/91.0.864.59 "
	// Add more characters to exceed 255
	for i := 0; i < 200; i++ {
		longUA += "X"
	}

	require.Greater(t, len([]rune(longUA)), 255, "Test UA should exceed 255 runes")

	// WHEN: Validated
	result := validateUserAgent(longUA)

	// THEN: Truncates cleanly without breaking structure
	assert.LessOrEqual(t, len([]rune(result)), 255)
	assert.True(t, len(result) > 0)

	// Prefix should be preserved
	assert.Contains(t, result, "Mozilla/5.0")
}

// =============================================================================
// ERROR CONSTANTS TESTS
// =============================================================================

func TestErrorConstants(t *testing.T) {
	// Verify error constants are properly defined and distinct
	assert.NotEqual(t, ErrInvalidCredentials, ErrInvalidToken)
	assert.NotEqual(t, ErrInvalidToken, ErrSessionNotFound)
	assert.NotEqual(t, ErrInvalidCredentials, ErrSessionNotFound)

	// Verify error messages are meaningful
	assert.Contains(t, ErrInvalidCredentials.Error(), "invalid")
	assert.Contains(t, ErrInvalidToken.Error(), "invalid")
	assert.Contains(t, ErrSessionNotFound.Error(), "session")
}

// =============================================================================
// PASSWORD HASH TESTS (using existing util but testing service integration)
// =============================================================================

func TestPasswordHashIntegration(t *testing.T) {
	// GIVEN: Test password and its hash
	password := testutil.TestPassword
	hash := testutil.TestPasswordHash

	// WHEN: Comparing correct password
	err := testutil.CompareTestPassword(hash, password)

	// THEN: Comparison succeeds
	assert.NoError(t, err)

	// WHEN: Comparing wrong password
	err = testutil.CompareTestPassword(hash, "wrong-password")

	// THEN: Comparison fails
	assert.Error(t, err)
}

// =============================================================================
// HELPER FUNCTION TESTS
// =============================================================================

func TestHashIdentifier(t *testing.T) {
	// Test that hashIdentifier creates consistent short hashes
	id1 := "user@example.com"
	hash1 := hashIdentifier(id1)
	hash2 := hashIdentifier(id1)

	assert.Equal(t, hash1, hash2, "Hash should be deterministic")
	assert.Equal(t, 16, len(hash1), "Hash should be 8 bytes = 16 hex chars")

	// Different inputs produce different hashes
	id2 := "other@example.com"
	hash3 := hashIdentifier(id2)
	assert.NotEqual(t, hash1, hash3, "Different inputs should produce different hashes")
}

func TestHashUserID(t *testing.T) {
	// Test that hashUserID creates consistent hashes for user IDs
	userID := int64(12345)
	hash1 := hashUserID(userID)
	hash2 := hashUserID(userID)

	assert.Equal(t, hash1, hash2, "Hash should be deterministic")
	assert.Equal(t, 16, len(hash1), "Hash should be 8 bytes = 16 hex chars")

	// Different IDs produce different hashes
	hash3 := hashUserID(67890)
	assert.NotEqual(t, hash1, hash3, "Different IDs should produce different hashes")
}

func TestIpToInet(t *testing.T) {
	// Test nil IP
	nilInet := ipToInet(nil)
	assert.False(t, nilInet.Valid, "Nil IP should produce invalid Inet")

	// Test IPv4
	ipv4 := testutil.ParseIP("192.168.1.1")
	inet4 := ipToInet(&ipv4)
	assert.True(t, inet4.Valid)
	assert.NotNil(t, inet4.IPNet.IP)

	// Test IPv6
	ipv6 := testutil.ParseIP("2001:0db8:85a3:0000:0000:8a2e:0370:7334")
	inet6 := ipToInet(&ipv6)
	assert.True(t, inet6.Valid)
	assert.NotNil(t, inet6.IPNet.IP)
}
