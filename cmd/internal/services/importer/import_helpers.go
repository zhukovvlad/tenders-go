package importer

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/util"
)

// processCoreTenderData сохраняет основные данные тендера: объект, исполнитель, дата подготовки.
func (s *TenderImportService) processCoreTenderData(
	ctx context.Context,
	qtx db.Querier,
	payload *api_models.FullTenderData,
) (*db.Tender, error) {
	dbObject, err := s.Entities.GetOrCreateObject(ctx, qtx, payload.TenderObject, payload.TenderAddress)
	if err != nil {
		return nil, err
	}

	dbExecutor, err := s.Entities.GetOrCreateExecutor(ctx, qtx, payload.ExecutorData.ExecutorName, payload.ExecutorData.ExecutorPhone)
	if err != nil {
		return nil, err
	}

	preparedDate := util.ParseDate(payload.ExecutorData.ExecutorDate)

	tenderParams := db.UpsertTenderParams{
		EtpID:              payload.TenderID,
		Title:              payload.TenderTitle,
		ObjectID:           dbObject.ID,
		ExecutorID:         dbExecutor.ID,
		DataPreparedOnDate: preparedDate,
	}

	dbTender, err := qtx.UpsertTender(ctx, tenderParams)
	if err != nil {
		return nil, fmt.Errorf("не удалось сохранить тендер: %w", err)
	}

	s.logger.Infof("Успешно сохранен тендер: ID=%d, ETP_ID=%s", dbTender.ID, dbTender.EtpID)
	return &dbTender, nil
}

// processLot обрабатывает один лот и все его предложения.
// В случае успеха возвращает ID созданного/обновленного лота и nil.
// В случае ошибки возвращает 0 и саму ошибку.
func (s *TenderImportService) processLot(
	ctx context.Context,
	qtx db.Querier,
	tenderID int64,
	lotKey string,
	lotAPI api_models.Lot,
) (int64, bool, error) {
	s.logger.Infof("processLot: начало обработки лота %s (предложений: %d)", lotKey, len(lotAPI.ProposalData)+1)
	
	// UpsertLot уже возвращает нам полную запись о лоте, включая его ID
	dbLot, err := qtx.UpsertLot(ctx, db.UpsertLotParams{
		TenderID: tenderID,
		LotKey:   lotKey,
		LotTitle: lotAPI.LotTitle,
	})
	if err != nil {
		// Если лот не удалось сохранить, возвращаем нулевой ID и ошибку
		return 0, false, fmt.Errorf("не удалось сохранить лот: %w", err)
	}
	s.logger.Infof("processLot: лот %s сохранен, DB ID: %d", lotKey, dbLot.ID)

	hasNewPending := false

	// Обработка базового предложения
	s.logger.Infof("processLot: обработка базового предложения для лота %s", lotKey)
	baselineHasNew, err := s.processProposal(ctx, qtx, dbLot.ID, &lotAPI.BaseLineProposal, true, lotAPI.LotTitle)
	if err != nil {
		// Если дочерний элемент не удалось обработать, возвращаем нулевой ID и ошибку
		return 0, false, fmt.Errorf("обработка базового предложения: %w", err)
	}
	if baselineHasNew {
		hasNewPending = true
	}
	s.logger.Infof("processLot: базовое предложение обработано для лота %s", lotKey)

	// Обработка предложений подрядчиков
	s.logger.Infof("processLot: обработка %d предложений подрядчиков для лота %s", len(lotAPI.ProposalData), lotKey)
	proposalIdx := 0
	for _, proposalDetails := range lotAPI.ProposalData {
		proposalIdx++
		s.logger.Infof("processLot: обработка предложения %d/%d (подрядчик: %s) для лота %s", 
			proposalIdx, len(lotAPI.ProposalData), proposalDetails.Title, lotKey)
		
		proposalHasNew, err := s.processProposal(ctx, qtx, dbLot.ID, &proposalDetails, false, lotAPI.LotTitle)
		if err != nil {
			// Если дочерний элемент не удалось обработать, возвращаем нулевой ID и ошибку
			return 0, false, fmt.Errorf("обработка предложения от '%s': %w", proposalDetails.Title, err)
		}
		if proposalHasNew {
			hasNewPending = true
		}
	}
	s.logger.Infof("processLot: лот %s обработан полностью", lotKey)

	return dbLot.ID, hasNewPending, nil
}

