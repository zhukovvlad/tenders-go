package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestServer представляет тестовый HTTP сервер
type TestServer struct {
	Router *gin.Engine
}

// NewTestServer создает новый тестовый сервер
func NewTestServer() *TestServer {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	return &TestServer{
		Router: router,
	}
}

// MakeRequest выполняет HTTP запрос к тестовому серверу
func (s *TestServer) MakeRequest(
	t *testing.T,
	method, path string,
	body interface{},
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		require.NoError(t, err, "Failed to marshal request body")
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, path, bodyReader)
	require.NoError(t, err, "Failed to create request")

	// Устанавливаем заголовки
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Выполняем запрос
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	return w
}

// ParseResponseBody парсит JSON ответ в структуру
func ParseResponseBody(t *testing.T, body *bytes.Buffer, target interface{}) {
	t.Helper()

	err := json.Unmarshal(body.Bytes(), target)
	require.NoError(t, err, "Failed to parse response body")
}

// AssertResponse проверяет код статуса и парсит тело ответа
func AssertResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int, target interface{}) {
	t.Helper()

	require.Equal(t, expectedStatus, w.Code, "Unexpected status code")

	if target != nil && w.Body.Len() > 0 {
		ParseResponseBody(t, w.Body, target)
	}
}

// AssertErrorResponse проверяет ответ с ошибкой
func AssertErrorResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int, expectedMessage string) {
	t.Helper()

	require.Equal(t, expectedStatus, w.Code, "Unexpected status code")

	var response map[string]interface{}
	ParseResponseBody(t, w.Body, &response)

	if expectedMessage != "" {
		errVal, ok := response["error"].(string)
		require.True(t, ok, "Expected 'error' field to be a string, got: %T", response["error"])
		AssertContains(t, errVal, expectedMessage)
	}
}

// MakeGetRequest выполняет GET запрос
func (s *TestServer) MakeGetRequest(t *testing.T, path string, headers map[string]string) *httptest.ResponseRecorder {
	return s.MakeRequest(t, http.MethodGet, path, nil, headers)
}

// MakePostRequest выполняет POST запрос
func (s *TestServer) MakePostRequest(t *testing.T, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	return s.MakeRequest(t, http.MethodPost, path, body, headers)
}

// MakePutRequest выполняет PUT запрос
func (s *TestServer) MakePutRequest(t *testing.T, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	return s.MakeRequest(t, http.MethodPut, path, body, headers)
}

// MakeDeleteRequest выполняет DELETE запрос
func (s *TestServer) MakeDeleteRequest(t *testing.T, path string, headers map[string]string) *httptest.ResponseRecorder {
	return s.MakeRequest(t, http.MethodDelete, path, nil, headers)
}

// WithAuth добавляет JWT токен в заголовки
func WithAuth(token string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + token,
	}
}

// MergeHeaders объединяет несколько map с заголовками
func MergeHeaders(headers ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, h := range headers {
		for k, v := range h {
			result[k] = v
		}
	}
	return result
}

// GetJSONString возвращает JSON строку из body
func GetJSONString(t *testing.T, v interface{}) string {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err, "Failed to marshal to JSON")
	return string(data)
}

// CreateTestContext создает тестовый Gin контекст
func CreateTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}
