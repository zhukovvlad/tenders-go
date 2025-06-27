package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/util"
)

type listTendersResponse struct {
	ID                 int64  `json:"id"`
	EtpID              string `json:"etp_id"`
	Title              string `json:"title"`
	DataPreparedOnDate string `json:"data_prepared_on_date"` // <--- ТЕПЕРЬ ПРОСТО string
	ObjectAddress      string `json:"object_address"`
	ExecutorName       string `json:"executor_name"`
	ProposalsCount     int64  `json:"proposals_count"`
	CategoryID         sql.NullInt64 `json:"category_id"` // Добавили поле
}

func (s *Server) HomeHandler(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Welcome to the Tenders API",
	})
}

func (s *Server) ImportTenderHandler(c *gin.Context) {
	handlerLogger := s.logger.WithField("handler", "ImportTenderHandler")
	handlerLogger.Info("Начало обработки запроса на импорт тендера")

	var tenderPayload api_models.FullTenderData
	if err := c.ShouldBindJSON(&tenderPayload); err != nil {
		handlerLogger.Errorf("Ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %w", err)))
		return
	}

	requestLogger := handlerLogger.WithField("etp_id_from_json", tenderPayload.TenderID)
	requestLogger.Infof("Получены данные для тендера ETP_ID: %s", tenderPayload.TenderID)

	ctx := c.Request.Context() // Используем контекст из Gin запроса

	err := s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		var txErr error
		
		// 1. Обработка и вставка/обновление Object
		objectTitle := tenderPayload.TenderObject
		objectAddress := tenderPayload.TenderAddress

		if strings.TrimSpace(objectTitle) == "" {
			return fmt.Errorf("название объекта (tender_object) не может быть пустым для тендера %s", tenderPayload.TenderID)
		}
		if strings.TrimSpace(objectAddress) == "" {
			return fmt.Errorf("адрес объекта (tender_address) не может быть пустым для тендера %s", tenderPayload.TenderID)
		}
		dbObject, txErr := s.tenderService.GetOrCreateObject(ctx, qtx, objectTitle, objectAddress) // Передаем логгер
		if txErr != nil {
			// Логгирование ошибки уже произошло внутри getOrCreateObject
			return fmt.Errorf("обработка объекта для тендера %s: %w", tenderPayload.TenderID, txErr)
		}

		// 2. Обработка и вставка/обновление Executor
		executorName := tenderPayload.ExecutorData.ExecutorName
		executorPhone := tenderPayload.ExecutorData.ExecutorPhone
		if strings.TrimSpace(executorName) == "" {
			return fmt.Errorf("имя исполнителя (executor_name) не может быть пустым для тендера %s", tenderPayload.TenderID)
		}
		if strings.TrimSpace(executorPhone) == "" {
			return fmt.Errorf("телефон исполнителя (executor_phone) не может быть пустым для тендера %s", tenderPayload.TenderID)
		}
		dbExecutor, txErr := s.tenderService.GetOrCreateExecutor(ctx, qtx, executorName, executorPhone) // Передаем логгер
		if txErr != nil {
			return fmt.Errorf("обработка исполнителя для тендера %s: %w", tenderPayload.TenderID, txErr)
		}

		// 3. Парсинг DataPreparedOnDate
		var preparedDate sql.NullTime
		if tenderPayload.ExecutorData.ExecutorDate != "" {
			t, errDate := time.Parse("02.01.2006 15:04:05", tenderPayload.ExecutorData.ExecutorDate)
			if errDate != nil {
				requestLogger.Warnf("Не удалось распарсить data_prepared_on_date (executor_date) '%s': %v. Будет NULL.", 
					tenderPayload.ExecutorData.ExecutorDate, errDate)
			} else {
				preparedDate = sql.NullTime{Time: t, Valid: true}
			}
		}

		// 4. Вставка/обновление основной информации о Тендере
		if strings.TrimSpace(tenderPayload.TenderID) == "" {
			return fmt.Errorf("tender_id (etp_id) не может быть пустым")
		}
		if strings.TrimSpace(tenderPayload.TenderTitle) == "" {
			return fmt.Errorf("tender_title не может быть пустым для tender_id %s", tenderPayload.TenderID)
		}
		tenderParams := db.UpsertTenderParams{ // Используем ваш UpsertTender
			EtpID:              tenderPayload.TenderID,
			Title:              tenderPayload.TenderTitle,
			ObjectID:           dbObject.ID,
			ExecutorID:         dbExecutor.ID,
			DataPreparedOnDate: preparedDate,
		}

		dbTender, txErr := qtx.UpsertTender(ctx, tenderParams)
		if txErr != nil {
			return fmt.Errorf("не удалось создать/обновить тендер с ETP_ID %s: %w", tenderPayload.TenderID, txErr)
		}
		requestLogger.Infof("Успешно создан/обновлен тендер: ID=%d, ETP_ID=%s", dbTender.ID, dbTender.EtpID)
		
		// 5. Обработка LotsData
		for lotKey, lotAPI := range tenderPayload.LotsData {
			lotLogger := requestLogger.WithField("lot_key", lotKey)
			lotLogger.Infof("Обработка лота: %s", lotAPI.LotTitle)

			dbLot, txErr := qtx.UpsertLot(ctx, db.UpsertLotParams{
				TenderID: dbTender.ID,
				LotKey: lotKey,
				LotTitle: lotAPI.LotTitle,
			})
			if txErr != nil {
				return fmt.Errorf("не удалось создать/обновить лот '%s' для тендера %s: %w", lotKey, dbTender.EtpID, txErr)
			}
			lotLogger.Infof("Обработан Lot ID: %d", dbLot.ID)

			organizerInn := "0000000000" // Замените на реальный INN организатора, если есть
			organizerTitle := "Initiator" // Замените на реальное название организатора, если есть
			dbOrganizer, txErr := s.tenderService.GetOrCreateContractor(ctx, qtx, organizerInn, organizerTitle, "N/A", "N/A") // Пустые адрес и аккредитация
			if txErr != nil {
				return fmt.Errorf("не удалось получить/создать подрядчика-организатора: %w", txErr)
			}

			if lotAPI.BaseLineProposal.AdditionalInfo != nil {
				baselineLogger := lotLogger.WithField("proposal_type", "badeline")
				baselineLogger.Info("Обработка расчетной стоимости")
				dbBaselineProposal, txErr := qtx.UpsertProposal(ctx, db.UpsertProposalParams{
					LotID: dbLot.ID,
					ContractorID: dbOrganizer.ID,
					IsBaseline: true,
					ContractorCoordinate: util.NullableString(util.NilIfEmpty(lotAPI.BaseLineProposal.ContractorCoordinate)),
					ContractorWidth: util.NullableInt32(util.IntPointerOrNil(lotAPI.BaseLineProposal.ContractorWidth)),
					ContractorHeight: util.NullableInt32(util.IntPointerOrNil(lotAPI.BaseLineProposal.ContractorHeight)),
				})
				if txErr != nil {
					return fmt.Errorf("не удалось создать/обновить базовое предложение для лота '%s': %w", lotKey, txErr)
				}
				baselineLogger.Infof("Обработано базовое предложение ID: %d", dbBaselineProposal.ID)

				// Обработка AdditionalInfo для базового предложения
				
				if txErr = s.tenderService.ProcessProposalAdditionalInfo(ctx, qtx, dbBaselineProposal.ID, lotAPI.BaseLineProposal.AdditionalInfo); txErr != nil {
					return fmt.Errorf("обработка доп.инфо для базового предложения лота '%s': %w", lotKey, txErr)
				}
				// Обработка ContractorItems (Positions и Summary) для базового предложения
				if txErr = s.tenderService.ProcessContractorItems(ctx, qtx, dbBaselineProposal.ID, lotAPI.BaseLineProposal.ContractorItems); txErr != nil {
					return fmt.Errorf("обработка позиций/итогов для базового предложения лота '%s': %w", lotKey, txErr)
				}
			}

			for contractorJsonKey, proposalDetailsAPI := range lotAPI.ProposalData {
				contractorLogger := lotLogger.WithField("contractor_json_key", contractorJsonKey).WithField("contractor_title", proposalDetailsAPI.Title)
				contractorLogger.Info("Обработка предложения от подрядчика")

				dbContractor, txErr := s.tenderService.GetOrCreateContractor(ctx, qtx,
					proposalDetailsAPI.Inn,
					proposalDetailsAPI.Title,
					proposalDetailsAPI.Address,
					proposalDetailsAPI.Accreditation,
				)
				if txErr != nil {
					return fmt.Errorf("не удалось получить/создать подрядчика '%s' (ИНН %s): %w", proposalDetailsAPI.Title, proposalDetailsAPI.Inn, txErr)
				}

				dbProposal, txErr := qtx.UpsertProposal(ctx, db.UpsertProposalParams{
					LotID:                  dbLot.ID,
					ContractorID:           dbContractor.ID,
					IsBaseline:             false,
					ContractorCoordinate:   util.NullableString(util.NilIfEmpty(proposalDetailsAPI.ContractorCoordinate)),
					ContractorWidth:        util.NullableInt32(util.IntPointerOrNil(proposalDetailsAPI.ContractorWidth)),
					ContractorHeight:       util.NullableInt32(util.IntPointerOrNil(proposalDetailsAPI.ContractorHeight)),
				})
				if txErr != nil {
					return fmt.Errorf("не удалось создать/обновить предложение от подрядчика '%s' для лота '%s': %w", proposalDetailsAPI.Title, lotKey, txErr)
				}
				contractorLogger.Infof("Обработано предложение ID: %d", dbProposal.ID)

				// Обработка AdditionalInfo для предложения подрядчика
				if txErr = s.tenderService.ProcessProposalAdditionalInfo(ctx, qtx, dbProposal.ID, proposalDetailsAPI.AdditionalInfo); txErr != nil {
					return fmt.Errorf("обработка доп.инфо для предложения '%s' лота '%s': %w", proposalDetailsAPI.Title, lotKey, txErr)
				}
				// Обработка ContractorItems (Positions и Summary) для предложения подрядчика
				if txErr = s.tenderService.ProcessContractorItems(ctx, qtx, dbProposal.ID, proposalDetailsAPI.ContractorItems); txErr != nil {
					return fmt.Errorf("обработка позиций/итогов для предложения '%s' лота '%s': %w", proposalDetailsAPI.Title, lotKey, txErr)
				}
			}
		}
		return nil
	})

	if err != nil {
		requestLogger.Errorf("Транзакция при импорте тендера не удалась: %v", err) // Используем requestLogger с ETP_ID
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	requestLogger.Info("Базовая информация о тендере успешно обработана.")
	c.JSON(http.StatusCreated, gin.H{
		"message":   "Базовая информация о тендере успешно обработана",
		"tender_id": tenderPayload.TenderID,
	})
		
}