// processProposal — унифицированный метод для обработки любого предложения
func (s *TenderImportService) processProposal(ctx context.Context, qtx db.Querier, lotID int64, proposalAPI *api_models.ContractorProposalDetails, isBaseline bool, lotTitle string) (bool, error) {
	var inn, title, address, accreditation string
	if isBaseline {
		// Для базового предложения используем константы или предопределенные значения
		inn, title = "0000000000", "Initiator"
		address, accreditation = "N/A", "N/A"
	} else {
		inn, title, address, accreditation = proposalAPI.Inn, proposalAPI.Title, proposalAPI.Address, proposalAPI.Accreditation
	}

	dbContractor, err := s.Entities.GetOrCreateContractor(ctx, qtx, inn, title, address, accreditation)
	if err != nil {
		return false, err
	}

	dbProposal, err := qtx.UpsertProposal(ctx, db.UpsertProposalParams{
		LotID:                lotID,
		ContractorID:         dbContractor.ID,
		IsBaseline:           isBaseline,
		ContractorCoordinate: util.NullableString(&proposalAPI.ContractorCoordinate),
		// ... другие поля ...
	})
	if err != nil {
		return false, fmt.Errorf("не удалось сохранить предложение: %w", err)
	}

	// Вызываем уже существующие у вас публичные методы, сделав их приватными
	if err := s.processProposalAdditionalInfo(ctx, qtx, dbProposal.ID, proposalAPI.AdditionalInfo, isBaseline); err != nil {
		return false, err
	}

	hasNewPending, err := s.processContractorItems(ctx, qtx, dbProposal.ID, proposalAPI.ContractorItems, lotTitle)
	if err != nil {
		return false, err
	}
	return hasNewPending, nil
}

// processContractorItems теперь только оркестрирует процесс
func (s *TenderImportService) processContractorItems(ctx context.Context, qtx db.Querier, proposalID int64, itemsAPI api_models.ContractorItemsContainer, lotTitle string) (bool, error) {
	logger := s.logger.WithField("proposal_id", proposalID)
	logger.Info("Обработка позиций и итогов")

	hasNewPending := false

	if itemsAPI.Positions != nil {
		for key, posAPI := range itemsAPI.Positions {
			// Вызываем хелпер для одной позиции
			posHasNew, err := s.processSinglePosition(ctx, qtx, proposalID, key, posAPI, lotTitle)
			if err != nil {
				// Ошибка уже залогирована внутри хелпера
				return false, fmt.Errorf("обработка позиции '%s': %w", key, err)
			}
			if posHasNew {
				hasNewPending = true
			}
		}
	}
	logger.Info("Позиции успешно обработаны")

	if itemsAPI.Summary != nil {
		for key, sumLineAPI := range itemsAPI.Summary {
			// Вызываем хелпер для одной строки итога
			if err := s.processSingleSummaryLine(ctx, qtx, proposalID, key, sumLineAPI); err != nil {
				return false, fmt.Errorf("обработка строки итога '%s': %w", key, err)
			}
		}
	}
	logger.Info("Итоги успешно обработаны")
	return hasNewPending, nil
}

