package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// clusterizeRequest описывает тело POST /api/v1/admin/catalog/clusterize.
type clusterizeRequest struct {
	MinClusterSize float64 `json:"min_cluster_size"`
	UmapComponents float64 `json:"umap_components"`
	LlmTopK        float64 `json:"llm_top_k"`
}

// GetClusteringSettingsHandler обрабатывает GET /api/v1/admin/catalog/clusterize/settings.
// Возвращает текущие параметры кластеризации из system_settings (или дефолты при отсутствии).
// При ошибке БД возвращает 500, чтобы не маскировать сбой фиктивными дефолтами.
func (s *Server) GetClusteringSettingsHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "GetClusteringSettingsHandler")
	ctx := c.Request.Context()

	minClusterSize, err := s.settingsService.GetNumericSetting(ctx, "clustering_min_size", 5)
	if err != nil {
		logger.Errorf("Ошибка чтения clustering_min_size: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("ошибка чтения настроек кластеризации")))
		return
	}
	umapComponents, err := s.settingsService.GetNumericSetting(ctx, "clustering_umap_components", 15)
	if err != nil {
		logger.Errorf("Ошибка чтения clustering_umap_components: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("ошибка чтения настроек кластеризации")))
		return
	}
	llmTopK, err := s.settingsService.GetNumericSetting(ctx, "clustering_llm_top_k", 10)
	if err != nil {
		logger.Errorf("Ошибка чтения clustering_llm_top_k: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("ошибка чтения настроек кластеризации")))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"min_cluster_size": minClusterSize,
		"umap_components":  umapComponents,
		"llm_top_k":        llmTopK,
	})
}

// ProxyClusterizeHandler обрабатывает POST /api/v1/admin/catalog/clusterize.
// Сохраняет параметры кластеризации в system_settings и проксирует запрос в Python.
func (s *Server) ProxyClusterizeHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "ProxyClusterizeHandler")

	// 1. Читаем тело запроса (ограничиваем 1 MiB для защиты от DoS)
	const maxClusterizeBodyBytes int64 = 1 << 20 // 1 MiB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxClusterizeBodyBytes)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, errorResponse(fmt.Errorf("тело запроса превышает допустимый размер (%d байт)", maxClusterizeBodyBytes)))
			return
		}
		logger.Errorf("ошибка чтения тела запроса: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("ошибка чтения тела запроса")))
		return
	}

	// 2. Десериализуем параметры кластеризации
	var req clusterizeRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		logger.Errorf("ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %v", err)))
		return
	}

	// 3. Валидируем параметры кластеризации
	if req.MinClusterSize <= 0 || req.UmapComponents <= 0 || req.LlmTopK <= 0 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("min_cluster_size, umap_components, llm_top_k должны быть > 0")))
		return
	}
	if math.IsNaN(req.MinClusterSize) || math.IsInf(req.MinClusterSize, 0) ||
		math.IsNaN(req.UmapComponents) || math.IsInf(req.UmapComponents, 0) ||
		math.IsNaN(req.LlmTopK) || math.IsInf(req.LlmTopK, 0) {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("min_cluster_size, umap_components, llm_top_k не должны быть NaN или Inf")))
		return
	}

	// 4. Извлекаем user_id из JWT-контекста
	userID, exists := c.Get("user_id")
	if !exists {
		logger.Errorf("user_id отсутствует в контексте (middleware не установил)")
		c.JSON(http.StatusUnauthorized, errorResponse(fmt.Errorf("user not authenticated")))
		return
	}
	uid, ok := userID.(int64)
	if !ok {
		logger.Errorf("user_id имеет неожиданный тип: %T", userID)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("invalid user_id type")))
		return
	}
	updatedBy := strconv.FormatInt(uid, 10)

	// 5. Сохраняем параметры как новые дефолты
	if err := s.settingsService.SaveClusteringSettings(
		c.Request.Context(),
		req.MinClusterSize,
		req.UmapComponents,
		req.LlmTopK,
		updatedBy,
	); err != nil {
		logger.Errorf("ошибка сохранения настроек кластеризации: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("ошибка сохранения настроек кластеризации")))
		return
	}

	// 6. Проксируем запрос в Python
	pythonURL := s.config.Services.ParserService.URL + "/clusterize"

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pythonURL, bytes.NewReader(rawBody))
	if err != nil {
		logger.Errorf("ошибка создания HTTP-запроса для прокси: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("внутренняя ошибка сервера")))
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	logger.Infof("Проксирование запроса кластеризации на Python сервис (min_cluster_size=%g, umap_components=%g, llm_top_k=%g)",
		req.MinClusterSize, req.UmapComponents, req.LlmTopK)

	// Используем отдельный клиент без глобального Timeout — управление временем целиком
	// через context.WithTimeout выше (10 минут). s.httpClient имеет Timeout=5min что было бы
	// жёстким ограничением независимым от контекста.
	clusterizeClient := &http.Client{}
	resp, err := clusterizeClient.Do(proxyReq)
	if err != nil {
		logger.Errorf("сервис кластеризации недоступен: %v", err)
		c.JSON(http.StatusBadGateway, errorResponse(fmt.Errorf("сервис кластеризации временно недоступен")))
		return
	}
	defer resp.Body.Close()

	// 7. Проксируем ответ от Python обратно клиенту.
	// Статические hop-by-hop заголовки (RFC 2616 §13.5.1) не проксируются.
	// Динамические hop-by-hop заголовки, перечисленные в поле Connection (RFC 7230 §6.1), — тоже.
	hopByHop := map[string]bool{
		"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
		"Proxy-Authorization": true, "Te": true, "Trailer": true,
		"Transfer-Encoding": true, "Upgrade": true,
	}
	for _, v := range resp.Header.Values("Connection") {
		for _, token := range strings.Split(v, ",") {
			if h := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(token)); h != "" {
				hopByHop[h] = true
			}
		}
	}
	c.Status(resp.StatusCode)
	for key, values := range resp.Header {
		if hopByHop[key] {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		logger.Errorf("ошибка проксирования ответа Python сервиса: %v", err)
	}
}