func (s *Server) getStatsHandler (c *gin.Context) {
	count, err := s.store.GetTendersCount(c.Request.Context())
	if err != nil {
		s.logger.Errorf("Ошибка при получении количества тендеров: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenders_count": count,
		"message":       "Статистика успешно получена",
	})
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

// listTenderTypesHandler - обработчик для получения списка типов тендеров с пагинацией.
func (s *Server) listTenderTypesHandler(c *gin.Context) {
	// Получаем параметры пагинации из URL
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20") // Можно задать другой размер страницы по умолчанию

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page")))
		return
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page_size (допустимо от 1 до 100)")))
		return
	}

	// Создаем структуру с параметрами для sqlc
	params := db.ListTenderTypesParams{
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	// Вызываем метод из sqlc
	tenderTypes, err := s.store.ListTenderTypes(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка типов тендеров: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// В этот раз нам не нужно преобразовывать данные, так как структура `db.TenderType`
	// уже подходит для JSON-ответа. Возвращаем ее напрямую.
	if tenderTypes == nil {
		tenderTypes = make([]db.TenderType, 0)
	}

	c.JSON(http.StatusOK, tenderTypes)
}

// createTenderTypeRequest определяет структуру входящего JSON при создании типа
type createTenderTypeRequest struct {
	Title string `json:"title" binding:"required"`
}

// createTenderTypeHandler - обработчик для создания нового типа тендера
func (s *Server) createTenderTypeHandler(c *gin.Context) {
	var req createTenderTypeRequest

	// Парсим и валидируем входящий JSON
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}
	
	// --- ИЗМЕНЕНИЕ ЗДЕСЬ ---
	// Вместо создания структуры UpsertTenderTypeParams,
	// мы передаем req.Title (тип string) напрямую,
	// как и ожидает сгенерированная sqlc функция.
	tenderType, err := s.store.UpsertTenderType(c.Request.Context(), req.Title)
	
	if err != nil {
		s.logger.Errorf("ошибка при создании/обновлении типа тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// Возвращаем созданный/найденный объект со статусом 201 Created
	c.JSON(http.StatusCreated, tenderType)
}

// Структура для входящего JSON при обновлении
type updateTenderTypeRequest struct {
	Title string `json:"title" binding:"required"`
}

// updateTenderTypeHandler - обновляет существующий тип тендера
func (s *Server) updateTenderTypeHandler(c *gin.Context) {
	// Получаем ID из URL
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}

	var req updateTenderTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	params := db.UpdateTenderTypeParams{
		ID:    id,
		Title: req.Title,
	}

	updatedType, err := s.store.UpdateTenderType(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка обновления типа тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, updatedType)
}

// deleteTenderTypeHandler - удаляет тип тендера
func (s *Server) deleteTenderTypeHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}

	err = s.store.DeleteTenderType(c.Request.Context(), id)
	if err != nil {
		s.logger.Errorf("ошибка удаления типа тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// При успешном удалении возвращаем статус 204 No Content
	c.Status(http.StatusNoContent)
}

// Определяем структуры для нашего комплексного API-ответа
type tenderPageResponse struct {
	Details *db.GetTenderDetailsRow `json:"details"`
	Lots    []db.Lot              `json:"lots"`
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

	// === ШАГ 2: Получаем список лотов для этого тендера ===
	// lots, err := s.store.ListLotsForTender(c.Request.Context(), id)
	// if err != nil {
	// 	s.logger.Errorf("ошибка получения лотов для тендера %d: %v", id, err)
	// 	c.JSON(http.StatusInternalServerError, errorResponse(err))
	// 	return
	// }

	// if lots == nil {
	// 	lots = make([]db.Lot, 0)
	// }
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

// Обновляем структуру для API-ответа
type proposalResponse struct {
	ProposalID      int64           `json:"proposal_id"`
	ContractorID    int64           `json:"contractor_id"`
	ContractorTitle string          `json:"contractor_title"`
	ContractorInn   string          `json:"contractor_inn"`
	IsWinner        bool            `json:"is_winner"`
	TotalCost       *float64        `json:"total_cost"`
    // Добавляем поле для всего объекта additional_info
	AdditionalInfo  json.RawMessage `json:"additional_info"` 
}

// listProposalsHandler - обработчик для получения списка предложений по тендеру
func (s *Server) listProposalsHandler(c *gin.Context) {
	tenderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID тендера")))
		return
	}

	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20")

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		pageID = 1
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 {
		pageSize = 20
	}

	params := db.ListProposalsForTenderParams{
		TenderID: tenderID,
		Limit:    int32(pageSize),
		Offset:   (int32(pageID) - 1) * int32(pageSize),
	}

	dbProposals, err := s.store.ListProposalsForTender(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка предложений: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// --- ЛОГИКА ПРЕОБРАЗОВАНИЯ ДАННЫХ ---
	apiResponse := make([]proposalResponse, 0, len(dbProposals))
	for _, p := range dbProposals {
		// Создаем экземпляр нашей чистой структуры для API
		apiProp := proposalResponse{
			ProposalID:      p.ProposalID,
			ContractorID:    p.ContractorID,
			ContractorTitle: p.ContractorTitle,
			ContractorInn:   p.ContractorInn,
			IsWinner:        p.IsWinner,
			AdditionalInfo:  p.AdditionalInfo,
		}

		// Проверяем, что строка с ценой не NULL
		if p.TotalCost.Valid {
			// Конвертируем строку (p.TotalCost.String) в float64
			cost, err := strconv.ParseFloat(p.TotalCost.String, 64)
			if err == nil { // Если конвертация прошла успешно
				apiProp.TotalCost = &cost // Присваиваем указатель на полученное число
			}
            // Если конвертация не удалась, поле останется nil, что тоже корректно
		}

		apiResponse = append(apiResponse, apiProp)
	}

	c.JSON(http.StatusOK, apiResponse)
}

// listProposalsForLotHandler - обработчик для получения списка предложений по ID лота
func (s *Server) listProposalsForLotHandler(c *gin.Context) {
	// Получаем ID лота из URL
	lotID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID лота")))
		return
	}

	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20")

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		pageID = 1
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 {
		pageSize = 20
	}

	params := db.ListRichProposalsForLotParams{ // <-- Используем правильный тип параметров
		LotID:  lotID,
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	// Вызываем новую функцию, сгенерированную sqlc
	dbProposals, err := s.store.ListRichProposalsForLot(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка предложений для лота %d: %v", lotID, err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// --- Логика преобразования данных (остается такой же, как мы делали раньше) ---
	// Она преобразует "сырые" данные из БД в "чистый" JSON для API
	apiResponse := make([]proposalResponse, 0, len(dbProposals))
	for _, p := range dbProposals {
		apiProp := proposalResponse{
			ProposalID:      p.ProposalID,
			ContractorID:    p.ContractorID,
			ContractorTitle: p.ContractorTitle,
			ContractorInn:   p.ContractorInn,
			IsWinner:        p.IsWinner,
			AdditionalInfo:  p.AdditionalInfo,
		}
		if p.TotalCost.Valid {
			// Конвертируем строку (p.TotalCost.String) в float64
			cost, err := strconv.ParseFloat(p.TotalCost.String, 64)
			if err == nil { // Если конвертация прошла успешно
				apiProp.TotalCost = &cost // Присваиваем указатель на полученное число
			}
            // Если конвертация не удалась, поле останется nil, что тоже корректно
		}
		apiResponse = append(apiResponse, apiProp)
	}
	// ------------------------------------------------------------------------

	c.JSON(http.StatusOK, apiResponse)
}

// listTenderChaptersHandler - обработчик для получения списка разделов тендеров с пагинацией.
func (s *Server) listTenderChaptersHandler(c *gin.Context) {
	// Получаем параметры пагинации из URL
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20")

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page")))
		return
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page_size (допустимо от 1 до 100)")))
		return
	}

	params := db.ListTenderChaptersParams{
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	// Вызываем новую функцию из sqlc
	tenderChapters, err := s.store.ListTenderChapters(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка разделов тендеров: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if tenderChapters == nil {
		tenderChapters = make([]db.ListTenderChaptersRow, 0)
	}

	c.JSON(http.StatusOK, tenderChapters)
}

// Структура для входящего JSON остается той же
type createTenderChapterRequest struct {
	Title        string `json:"title" binding:"required"`
	TenderTypeID int64  `json:"tender_type_id" binding:"required,min=1"`
}

// createTenderChapterHandler - обработчик для создания/обновления раздела тендера
func (s *Server) createTenderChapterHandler(c *gin.Context) {
	var req createTenderChapterRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// --- ИЗМЕНЕНИЕ: Вызываем UpsertTenderChapter вместо CreateTenderChapter ---
	params := db.UpsertTenderChapterParams{ // sqlc сгенерирует эту структуру для Upsert
		Title:        req.Title,
		TenderTypeID: req.TenderTypeID,
	}

	// Вызываем более надежный метод Upsert
	chapter, err := s.store.UpsertTenderChapter(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка при создании/обновлении раздела тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// Возвращаем созданный/обновленный объект
	c.JSON(http.StatusCreated, chapter)
}

// Структура для входящего JSON при обновлении раздела
type updateTenderChapterRequest struct {
	Title        string `json:"title" binding:"required"`
	TenderTypeID int64  `json:"tender_type_id" binding:"required,min=1"`
}

// updateTenderChapterHandler - обновляет существующий раздел тендера
func (s *Server) updateTenderChapterHandler(c *gin.Context) {
	// Получаем ID из URL
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID раздела")))
		return
	}

	var req updateTenderChapterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	params := db.UpdateTenderChapterParams{
		ID:           id,
		Title:        sql.NullString{String: req.Title, Valid: true},
		TenderTypeID: sql.NullInt64{Int64: req.TenderTypeID, Valid: true},
	}

	updatedChapter, err := s.store.UpdateTenderChapter(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка обновления раздела тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, updatedChapter)
}

// deleteTenderChapterHandler - удаляет раздел тендера
func (s *Server) deleteTenderChapterHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID раздела")))
		return
	}

	err = s.store.DeleteTenderChapter(c.Request.Context(), id)
	if err != nil {
		s.logger.Errorf("ошибка удаления раздела тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.Status(http.StatusNoContent)
}

// --- ХЭНДЛЕРЫ ДЛЯ TENDER_CATEGORIES ---

// Структура для JSON-запросов на создание/обновление категории
type tenderCategoryRequest struct {
	Title           string `json:"title" binding:"required"`
	TenderChapterID int64  `json:"tender_chapter_id" binding:"required,min=1"`
}

// listTenderCategoriesHandler получает список всех категорий
func (s *Server) listTenderCategoriesHandler(c *gin.Context) {
	// Логика пагинации
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "100") // Берем побольше для справочника

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil { c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page"))); return }

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 { c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page_size"))); return }

	params := db.ListTenderCategoriesParams{
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	categories, err := s.store.ListTenderCategories(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	c.JSON(http.StatusOK, categories)
}

// createTenderCategoryHandler создает новую категорию
func (s *Server) createTenderCategoryHandler(c *gin.Context) {
	var req tenderCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}
    // Используем ваш надежный Upsert
	params := db.UpsertTenderCategoryParams{
		Title:           req.Title,
		TenderChapterID: req.TenderChapterID,
	}
	category, err := s.store.UpsertTenderCategory(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	c.JSON(http.StatusCreated, category)
}

// updateTenderCategoryHandler обновляет категорию
func (s *Server) updateTenderCategoryHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}
	var req tenderCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}
	params := db.UpdateTenderCategoryParams{
		ID:              id,
		Title:           sql.NullString{String: req.Title, Valid: true},
		TenderChapterID: sql.NullInt64{Int64: req.TenderChapterID, Valid: true},
	}
	category, err := s.store.UpdateTenderCategory(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	c.JSON(http.StatusOK, category)
}

// deleteTenderCategoryHandler удаляет категорию
func (s *Server) deleteTenderCategoryHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}
	err = s.store.DeleteTenderCategory(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	c.Status(http.StatusNoContent)
}

// listChaptersByTypeHandler - получает список разделов для конкретного типа тендера
func (s *Server) listChaptersByTypeHandler(c *gin.Context) {
	// Получаем ID типа из URL, например /api/v1/tender-types/1/chapters
	typeID, err := strconv.ParseInt(c.Param("type_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID типа тендера")))
		return
	}

	// Пагинация (можно оставить для унификации, но для выпадающего списка обычно не нужна)
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "100") // Берем с запасом

	pageID, _ := strconv.ParseInt(pageIDStr, 10, 32)
	pageSize, _ := strconv.ParseInt(pageSizeStr, 10, 32)
	if pageID < 1 { pageID = 1 }
	if pageSize < 1 { pageSize = 100 }

	params := db.ListTenderChaptersByTypeParams{
		TenderTypeID: typeID,
		Limit:        int32(pageSize),
		Offset:       (int32(pageID) - 1) * int32(pageSize),
	}

	chapters, err := s.store.ListTenderChaptersByType(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if chapters == nil {
		chapters = make([]db.TenderChapter, 0)
	}

	c.JSON(http.StatusOK, chapters)
}