package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
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

type WinnerResponse struct {
	ID             int64   `json:"id"`              // ID записи победителя (для редактирования/удаления)
	ContractorName string  `json:"contractor_name"` // Название подрядчика
	Inn            string  `json:"inn"`             // ИНН
	Price          *string `json:"price,omitempty"` // Цена (строкой, чтобы не терять копейки), nil если не установлена
	Rank           *int32  `json:"rank,omitempty"`  // Место, nil если не установлено
}

type LotResponse struct {
	ID            int64             `json:"id"`
	LotKey        string            `json:"lot_key"`
	LotTitle      string            `json:"lot_title"`
	TenderID      int64             `json:"tender_id"`
	KeyParameters map[string]string `json:"key_parameters"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
	Winners       []WinnerResponse  `json:"winners"`
}

// getTenderDetailsHandler - возвращает детали тендера и его лоты
func (s *Server) getTenderDetailsHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный формат ID тендера")))
		return
	}

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
		// НОВАЯ ПЕРЕМЕННАЯ: "сырые" данные о победителях из БД
		winnersRaw []db.GetWinnersByTenderIDRow
	)

	g, ctx := errgroup.WithContext(c.Request.Context())

	// 1. Детали тендера
	g.Go(func() error {
		var err error
		tenderDetails, err = s.store.GetTenderDetails(ctx, id)
		if err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("тендер с ID '%d' не найден: %w", id, err)
			}
			return err
		}
		return nil
	})

	// 2. Лоты
	g.Go(func() error {
		params := db.ListLotsByTenderIDParams{
			TenderID: id,
			Limit:    int32(queryParams.Limit),
			Offset:   int32(queryParams.Offset),
		}
		var err error
		lots, err = s.store.ListLotsByTenderID(ctx, params)
		if err != nil {
			return err
		}
		return nil
	})

	// 3. НОВАЯ ГОРУТИНА: Победители
	g.Go(func() error {
		var err error
		winnersRaw, err = s.store.GetWinnersByTenderID(ctx, id)
		if err != nil {
			// Логируем ошибку, но не валим весь запрос, если с победителями что-то не так
			s.logger.Errorf("ошибка получения победителей для тендера %d: %v", id, err)
			return nil
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, errorResponse(err))
		} else {
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
		return
	}

	// 4. Сборка данных: Группируем победителей по LotID
	winnersMap := make(map[int64][]WinnerResponse)
	for _, w := range winnersRaw {
		// Обработка nullable полей (Price, Rank могут быть NULL в БД)
		// Используем pointers, чтобы отличить NULL от реального нулевого значения
		var pricePtr *string
		if w.Price.Valid {
			pricePtr = &w.Price.String
		}

		var rankPtr *int32
		if w.Rank.Valid {
			rankPtr = &w.Rank.Int32
		}

		item := WinnerResponse{
			ID:             w.WinnerID, // Поле из SQL: w.id AS winner_id
			ContractorName: w.ContractorName,
			Inn:            w.ContractorInn,
			Price:          pricePtr,
			Rank:           rankPtr,
		}
		winnersMap[w.LotID] = append(winnersMap[w.LotID], item)
	}

	// 5. Заполнение ответа
	lotResponses := make([]LotResponse, len(lots))
	for i, lot := range lots {
		lr := newLotResponse(lot, s.logger)

		// Если для лота есть победители — вставляем их
		if winners, ok := winnersMap[lot.ID]; ok {
			lr.Winners = winners
		}

		lotResponses[i] = lr
	}

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

// --- CRUD Победителей ---

type createWinnerRequest struct {
	ProposalID int64  `json:"proposal_id" binding:"required"`
	Rank       int32  `json:"rank" binding:"required,gte=1"`
	Notes      string `json:"notes"`
}

type updateWinnerRequest struct {
	Rank  *int32  `json:"rank"`
	Notes *string `json:"notes"`
}

// POST /api/v1/lots/:lotId/winners
func (s *Server) createWinnerHandler(c *gin.Context) {
	lotID, err := strconv.ParseInt(c.Param("lotId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID лота")))
		return
	}

	var req createWinnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// 1. Валидация: Принадлежит ли proposal этому лоту?
	paramsCheck := db.CheckProposalBelongsToLotParams{
		ID:    req.ProposalID,
		LotID: lotID,
	}
	isValid, err := s.store.CheckProposalBelongsToLot(c.Request.Context(), paramsCheck)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	if !isValid {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("proposal_id %d не принадлежит лоту %d", req.ProposalID, lotID)))
		return
	}

	// 2. Создание
	createParams := db.CreateWinnerParams{
		ProposalID: req.ProposalID,
		Rank:       sql.NullInt32{Int32: req.Rank, Valid: true},
		Notes:      sql.NullString{String: req.Notes, Valid: req.Notes != ""},
	}

	winner, err := s.store.CreateWinner(c.Request.Context(), createParams)
	if err != nil {
		// Проверяем, является ли ошибка (или обернутая ошибка) PostgreSQL ошибкой нарушения уникальности
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, errorResponse(fmt.Errorf("это предложение уже является победителем")))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusCreated, winner)
}

// PATCH /api/v1/winners/:winnerId
func (s *Server) updateWinnerHandler(c *gin.Context) {
	winnerID, err := strconv.ParseInt(c.Param("winnerId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID победителя")))
		return
	}

	var req updateWinnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// Требуем хотя бы одно поле для обновления
	if req.Rank == nil && req.Notes == nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("необходимо указать хотя бы одно поле для обновления")))
		return
	}

	params := db.UpdateWinnerDetailsParams{
		ID: winnerID,
	}
	// Заполняем только переданные поля
	if req.Rank != nil {
		if *req.Rank < 1 {
			c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("rank должен быть >= 1")))
			return
		}
		params.Rank = sql.NullInt32{Int32: *req.Rank, Valid: true}
	}
	if req.Notes != nil {
		// Пустая строка сохраняется как NULL для консистентности с createWinnerHandler
		params.Notes = sql.NullString{String: *req.Notes, Valid: *req.Notes != ""}
	}

	updatedWinner, err := s.store.UpdateWinnerDetails(c.Request.Context(), params)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, errorResponse(fmt.Errorf("победитель не найден")))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, updatedWinner)
}

// DELETE /api/v1/winners/:winnerId
func (s *Server) deleteWinnerHandler(c *gin.Context) {
	winnerID, err := strconv.ParseInt(c.Param("winnerId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID победителя")))
		return
	}

	// Используем метод DeleteWinnerByID из winner.sql, который возвращает удаленную запись
	deletedWinner, err := s.store.DeleteWinnerByID(c.Request.Context(), winnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, errorResponse(fmt.Errorf("победитель не найден")))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, deletedWinner)
}
