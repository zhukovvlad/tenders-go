package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// clusterizeRequest описывает тело POST /admin/catalog/clusterize.
type clusterizeRequest struct {
	MinClusterSize float64 `json:"min_cluster_size"`
	UmapComponents float64 `json:"umap_components"`
	LlmTopK        float64 `json:"llm_top_k"`
}

// GetClusteringSettingsHandler обрабатывает GET /api/v1/admin/catalog/clusterize/settings.
// Возвращает текущие параметры кластеризации из system_settings (или дефолты).
func (s *Server) GetClusteringSettingsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	minClusterSize := s.settingsService.GetNumericSettingOrDefault(ctx, "clustering_min_size", 5)
	umapComponents := s.settingsService.GetNumericSettingOrDefault(ctx, "clustering_umap_components", 15)
	llmTopK := s.settingsService.GetNumericSettingOrDefault(ctx, "clustering_llm_top_k", 10)

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

	// 1. Читаем тело запроса
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
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

	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		logger.Errorf("сервис кластеризации недоступен: %v", err)
		c.JSON(http.StatusBadGateway, errorResponse(fmt.Errorf("сервис кластеризации временно недоступен")))
		return
	}
	defer resp.Body.Close()

	// 7. Проксируем ответ от Python обратно клиенту (hop-by-hop заголовки не проксируются — RFC 2616 §13.5.1)
	hopByHop := map[string]bool{
		"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
		"Proxy-Authorization": true, "Te": true, "Trailer": true,
		"Transfer-Encoding": true, "Upgrade": true,
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
	io.Copy(c.Writer, resp.Body)
}