// processSinglePosition обрабатывает одну позицию
func (s *TenderImportService) processSinglePosition(
	ctx context.Context,
	qtx db.Querier,
	proposalID int64,
	positionKey string,
	posAPI api_models.PositionItem,
	lotTitle string,
) (bool, error) {
	// 1. Получаем зависимости
	unitID, err := s.Entities.GetOrCreateUnitOfMeasurement(ctx, qtx, posAPI.Unit)
	if err != nil {
		return false, fmt.Errorf("не удалось получить/создать единицу измерения: %w", err)
	}

	catPos, isNewPendingItem, err := s.Entities.GetOrCreateCatalogPosition(ctx, qtx, posAPI, lotTitle, unitID)
	if err != nil {
		return false, fmt.Errorf("не удалось получить/создать позицию каталога: %w", err)
	}

	if catPos.ID == 0 {
		s.logger.Warnf("Позиция каталога не была создана (возможно, пустой заголовок), пропуск: %s", posAPI.JobTitle)
		return false, nil
	}


	var finalCatalogPositionID sql.NullInt64

	if catPos.Kind != "POSITION" {
		// Заголовки (HEADER, LOT_HEADER) сразу привязываем
		finalCatalogPositionID = sql.NullInt64{Int64: catPos.ID, Valid: true}
	} else {
		// Для POSITION проверяем кэш
		hashKey := util.GetSHA256Hash(catPos.StandardJobTitle)
		const currentNormVersion = 1

		cachedMatch, err := qtx.GetMatchingCache(ctx, db.GetMatchingCacheParams{
			JobTitleHash: hashKey,
			NormVersion:  currentNormVersion,
		})

		switch err {
		case nil:
			// === CACHE HIT ===
			// Отлично, Python-воркер уже сделал работу.
			finalCatalogPositionID = sql.NullInt64{Int64: cachedMatch.CatalogPositionID, Valid: true}

		case sql.ErrNoRows:
			// === CACHE MISS ===
			// НОВАЯ СТРАТЕГИЯ: Сохраняем ID новой позиции (draft_catalog_id)
			// Это позволяет Python использовать его как Fallback, если RAG не найдет лучшего варианта
			finalCatalogPositionID = sql.NullInt64{Int64: catPos.ID, Valid: true}

		default:
			// Другая, неожиданная ошибка БД
			return false, fmt.Errorf("ошибка чтения matching_cache: %w", err)
		}
	}

	// 2. Маппинг данных
	params := mapApiPositionToDbParams(proposalID, positionKey, finalCatalogPositionID, unitID, posAPI)

	// 3. Выполнение запроса
	if _, err := qtx.UpsertPositionItem(ctx, params); err != nil {
		s.logger.WithField("position_key", positionKey).Errorf("Не удалось сохранить позицию: %v", err)
		return false, fmt.Errorf("не удалось сохранить позицию: %w", err)
	}
	return isNewPendingItem, nil
}

// processSingleSummaryLine обрабатывает одну строку итога.
// Он вызывает маппер для преобразования данных и выполняет запрос к БД.
func (s *TenderImportService) processSingleSummaryLine(
	ctx context.Context,
	qtx db.Querier,
	proposalID int64,
	summaryKey string,
	sumLineAPI api_models.SummaryLine,
) error {
	// Шаг 1: Преобразование API модели в параметры для БД с помощью "чистой" функции-маппера.
	params := mapApiSummaryToDbParams(proposalID, summaryKey, sumLineAPI)

	// Шаг 2: Выполнение запроса к БД.
	if _, err := qtx.UpsertProposalSummaryLine(ctx, params); err != nil {
		s.logger.WithField("summary_key", summaryKey).Errorf("Не удалось сохранить строку итога: %v", err)
		// Возвращаем оригинальную ошибку, чтобы транзакция откатилась.
		return err
	}

	return nil
}

