package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"golang.org/x/sync/errgroup"
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
	Lots    []LotResponse           `json:"lots"`
}

type LotResponse struct {
	ID            int64             `json:"id"`
	LotKey        string            `json:"lot_key"`
	LotTitle      string            `json:"lot_title"`
	TenderID      int64             `json:"tender_id"`
	KeyParameters map[string]string `json:"key_parameters"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// getTenderDetailsHandler - возвращает детали тендера и его лоты
func (s *Server) getTenderDetailsHandler(c *gin.Context) {
    id, err := strconv.ParseInt(c.Param("id"), 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный формат ID тендера")))
        return
    }

    // Добавим параметры пагинации из query
    var queryParams struct {
        Limit  int `form:"limit,default=100"`
        Offset int `form:"offset,default=0"`
    }
    if err := c.ShouldBindQuery(&queryParams); err != nil {
        c.JSON(http.StatusBadRequest, errorResponse(err))
        return
    }

    var (
        tenderDetails db.GetTenderDetailsRow
        lots          []db.Lot
    )
    
    // Используем errgroup для параллельного выполнения независимых запросов
    g, ctx := errgroup.WithContext(c.Request.Context())

    // Горутина для получения деталей тендера
    g.Go(func() error {
        var err error
        tenderDetails, err = s.store.GetTenderDetails(ctx, id)
        if err != nil {
            if err == sql.ErrNoRows {
                // Оборачиваем ошибку для корректной обработки ниже
                return fmt.Errorf("тендер с ID '%d' не найден: %w", id, err)
            }
            return err
        }
        return nil
    })

    // Горутина для получения списка лотов
    g.Go(func() error {
        params := db.ListLotsByTenderIDParams{
            TenderID: id,
            Limit:    int32(queryParams.Limit),
            Offset:   int32(queryParams.Offset),
        }
        var err error
        lots, err = s.store.ListLotsByTenderID(ctx, params)
        if err != nil {
            s.logger.Errorf("ошибка получения лотов для тендера %d: %v", id, err)
            return err
        }
        return nil
    })

    // Ждем завершения всех горутин и проверяем на ошибки
    if err := g.Wait(); err != nil {
        // Проверяем, была ли это ошибка "не найдено"
        if errors.Is(err, sql.ErrNoRows) {
            c.JSON(http.StatusNotFound, errorResponse(err))
        } else {
            c.JSON(http.StatusInternalServerError, errorResponse(err))
        }
        return
    }

    // Трансформация данных теперь в одну строку
    lotResponses := make([]LotResponse, len(lots))
    for i, lot := range lots {
        lotResponses[i] = newLotResponse(lot, s.logger)
    }

    // Собираем финальный ответ
    response := tenderPageResponse{
        Details: &tenderDetails,
        Lots:    lotResponses,
    }

    c.JSON(http.StatusOK, response)
}

// Используем указатели (*), чтобы отличить непереданное поле от поля, переданного как `null`.
type patchTenderRequest struct {
	CategoryID *int64  `json:"category_id" binding:"omitempty,gte=1"`
	Title      *string `json:"title" binding:"omitempty,min=3,max=255"`
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
		s.logger.Warnf("invalid patchTender input: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("invalid input: %v", err)))
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
