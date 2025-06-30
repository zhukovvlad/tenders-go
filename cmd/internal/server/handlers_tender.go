package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

type listTendersResponse struct {
	ID                 int64         `json:"id"`
	EtpID              string        `json:"etp_id"`
	Title              string        `json:"title"`
	DataPreparedOnDate string        `json:"data_prepared_on_date"` // <--- ТЕПЕРЬ ПРОСТО string
	ObjectAddress      string        `json:"object_address"`
	ExecutorName       string        `json:"executor_name"`
	ProposalsCount     int64         `json:"proposals_count"`
	CategoryID         sql.NullInt64 `json:"category_id"` // Добавили поле
}

func (s *Server) listTendersHandler(c *gin.Context) {
	// 1. Получаем параметры пагинации из URL query string.
	// Используем DefaultQuery, чтобы задать значения по умолчанию, если параметры не переданы.
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "10")

	// 2. Конвертируем строковые параметры в числа.
	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page")))
		return
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 || pageSize > 100 { // Ограничиваем максимальный размер страницы
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page_size (допустимо от 1 до 100)")))
		return
	}

	// 3. Создаем структуру с параметрами для sqlc.
	params := db.ListTendersParams{
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	dbTenders, err := s.store.ListTenders(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	apiResponse := make([]listTendersResponse, 0, len(dbTenders))

	for _, dbTender := range dbTenders {
		formattedDate := ""
		if dbTender.DataPreparedOnDate.Valid {
			// --- ИЗМЕНЕНИЕ ЗДЕСЬ ---
			// Форматируем дату в нужный вид "ДД-ММ-ГГГГ"
			formattedDate = dbTender.DataPreparedOnDate.Time.Format("02-01-2006")
		}

		apiTender := listTendersResponse{
			ID:                 dbTender.ID,
			EtpID:              dbTender.EtpID,
			Title:              dbTender.Title,
			DataPreparedOnDate: formattedDate,
			ObjectAddress:      dbTender.ObjectAddress,
			ExecutorName:       dbTender.ExecutorName,
			ProposalsCount:     dbTender.ProposalsCount,
			CategoryID:         dbTender.CategoryID,
		}
		apiResponse = append(apiResponse, apiTender)
	}

	c.JSON(http.StatusOK, apiResponse)
}

// Определяем структуры для нашего комплексного API-ответа
type tenderPageResponse struct {
	Details *db.GetTenderDetailsRow `json:"details"`
	Lots    []db.Lot                `json:"lots"`
}

// getTenderDetailsHandler - возвращает детали тендера и его лоты
func (s *Server) getTenderDetailsHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный формат ID тендера")))
		return
	}

	// === ШАГ 1: Получаем детали самого тендера ===

	tenderDetails, err := s.store.GetTenderDetails(c.Request.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, errorResponse(fmt.Errorf("тендер с ID '%d' не найден", id)))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	params := db.ListLotsByTenderIDParams{
		TenderID: id,
		Limit:    100, // Ограничиваем количество лотов для упрощения
		Offset:   0,   // Начинаем с первого лота
	}

	lots, err := s.store.ListLotsByTenderID(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения лотов для тендера %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if lots == nil {
		lots = make([]db.Lot, 0)
	}

	// === ШАГ 3: Собираем все в один комплексный ответ ===
	response := tenderPageResponse{
		Details: &tenderDetails,
		Lots:    lots,
	}

	c.JSON(http.StatusOK, response)
}

// Используем указатели (*), чтобы отличить непереданное поле от поля, переданного как `null`.
type patchTenderRequest struct {
	CategoryID *int64  `json:"category_id"`
	Title      *string `json:"title"`
	// В будущем сюда можно добавить любые другие поля, которые можно обновлять
}

// patchTenderHandler - УНИВЕРСАЛЬНЫЙ обработчик для частичного обновления тендера
func (s *Server) patchTenderHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID тендера")))
		return
	}

	// Шаг A: Парсим JSON в нашу простую и гибкую структуру `patchTenderRequest`
	var req patchTenderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// Шаг B: Создаем сложную структуру `UpdateTenderDetailsParams`, которую ожидает sqlc.
	// Изначально все поля в ней "невалидны" (Valid: false), и COALESCE их проигнорирует.
	params := db.UpdateTenderDetailsParams{
		ID: id,
	}

	// Шаг C: Вручную заполняем структуру для sqlc, проверяя, какие поля пришли от фронтенда.
	// Мы обновляем поле, только если оно было явно передано в JSON.

	if req.CategoryID != nil { // Если поле category_id пришло...
		params.CategoryID = sql.NullInt64{Int64: *req.CategoryID, Valid: true}
	}

	if req.Title != nil { // Если поле title пришло...
		params.Title = sql.NullString{String: *req.Title, Valid: true}
	}

	// ... в будущем здесь можно добавить проверки для других полей ...

	// Шаг D: Вызываем универсальную функцию обновления с правильно подготовленными параметрами.
	tender, err := s.store.UpdateTenderDetails(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка частичного обновления тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, tender)
}