func (s *TenderImportService) processProposalAdditionalInfo(
	ctx context.Context,
	qtx db.Querier,
	proposalID int64,
	additionalInfoAPI map[string]*string,
	isBaseline bool, // ← добавь новый аргумент сюда
) error {
	if isBaseline {
		s.logger.WithField("proposal_id", proposalID).Info("Baseline-предложение, пропускаем доп. информацию")
		return nil
	}

	logger := s.logger.WithField("proposal_id", proposalID).WithField("section", "additional_info")
	logger.Info("Обработка дополнительной информации")

	if additionalInfoAPI == nil {
		logger.Warn("Дополнительная информация (additionalInfoAPI) не предоставлена, пропуск обработки")
		return nil
	}

	if err := qtx.DeleteAllAdditionalInfoForProposal(ctx, proposalID); err != nil {
		logger.Errorf("Ошибка удаления старой дополнительной информации для предложения ID %d: %v", proposalID, err)
		return fmt.Errorf("ошибка удаления старой дополнительной информации для предложения ID %d: %w", proposalID, err)
	}
	for key, valuePtr := range additionalInfoAPI {
		_, err := qtx.UpsertProposalAdditionalInfo(ctx, db.UpsertProposalAdditionalInfoParams{
			ProposalID: proposalID,
			InfoKey:    key,
			InfoValue:  util.NullableString(valuePtr),
		})
		if err != nil {
			logger.Errorf("Не удалось сохранить доп. инфо (ключ: %s): %v", key, err)
			return fmt.Errorf("не удалось сохранить доп. инфо (ключ: %s): %w", key, err)
		}
	}
	logger.Info("Дополнительная информация успешно обработана")
	return nil
}

func mapApiPositionToDbParams(
	proposalID int64,
	positionKey string,
	catalogPositionID sql.NullInt64,
	unitID sql.NullInt64,
	posAPI api_models.PositionItem,
) db.UpsertPositionItemParams {
	return db.UpsertPositionItemParams{
		ProposalID:                    proposalID,
		CatalogPositionID:             catalogPositionID,
		PositionKeyInProposal:         positionKey,
		CommentOrganazier:             util.NullableString(posAPI.CommentOrganizer),
		CommentContractor:             util.NullableString(posAPI.CommentContractor),
		ItemNumberInProposal:          util.NullableString(&posAPI.Number), // Number - string, not *string в api_models
		ChapterNumberInProposal:       util.NullableString(posAPI.ChapterNumber),
		JobTitleInProposal:            posAPI.JobTitle,
		UnitID:                        unitID, // sql.NullInt64
		Quantity:                      util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.Quantity)),
		SuggestedQuantity:             util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.SuggestedQuantity)),
		TotalCostForOrganizerQuantity: util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCostForOrganizerQuantity)),
		UnitCostMaterials:             util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.Materials)),
		UnitCostWorks:                 util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.Works)),
		UnitCostIndirectCosts:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.IndirectCosts)),
		UnitCostTotal:                 util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.Total)),
		TotalCostMaterials:            util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.Materials)),
		TotalCostWorks:                util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.Works)),
		TotalCostIndirectCosts:        util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.IndirectCosts)),
		TotalCostTotal:                util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.Total)), // Убедитесь, что это поле nullable в таблице
		DeviationFromBaselineCost:     util.ConvertNullFloat64ToNullString(util.NullableFloat64(nil)),                    // Заполните из posAPI, если есть
		IsChapter:                     posAPI.IsChapter,
		ChapterRefInProposal:          util.NullableString(posAPI.ChapterRef),
	}
}

// mapApiSummaryToDbParams преобразует API-модель строки итога в параметры для sqlc.
// Это чистая функция без побочных эффектов.
func mapApiSummaryToDbParams(
	proposalID int64,
	summaryKey string,
	sumLineAPI api_models.SummaryLine,
) db.UpsertProposalSummaryLineParams {
	return db.UpsertProposalSummaryLineParams{
		// Основные идентификаторы
		ProposalID: proposalID,
		SummaryKey: summaryKey,

		// Основные данные
		JobTitle: sumLineAPI.JobTitle,

		// Данные из TotalCost (для summary обычно используется TotalCost, а не UnitCost)
		MaterialsCost:     util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Materials)),
		WorksCost:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Works)),
		IndirectCostsCost: util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.IndirectCosts)),
		TotalCost:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Total)),
	}
}
