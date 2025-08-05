package server

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) ProxyUploadHandler(c *gin.Context) {
	// Получаем файл из формы
	sourceFile, sourceHeader, err := c.Request.FormFile("tenderFile")
	if err != nil {
		s.logger.Errorf("ошибка получения файла из формы: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("файл 'tenderFile' не предоставлен")))
		return
	}
	defer sourceFile.Close()

	// Получаем параметр enable_ai (по умолчанию true, как в Python)
	enableAI := c.DefaultPostForm("enable_ai", "true")

	// Создаем новый multipart writer для прокси запроса
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Добавляем файл (меняем имя поля с tenderFile на file для Python)
	part, err := writer.CreateFormFile("file", sourceHeader.Filename)
	if err != nil {
		s.logger.Errorf("ошибка создания form-file для прокси: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("внутренняя ошибка сервера")))
		return
	}

	if _, err = io.Copy(part, sourceFile); err != nil {
		s.logger.Errorf("ошибка копирования файла для прокси: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("внутренняя ошибка сервера")))
		return
	}

	// Добавляем параметр enable_ai
	if err := writer.WriteField("enable_ai", enableAI); err != nil {
		s.logger.Errorf("ошибка добавления поля enable_ai: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("внутренняя ошибка сервера")))
		return
	}

	if err := writer.Close(); err != nil {
		s.logger.Errorf("ошибка закрытия multipart writer: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("внутренняя ошибка сервера")))
		return
	}

	// Отправляем запрос на Python сервис
	pythonParserBaseUrl := s.config.Services.ParserService.URL
	pythonParserUrl := fmt.Sprintf("%s/parse-tender/", pythonParserBaseUrl)

	req, err := http.NewRequest(http.MethodPost, pythonParserUrl, body)
	if err != nil {
		s.logger.Errorf("ошибка создания HTTP-запроса для прокси: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("внутренняя ошибка сервера")))
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Логируем информацию о запросе
	s.logger.Infof("Проксирование файла %s на Python сервис (enable_ai=%s)", sourceHeader.Filename, enableAI)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Errorf("сервис парсера недоступен: %v", err)
		c.JSON(http.StatusBadGateway, errorResponse(fmt.Errorf("сервис обработки файлов временно недоступен")))
		return
	}
	defer resp.Body.Close()

	// Перенаправляем ответ от Python обратно клиенту
	c.Status(resp.StatusCode)
	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	io.Copy(c.Writer, resp.Body)
}

func (s *Server) GetTaskStatusHandler(c *gin.Context) {
	taskID := c.Param("task_id")
	pythonParserBaseUrl := s.config.Services.ParserService.URL
	pythonStatusURL := fmt.Sprintf("%s/tasks/%s/status", pythonParserBaseUrl, taskID)

	req, err := http.NewRequest("GET", pythonStatusURL, nil)
	if err != nil {
		s.logger.Errorf("ошибка создания HTTP-запроса для статуса: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("внутренняя ошибка сервера")))
		return
	}

	// Используем созданный при старте сервера httpClient
	resp, err := s.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse(fmt.Errorf("сервис обработки файлов временно недоступен")))
		return
	}
	defer resp.Body.Close()

	// Просто перенаправляем ответ от Python обратно клиенту
	c.Status(resp.StatusCode)
	for key, values := range resp.Header {
		c.Writer.Header().Set(key, values[0])
	}
	io.Copy(c.Writer, resp.Body)
}
