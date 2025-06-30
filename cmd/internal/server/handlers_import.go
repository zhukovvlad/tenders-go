package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/util"
)

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
