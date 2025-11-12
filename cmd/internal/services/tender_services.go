package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sqlc-dev/pqtype"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/util"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

const (
	// MaxUnmatchedPositionsLimit определяет максимальное количество позиций,
	// которое можно запросить за один вызов GetUnmatchedPositions.
	// Это ограничение предотвращает чрезмерную нагрузку на БД и память.
	MaxUnmatchedPositionsLimit = 1000
)

// ValidationError представляет ошибку валидации входных данных.
// Используется для разделения ошибок валидации (HTTP 400) от серверных ошибок (HTTP 500).
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NewValidationError создает новую ошибку валидации.
func NewValidationError(format string, args ...interface{}) error {
	return &ValidationError{
		Message: fmt.Sprintf(format, args...),
	}
}

// TenderProcessingService отвечает за полную обработку тендерных данных,
// включая импорт тендера, объектов, лотов, предложений, позиций и итоговых строк.
type TenderProcessingService struct {
	store  db.Store        // SQLC-совместимый store, обеспечивающий транзакции
	logger *logging.Logger // Обёртка над logrus с поддержкой полей
}

// NewTenderProcessingService создает новый экземпляр TenderProcessingService.
func NewTenderProcessingService(store db.Store, logger *logging.Logger) *TenderProcessingService {
	return &TenderProcessingService{
		store:  store,
		logger: logger,
	}
}

// ImportFullTender выполняет полный импорт тендера из API-модели и сохраняет "сырой" JSON.
// Все операции выполняются в одной транзакции.
//
// Поведение:
//  1. Импортирует основную информацию о тендере и связанные сущности (лоты и т.д.).
//  2. После успешного импорта делает UPSERT исходного JSON в таблицу tender_raw_data.
//     Перезапись допускается и желательна: при повторной загрузке данные полностью обновляются.
//  3. При любой ошибке в транзакции изменения откатываются.
//
// Аргументы:
//   - ctx: контекст запроса (таймаут/отмена)
//   - payload: распарсенная структура тендера (валидация должна быть выполнена до вызова)
//   - rawJSON: исходное тело запроса в виде байт (тот же JSON, что пришёл от парсера)
//
// Возвращает:
//   - ID тендера в БД,
//   - map[lotKey]lotDBID для всех созданных/обновлённых лотов,
//   - ошибку (nil при успехе).
func (s *TenderProcessingService) ImportFullTender(
	ctx context.Context,
	payload *api_models.FullTenderData,
	rawJSON []byte,
) (int64, map[string]int64, error) {

	var newTenderDBID int64
	lotIDs := make(map[string]int64)

	txErr := s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		// Шаг 1: Обработка основной информации о тендере
		dbTender, err := s.processCoreTenderData(ctx, qtx, payload)
		if err != nil {
			return err
		}
		newTenderDBID = dbTender.ID

		// Шаг 2: Обработка лотов
		for lotKey, lotAPI := range payload.LotsData {
			lotDBID, err := s.processLot(ctx, qtx, dbTender.ID, lotKey, lotAPI)
			if err != nil {
				return fmt.Errorf("ошибка при обработке лота '%s': %w", lotKey, err)
			}
			lotIDs[lotKey] = lotDBID
		}

		// Шаг 3: UPSERT "сырого" JSON в tender_raw_data в рамках той же транзакции.
		// sqlc сгенерировал тип параметра как json.RawMessage — передаём rawJSON как есть.
		s.logger.Infof("Сохраняем исходный JSON для тендера ID: %d", newTenderDBID)
		if _, err := qtx.UpsertTenderRawData(ctx, db.UpsertTenderRawDataParams{
			TenderID: newTenderDBID,
			RawData:  json.RawMessage(rawJSON),
		}); err != nil {
			s.logger.Errorf("Ошибка при сохранении tender_raw_data для тендера ID %d: %v", newTenderDBID, err)
			return fmt.Errorf("не удалось сохранить исходный JSON (tender_raw_data): %w", err)
		}
		s.logger.Infof("Исходный JSON успешно сохранен для тендера ID: %d", newTenderDBID)

		return nil // транзакция завершится успешно
	})

	if txErr != nil {
		s.logger.Errorf("Не удалось импортировать тендер ETP_ID %s: %v", payload.TenderID, txErr)
		return 0, nil, fmt.Errorf("транзакция импорта тендера провалена: %w", txErr)
	}

	s.logger.Infof("Тендер ETP_ID %s успешно импортирован с ID базы данных: %d", payload.TenderID, newTenderDBID)
	return newTenderDBID, lotIDs, nil
}

// processCoreTenderData сохраняет основные данные тендера: объект, исполнитель, дата подготовки.
func (s *TenderProcessingService) processCoreTenderData(
	ctx context.Context,
	qtx db.Querier,
	payload *api_models.FullTenderData,
) (*db.Tender, error) {
	dbObject, err := s.GetOrCreateObject(ctx, qtx, payload.TenderObject, payload.TenderAddress)
	if err != nil {
		return nil, err
	}

	dbExecutor, err := s.GetOrCreateExecutor(ctx, qtx, payload.ExecutorData.ExecutorName, payload.ExecutorData.ExecutorPhone)
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

// processSinglePosition обрабатывает одну позицию
func (s *TenderProcessingService) processSinglePosition(
	ctx context.Context,
	qtx db.Querier,
	proposalID int64,
	positionKey string,
	posAPI api_models.PositionItem,
	lotTitle string,
) error {
	// 1. Получаем зависимости
	catPos, err := s.GetOrCreateCatalogPosition(ctx, qtx, posAPI, lotTitle)
	if err != nil {
		return fmt.Errorf("не удалось получить/создать позицию каталога: %w", err)
	}

	if catPos.ID == 0 {
		s.logger.Warnf("Позиция каталога не была создана (возможно, пустой заголовок), пропуск: %s", posAPI.JobTitle)
		return nil
	}

	unitID, err := s.GetOrCreateUnitOfMeasurement(ctx, qtx, posAPI.Unit)
	if err != nil {
		return fmt.Errorf("не удалось получить/создать единицу измерения: %w", err)
	}

	var finalCatalogPositionID sql.NullInt64

	if catPos.Kind != "POSITION" {
		finalCatalogPositionID = sql.NullInt64{Int64: catPos.ID, Valid: true}
	} else {
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
			// Python-воркер еще не работал.
			// МЫ СТАВИМ NULL. ЭТО КЛЮЧЕВОЕ ИЗМЕНЕНИЕ.
			finalCatalogPositionID = sql.NullInt64{Valid: false}

		default:
			// Другая, неожиданная ошибка БД
			return fmt.Errorf("ошибка чтения matching_cache: %w", err)
		}
	}

	// 2. Маппинг данных
	params := mapApiPositionToDbParams(proposalID, positionKey, finalCatalogPositionID, unitID, posAPI)

	// 3. Выполнение запроса
	if _, err := qtx.UpsertPositionItem(ctx, params); err != nil {
		s.logger.WithField("position_key", positionKey).Errorf("Не удалось сохранить позицию: %v", err)
		return err // Возвращаем оригинальную ошибку от БД
	}
	return nil
}

// processSingleSummaryLine обрабатывает одну строку итога.
// Он вызывает маппер для преобразования данных и выполняет запрос к БД.
func (s *TenderProcessingService) processSingleSummaryLine(
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

func (s *TenderProcessingService) GetOrCreateObject(
	ctx context.Context,
	qtx db.Querier,
	title string,
	address string,
) (db.Object, error) {
	opLogger := s.logger.WithFields(logrus.Fields{
		"entity":  "object",
		"title":   title,
		"address": address,
	})

	return getOrCreateOrUpdate(
		ctx,
		qtx,
		func() (db.Object, error) {
			opLogger.Info("Пытаемся найти объект по названию")
			return qtx.GetObjectByTitle(ctx, title)
		},
		func() (db.Object, error) {
			opLogger.Info("Объект не найден, создаем новый.")
			return qtx.CreateObject(ctx, db.CreateObjectParams{
				Title:   title,
				Address: address,
			})
		},
		func(existing db.Object) (bool, db.UpdateObjectParams, error) {
			if existing.Address != address {
				opLogger.Infof("Адрес объекта отличается ('%s' -> '%s').", existing.Address, address)
				return true, db.UpdateObjectParams{
					ID:      existing.ID,
					Title:   sql.NullString{String: existing.Title, Valid: true}, // title не меняем
					Address: sql.NullString{String: address, Valid: true},
				}, nil
			}
			return false, db.UpdateObjectParams{}, nil
		},
		func(params db.UpdateObjectParams) (db.Object, error) {
			opLogger.Info("Обновляем существующий объект.")
			return qtx.UpdateObject(ctx, params)
		},
	)
}

// getOrCreateExecutor находит исполнителя по name. Если не найден, создает нового.
// Если найден, но телефон отличается, обновляет телефон.
func (s *TenderProcessingService) GetOrCreateExecutor(
	ctx context.Context,
	qtx db.Querier,
	name string,
	phone string,
) (db.Executor, error) {
	opLogger := s.logger.WithFields(logrus.Fields{
		"entity": "executor",
		"name":   name,
		"phone":  phone,
	})

	return getOrCreateOrUpdate(
		ctx,
		qtx,
		func() (db.Executor, error) {
			opLogger.Info("Пытаемся найти исполнителя по имени")
			return qtx.GetExecutorByName(ctx, name)
		},
		func() (db.Executor, error) {
			opLogger.Info("Исполнитель не найден, создаем нового.")
			return qtx.CreateExecutor(ctx, db.CreateExecutorParams{
				Name:  name,
				Phone: phone,
			})
		},
		func(existing db.Executor) (bool, db.UpdateExecutorParams, error) {
			opLogger.Info("Проверяем необходимость обновления исполнителя")
			if existing.Phone != phone {
				opLogger.Infof("Телефон исполнителя отличается ('%s' -> '%s').", existing.Phone, phone)
				return true, db.UpdateExecutorParams{
					ID:    existing.ID,
					Name:  sql.NullString{String: existing.Name, Valid: true}, // name не меняем
					Phone: sql.NullString{String: phone, Valid: true},
				}, nil
			}
			return false, db.UpdateExecutorParams{}, nil
		},
		func(params db.UpdateExecutorParams) (db.Executor, error) {
			return qtx.UpdateExecutor(ctx, params)
		},
	)
}

func (s *TenderProcessingService) GetOrCreateContractor(
	ctx context.Context,
	qtx db.Querier,
	inn string,
	title string,
	address string,
	accreditation string,
) (db.Contractor, error) {
	opLogger := s.logger.WithField(
		"entity",
		"contractor",
	).WithField("inn", inn)

	return getOrCreateOrUpdate(
		ctx,
		qtx,
		func() (db.Contractor, error) {
			opLogger.Info("Пытаемся найти подрядчика по ИНН")
			return qtx.GetContractorByINN(ctx, inn)
		},
		func() (db.Contractor, error) {
			opLogger.Info("Подрядчик не найден, создаем нового.")
			return qtx.CreateContractor(ctx, db.CreateContractorParams{
				Inn:           inn,
				Title:         title,
				Address:       address,
				Accreditation: accreditation,
			})
		},
		func(existing db.Contractor) (bool, db.UpdateContractorParams, error) {
			opLogger.Info("Подрядчик найден, проверяем необходимость обновления.")
			needsUpdate := false
			updateParams := db.UpdateContractorParams{
				ID: existing.ID,
			}

			if existing.Title != title {
				opLogger.Infof("Название подрядчика отличается: '%s' -> '%s'", existing.Title, title)
				updateParams.Title = sql.NullString{String: title, Valid: true}
				needsUpdate = true
			}
			if existing.Address != address {
				opLogger.Infof("Адрес подрядчика отличается: '%s' -> '%s'", existing.Address, address)
				updateParams.Address = sql.NullString{String: address, Valid: true}
				needsUpdate = true
			}
			if existing.Accreditation != accreditation {
				opLogger.Infof("Аккредитация подрядчика отличается: '%s' -> '%s'", existing.Accreditation, accreditation)
				updateParams.Accreditation = sql.NullString{String: accreditation, Valid: true}
				needsUpdate = true
			}
			return needsUpdate, updateParams, nil
		},
		func(params db.UpdateContractorParams) (db.Contractor, error) {
			opLogger.Info("Обновляем данные подрядчика.")
			return qtx.UpdateContractor(ctx, params)
		},
	)
}

func (s *TenderProcessingService) ProcessProposalAdditionalInfo(
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
			InfoValue:  sql.NullString{String: util.Deref(valuePtr), Valid: true},
		})
		if err != nil {
			logger.Errorf("Не удалось сохранить доп. инфо (ключ: %s): %v", key, err)
			return fmt.Errorf("не удалось сохранить доп. инфо (ключ: %s): %w", key, err)
		}
	}
	logger.Info("Дополнительная информация успешно обработана")
	return nil
}

// ProcessContractorItems теперь только оркестрирует процесс
func (s *TenderProcessingService) ProcessContractorItems(ctx context.Context, qtx db.Querier, proposalID int64, itemsAPI api_models.ContractorItemsContainer, lotTitle string) error {
	logger := s.logger.WithField("proposal_id", proposalID)
	logger.Info("Обработка позиций и итогов")

	if itemsAPI.Positions != nil {
		for key, posAPI := range itemsAPI.Positions {
			// Вызываем хелпер для одной позиции
			if err := s.processSinglePosition(ctx, qtx, proposalID, key, posAPI, lotTitle); err != nil {
				// Ошибка уже залогирована внутри хелпера
				return fmt.Errorf("обработка позиции '%s': %w", key, err)
			}
		}
	}
	logger.Info("Позиции успешно обработаны")

	if itemsAPI.Summary != nil {
		for key, sumLineAPI := range itemsAPI.Summary {
			// Вызываем хелпер для одной строки итога
			if err := s.processSingleSummaryLine(ctx, qtx, proposalID, key, sumLineAPI); err != nil {
				return fmt.Errorf("обработка строки итога '%s': %w", key, err)
			}
		}
	}
	logger.Info("Итоги успешно обработаны")
	return nil
}

func (s *TenderProcessingService) GetOrCreateCatalogPosition(
	ctx context.Context,
	qtx db.Querier,
	posAPI api_models.PositionItem,
	lotTitle string,
) (db.CatalogPosition, error) {

	// Шаг 1: Получаем и kind, и standardJobTitle
	kind, standardJobTitleForDB, err := s.getKindAndStandardTitle(posAPI, lotTitle)
	if err != nil {
		// Эта ошибка теперь не должна возникать, т.к. хелпер обрабатывает пустые строки
		return db.CatalogPosition{}, err
	}

	// Если имя пустое (например, заголовок с пустым job_title),
	// мы не должны создавать запись в catalog_positions.
	if standardJobTitleForDB == "" {
		// Возвращаем пустую структуру, `processSinglePosition` пропустит эту позицию
		return db.CatalogPosition{}, nil
	}

	opLogger := s.logger.WithFields(logrus.Fields{
		"service_method":          "GetOrCreateCatalogPosition",
		"input_raw_job_title":     posAPI.JobTitle,
		"used_standard_job_title": standardJobTitleForDB,
		"determined_kind":         kind,
	})

	// Используем getOrCreateOrUpdate.
	// P теперь - это существующий тип db.UpdateCatalogPositionDetailsParams
	return getOrCreateOrUpdate(
		ctx, qtx,
		// getFn
		func() (db.CatalogPosition, error) {
			return qtx.GetCatalogPositionByStandardJobTitle(ctx, standardJobTitleForDB)
		},
		// createFn
		func() (db.CatalogPosition, error) {
			opLogger.Info("Позиция каталога не найдена, создается новая.")

			// Как мы и обсуждали, устанавливаем статус в зависимости от типа
			var newStatus string
			if kind == "POSITION" {
				newStatus = "pending_indexing" // Ставим в очередь на RAG
			} else {
				newStatus = "na" // (Header, Trash и т.д. - не индексируем)
			}

			//
			return qtx.CreateCatalogPosition(ctx, db.CreateCatalogPositionParams{
				StandardJobTitle: standardJobTitleForDB,
				Description:      sql.NullString{String: posAPI.JobTitle, Valid: true},
				Kind:             kind,
				Status:           newStatus, // 👈 (ИСПРАВЛЕНИЕ)
			})
		},
		// diffFn: Проверяем, не изменился ли `kind`
		func(existing db.CatalogPosition) (bool, db.UpdateCatalogPositionDetailsParams, error) {
			// Если парсер вдруг передумал (например, `TO_REVIEW` -> `POSITION`), обновляем.
			if existing.Kind != kind {
				opLogger.Warnf("Kind для '%s' изменился: '%s' -> '%s'. Обновляем.", standardJobTitleForDB, existing.Kind, kind)
				return true, db.UpdateCatalogPositionDetailsParams{
					ID:   existing.ID,
					Kind: sql.NullString{String: kind, Valid: true},
					// Обновляем и описание на всякий случай
					Description: sql.NullString{String: posAPI.JobTitle, Valid: true},
				}, nil
			}
			return false, db.UpdateCatalogPositionDetailsParams{}, nil
		},
		// updateFn: будет вызвана хелпером, если diffFn вернет true
		func(params db.UpdateCatalogPositionDetailsParams) (db.CatalogPosition, error) {
			opLogger.Info("Обновляем Kind для существующей позиции.") //
			return qtx.UpdateCatalogPositionDetails(ctx, params)      //
		},
	)
}

// GetOrCreateUnitOfMeasurement находит или создает единицу измерения.
// apiUnitName - это указатель на строку с названием единицы измерения из JSON (поле "unit" из PositionItem).
// Возвращает sql.NullInt64, так как unit_id в position_items может быть NULL.
func (s *TenderProcessingService) GetOrCreateUnitOfMeasurement(
	ctx context.Context,
	qtx db.Querier, // Querier для выполнения запросов в транзакции
	apiUnitName *string,
) (sql.NullInt64, error) {

	// Шаг 1: Безопасно получаем и очищаем входное значение
	var originalUnitNameValue string
	if apiUnitName != nil {
		originalUnitNameValue = *apiUnitName
	}

	trimmedUnitName := strings.TrimSpace(originalUnitNameValue)

	// Если после очистки имя единицы измерения пустое, считаем, что оно не предоставлено.
	if trimmedUnitName == "" {
		// Можно не логировать это как ошибку, если это нормальная ситуация (например, для заголовков глав)
		// s.logger.Debug("Имя единицы измерения не предоставлено или пусто после очистки.")
		return sql.NullInt64{Valid: false}, nil
	}

	// Шаг 2: Нормализуем имя для использования в качестве ключа в БД
	// (например, приводим к нижнему регистру)
	normalizedNameForDB := strings.ToLower(trimmedUnitName)

	opLogger := s.logger.WithFields(logrus.Fields{
		"service_method":      "GetOrCreateUnitOfMeasurement",
		"input_api_unit_name": originalUnitNameValue, // Логируем исходное значение для отладки
		"normalized_name_key": normalizedNameForDB,
	})

	// Шаг 3: Пытаемся найти существующую единицу измерения
	unit, err := qtx.GetUnitOfMeasurementByNormalizedName(ctx, normalizedNameForDB)
	if err != nil {
		if err == sql.ErrNoRows {
			// Единица измерения не найдена, создаем новую
			opLogger.Info("Единица измерения не найдена, создается новая.")

			// Для поля full_name в таблице units_of_measurement можно использовать
			// trimmedUnitName (оригинальное, но очищенное от крайних пробелов) или normalizedNameForDB.
			// trimmedUnitName обычно предпочтительнее для отображения.
			fullNameParam := sql.NullString{String: trimmedUnitName, Valid: true}

			// Поле description пока оставляем пустым (sql.NullString{Valid: false})
			descriptionParam := sql.NullString{Valid: false}

			createdUnit, createErr := qtx.CreateUnitOfMeasurement(ctx, db.CreateUnitOfMeasurementParams{
				NormalizedName: normalizedNameForDB,
				FullName:       fullNameParam,
				Description:    descriptionParam,
			})
			if createErr != nil {
				opLogger.Errorf("Ошибка создания единицы измерения: %v", createErr)
				return sql.NullInt64{}, fmt.Errorf("ошибка создания единицы измерения '%s': %w", normalizedNameForDB, createErr)
			}
			opLogger.Infof("Единица измерения успешно создана, ID: %d", createdUnit.ID)
			return sql.NullInt64{Int64: createdUnit.ID, Valid: true}, nil
		}
		// Другая ошибка при попытке получить единицу измерения
		opLogger.Errorf("Ошибка получения единицы измерения по normalized_name: %v", err)
		return sql.NullInt64{}, fmt.Errorf("ошибка получения единицы измерения по normalized_name '%s': %w", normalizedNameForDB, err)
	}

	// Единица измерения найдена
	opLogger.Infof("Найдена существующая единица измерения, ID: %d", unit.ID)
	// На данном этапе мы не обновляем существующую запись (например, full_name или description).
	// Если это необходимо, можно добавить логику сравнения и вызова qtx.UpdateUnitOfMeasurement.
	// Но для "GetOrCreate" обычно достаточно вернуть найденное или только что созданное.
	return sql.NullInt64{Int64: unit.ID, Valid: true}, nil
}

// processLot обрабатывает один лот и все его предложения.
// В случае успеха возвращает ID созданного/обновленного лота и nil.
// В случае ошибки возвращает 0 и саму ошибку.
func (s *TenderProcessingService) processLot(
	ctx context.Context,
	qtx db.Querier,
	tenderID int64,
	lotKey string,
	lotAPI api_models.Lot,
) (int64, error) { // <-- ИЗМЕНЕНИЕ 1: Сигнатура функции теперь возвращает ID (int64)

	// UpsertLot уже возвращает нам полную запись о лоте, включая его ID
	dbLot, err := qtx.UpsertLot(ctx, db.UpsertLotParams{
		TenderID: tenderID,
		LotKey:   lotKey,
		LotTitle: lotAPI.LotTitle,
	})
	if err != nil {
		// Если лот не удалось сохранить, возвращаем нулевой ID и ошибку
		return 0, fmt.Errorf("не удалось сохранить лот: %w", err)
	}

	// Обработка базового предложения
	if err := s.processProposal(ctx, qtx, dbLot.ID, &lotAPI.BaseLineProposal, true, lotAPI.LotTitle); err != nil {
		// Если дочерний элемент не удалось обработать, возвращаем нулевой ID и ошибку
		return 0, fmt.Errorf("обработка базового предложения: %w", err)
	}

	// Обработка предложений подрядчиков
	for _, proposalDetails := range lotAPI.ProposalData {
		if err := s.processProposal(ctx, qtx, dbLot.ID, &proposalDetails, false, lotAPI.LotTitle); err != nil {
			// Если дочерний элемент не удалось обработать, возвращаем нулевой ID и ошибку
			return 0, fmt.Errorf("обработка предложения от '%s': %w", proposalDetails.Title, err)
		}
	}

	// <-- ИЗМЕНЕНИЕ 2: Если все прошло успешно, возвращаем ID лота и nil
	return dbLot.ID, nil
}

// processProposal — унифицированный метод для обработки любого предложения
func (s *TenderProcessingService) processProposal(ctx context.Context, qtx db.Querier, lotID int64, proposalAPI *api_models.ContractorProposalDetails, isBaseline bool, lotTitle string) error {
	var inn, title, address, accreditation string
	if isBaseline {
		// Для базового предложения используем константы или предопределенные значения
		inn, title = "0000000000", "Initiator"
		address, accreditation = "N/A", "N/A"
	} else {
		inn, title, address, accreditation = proposalAPI.Inn, proposalAPI.Title, proposalAPI.Address, proposalAPI.Accreditation
	}

	dbContractor, err := s.GetOrCreateContractor(ctx, qtx, inn, title, address, accreditation)
	if err != nil {
		return err
	}

	dbProposal, err := qtx.UpsertProposal(ctx, db.UpsertProposalParams{
		LotID:                lotID,
		ContractorID:         dbContractor.ID,
		IsBaseline:           isBaseline,
		ContractorCoordinate: util.NullableString(&proposalAPI.ContractorCoordinate),
		// ... другие поля ...
	})
	if err != nil {
		return fmt.Errorf("не удалось сохранить предложение: %w", err)
	}

	// Вызываем уже существующие у вас публичные методы, сделав их приватными
	if err := s.ProcessProposalAdditionalInfo(ctx, qtx, dbProposal.ID, proposalAPI.AdditionalInfo, isBaseline); err != nil {
		return err
	}

	if err := s.ProcessContractorItems(ctx, qtx, dbProposal.ID, proposalAPI.ContractorItems, lotTitle); err != nil {
		return err
	}
	return nil
}

// UpdateLotKeyParameters обновляет ключевые параметры лота, найденного по tender_id и lot_key
func (s *TenderProcessingService) UpdateLotKeyParameters(
	ctx context.Context,
	tenderEtpID string,
	lotKey string,
	keyParameters map[string]interface{},
) error {
	logger := s.logger.WithField("method", "UpdateLotKeyParameters")
	logger.Infof("Начинаем обновление ключевых параметров для тендера %s, лот %s", tenderEtpID, lotKey)

	// Сериализуем keyParameters в JSON
	keyParamsJSON, err := json.Marshal(keyParameters)
	if err != nil {
		logger.Errorf("Ошибка сериализации ключевых параметров: %v", err)
		return fmt.Errorf("не удалось сериализовать ключевые параметры: %w", err)
	}

	return s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		// Сначала найдем тендер по ETP ID
		tender, err := qtx.GetTenderByEtpID(ctx, tenderEtpID)
		if err != nil {
			if err == sql.ErrNoRows {
				logger.Warnf("Тендер с ETP ID %s не найден", tenderEtpID)
				return fmt.Errorf("тендер с ID %s не найден", tenderEtpID)
			}
			logger.Errorf("Ошибка при поиске тендера %s: %v", tenderEtpID, err)
			return fmt.Errorf("ошибка при поиске тендера: %w", err)
		}

		// Теперь найдем лот по tender_id и lot_key
		lot, err := qtx.GetLotByTenderAndKey(ctx, db.GetLotByTenderAndKeyParams{
			TenderID: tender.ID,
			LotKey:   lotKey,
		})
		if err != nil {
			if err == sql.ErrNoRows {
				logger.Warnf("Лот с ключом %s не найден в тендере %s", lotKey, tenderEtpID)
				return fmt.Errorf("лот с ключом %s не найден в тендере %s", lotKey, tenderEtpID)
			}
			logger.Errorf("Ошибка при поиске лота %s в тендере %s: %v", lotKey, tenderEtpID, err)
			return fmt.Errorf("ошибка при поиске лота: %w", err)
		}

		// Обновляем ключевые параметры лота
		updatedLot, err := qtx.UpdateLotDetails(ctx, db.UpdateLotDetailsParams{
			ID: lot.ID,
			LotKeyParameters: pqtype.NullRawMessage{
				RawMessage: keyParamsJSON,
				Valid:      true,
			},
		})
		if err != nil {
			logger.Errorf("Ошибка при обновлении ключевых параметров лота ID %d: %v", lot.ID, err)
			return fmt.Errorf("не удалось обновить ключевые параметры лота: %w", err)
		}

		logger.Infof("Ключевые параметры успешно обновлены для лота ID %d (тендер %s, лот %s)",
			updatedLot.ID, tenderEtpID, lotKey)
		return nil
	})
}

// UpdateLotKeyParametersDirectly обновляет ключевые параметры лота напрямую по lot_id (DB ID)
// без проверки tender_id - используется когда у нас есть только внутренние ID из БД
func (s *TenderProcessingService) UpdateLotKeyParametersDirectly(
	ctx context.Context,
	lotIDStr string,
	keyParameters map[string]interface{},
) error {
	logger := s.logger.WithFields(logrus.Fields{
		"method": "UpdateLotKeyParametersDirectly",
		"lot_id": lotIDStr,
	})
	logger.Info("Начинаем обновление ключевых параметров лота по DB ID")

	// Преобразуем lot_id из строки в int64
	lotID, err := strconv.ParseInt(lotIDStr, 10, 64)
	if err != nil {
		logger.Errorf("Неверный формат lot_id: %s", lotIDStr)
		return fmt.Errorf("неверный формат lot_id: %s", lotIDStr)
	}

	// Сериализуем keyParameters в JSON
	keyParamsJSON, err := json.Marshal(keyParameters)
	if err != nil {
		logger.Errorf("Ошибка сериализации ключевых параметров: %v", err)
		return fmt.Errorf("не удалось сериализовать ключевые параметры: %w", err)
	}

	return s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		// Просто найдем лот по ID для проверки существования
		lot, err := qtx.GetLotByID(ctx, lotID)
		if err != nil {
			if err == sql.ErrNoRows {
				logger.Warnf("Лот с ID %d не найден", lotID)
				return fmt.Errorf("лот с ID %s не найден", lotIDStr)
			}
			logger.Errorf("Ошибка при поиске лота %d: %v", lotID, err)
			return fmt.Errorf("ошибка при поиске лота: %w", err)
		}

		// Обновляем ключевые параметры лота
		updatedLot, err := qtx.UpdateLotDetails(ctx, db.UpdateLotDetailsParams{
			ID: lot.ID,
			LotKeyParameters: pqtype.NullRawMessage{
				RawMessage: keyParamsJSON,
				Valid:      true,
			},
		})
		if err != nil {
			logger.Errorf("Ошибка при обновлении ключевых параметров лота ID %d: %v", lot.ID, err)
			return fmt.Errorf("не удалось обновить ключевые параметры лота: %w", err)
		}

		logger.Infof("Ключевые параметры успешно обновлены для лота ID %d", updatedLot.ID)
		return nil
	})
}

func (s *TenderProcessingService) getKindAndStandardTitle(posAPI api_models.PositionItem, lotTitle string) (string, string, error) {

	// --- Шаг 1: Определяем `kind` ---
	// Сравниваем "яблоки с яблоками" (RAW c RAW)

	var kind string
	if !posAPI.IsChapter {
		kind = "POSITION"
	} else {
		normalizedPosTitle := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(posAPI.JobTitle)), " "))
		normalizedLotTitle := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(lotTitle)), " "))

		// В нашем JSON это сравнение даст:
		// "лот №1 - set 1 оч ub2_устройство свайного основания" == "лот №1 - set 1 оч ub2_устройство свайного основания"
		// Это TRUE.
		if normalizedPosTitle == normalizedLotTitle {
			kind = "LOT_HEADER"
		} else {
			kind = "HEADER"
		}
	}

	// --- Шаг 2: Определяем `standardJobTitle` (Лемму для БД) ---
	// А вот здесь мы уже берем лемму, если она есть

	var standardJobTitleForDB string
	if posAPI.JobTitleNormalized != nil && strings.TrimSpace(*posAPI.JobTitleNormalized) != "" {
		// Берем лемму из JSON: "лот 1 set 1 оч ub2_устройство свайный основание"
		standardJobTitleForDB = strings.TrimSpace(*posAPI.JobTitleNormalized)
	} else {
		// Fallback: используем ту же простую нормализацию, что и на шаге 1
		trimmedRaw := strings.TrimSpace(posAPI.JobTitle)
		if trimmedRaw == "" {
			return "", "", nil
		}
		s.logger.Warnf("Поле 'job_title_normalized' отсутствует для '%s'. Используется raw.", trimmedRaw)
		standardJobTitleForDB = strings.ToLower(strings.Join(strings.Fields(trimmedRaw), " "))
	}

	return kind, standardJobTitleForDB, nil
}

// GetUnmatchedPositions (Версия 3: БЕЗ lot_title)
// Возвращает список несопоставленных позиций с контекстной информацией.
//
// Параметр limit должен быть положительным числом. Если limit <= 0, возвращается ошибка валидации.
// Если limit превышает MaxUnmatchedPositionsLimit, он автоматически ограничивается этим максимумом.
func (s *TenderProcessingService) GetUnmatchedPositions(
	ctx context.Context,
	limit int32,
) ([]api_models.UnmatchedPositionResponse, error) {

	// Валидация параметра limit
	if limit <= 0 {
		s.logger.Warnf("Получен некорректный limit: %d (должен быть > 0)", limit)
		return nil, NewValidationError("параметр limit должен быть положительным числом, получено: %d", limit)
	}

	// Ограничиваем максимальное значение
	if limit > MaxUnmatchedPositionsLimit {
		s.logger.Infof("Запрошено limit=%d, ограничиваем до MaxUnmatchedPositionsLimit=%d",
			limit, MaxUnmatchedPositionsLimit)
		limit = MaxUnmatchedPositionsLimit
	}

	// 1. Вызываем наш НОВЫЙ рекурсивный SQLC-запрос
	// (sqlc сгенерирует row.FullParentPath, но НЕ row.LotTitle)
	dbRows, err := s.store.GetUnmatchedPositions(ctx, limit)
	if err != nil {
		s.logger.Errorf("Ошибка GetUnmatchedPositions: %v", err)
		return nil, fmt.Errorf("ошибка БД: %w", err)
	}

	response := make([]api_models.UnmatchedPositionResponse, 0, len(dbRows))

	for _, row := range dbRows {
		var context string

		// 2. Собираем "богатую" строку (БЕЗ ЛОТА)
		// (SQL вернет '' (пустую строку), если разделов нет, благодаря COALESCE)
		if row.FullParentPath != "" {
			// Если есть "хлебные крошки"
			context = fmt.Sprintf("Раздел: %s | Позиция: %s",
				row.FullParentPath,
				row.JobTitleInProposal,
			)
		} else {
			// Если у позиции нет родительского раздела (лежит в корне)
			// Используем только ее собственный заголовок.
			context = fmt.Sprintf("Позиция: %s", row.JobTitleInProposal)
		}

		response = append(response, api_models.UnmatchedPositionResponse{
			PositionItemID:     row.PositionItemID,
			JobTitleInProposal: row.JobTitleInProposal,
			RichContextString:  context,
		})
	}

	s.logger.Infof("Найдено %d не сопоставленных позиций для RAG-воркера", len(response))
	return response, nil
}

// MatchPosition обрабатывает POST /api/v1/positions/match
func (s *TenderProcessingService) MatchPosition(
	ctx context.Context,
	req api_models.MatchPositionRequest,
) error {

	// Устанавливаем версию нормы по умолчанию, если Python ее не прислал
	normVersion := req.NormVersion
	if normVersion == 0 {
		normVersion = 1 // Версия по умолчанию
	}

	// Выполняем оба обновления в одной транзакции
	txErr := s.store.ExecTx(ctx, func(qtx *db.Queries) error {

		// 1. Обновляем position_items, "закрывая" NULL
		//
		err := qtx.SetCatalogPositionID(ctx, db.SetCatalogPositionIDParams{
			CatalogPositionID: sql.NullInt64{Int64: req.CatalogPositionID, Valid: true},
			ID:                req.PositionItemID,
		})
		if err != nil {
			s.logger.Errorf("MatchPosition: Ошибка SetCatalogPositionID: %v", err)
			return fmt.Errorf("ошибка обновления position_items: %w", err)
		}

		// 2. Обновляем matching_cache для будущих импортов
		// (Ищем "сырой" job_title, чтобы сохранить в кэш для отладки)
		posItem, err := qtx.GetPositionItemByID(ctx, req.PositionItemID)
		if err != nil {
			s.logger.Warnf("MatchPosition: не удалось найти %d для лога кэша: %v", req.PositionItemID, err)
			// Инициализируем пустой posItem для безопасного использования ниже
			posItem = db.PositionItem{}
		}

		// Устанавливаем TTL для кэша (например, 30 дней)
		expiresAt := sql.NullTime{
			Time:  time.Now().AddDate(0, 0, 30), // 30 дней от сейчас
			Valid: true,
		}

		// Определяем jobTitleText: используем реальное значение, если posItem загружен успешно
		jobTitleText := sql.NullString{String: "", Valid: false}
		if posItem.JobTitleInProposal != "" {
			jobTitleText = sql.NullString{String: posItem.JobTitleInProposal, Valid: true}
		}

		//
		err = qtx.UpsertMatchingCache(ctx, db.UpsertMatchingCacheParams{
			JobTitleHash:      req.Hash,
			NormVersion:       int16(normVersion), // (Убедитесь, что тип int16 в sqlc)
			JobTitleText:      jobTitleText,
			CatalogPositionID: req.CatalogPositionID,
			ExpiresAt:         expiresAt, // 👈 (ДОБАВЛЕНО ПОЛЕ)
		})
		if err != nil {
			s.logger.Errorf("MatchPosition: Ошибка UpsertMatchingCache: %v", err)
			return fmt.Errorf("ошибка обновления matching_cache: %w", err)
		}

		return nil // Commit транзакции
	})

	if txErr != nil {
		return txErr // Возвращаем ошибку транзакции
	}

	s.logger.Infof("Успешно сопоставлена позиция %d -> %d (hash: %s)",
		req.PositionItemID, req.CatalogPositionID, req.Hash)
	return nil
}

// GetUnindexedCatalogItems реализует GET /api/v1/catalog/unindexed
func (s *TenderProcessingService) GetUnindexedCatalogItems(
	ctx context.Context,
	limit int32,
) ([]api_models.UnmatchedPositionResponse, error) {
	// (Мы переиспользуем DTO UnmatchedPositionResponse)

	// 1. Вызываем наш SQLC-запрос
	dbRows, err := s.store.GetUnindexedCatalogItems(ctx, limit)
	if err != nil {
		s.logger.Errorf("Ошибка GetUnindexedCatalogItems: %v", err)
		return nil, fmt.Errorf("ошибка БД: %w", err)
	}

	response := make([]api_models.UnmatchedPositionResponse, 0, len(dbRows))

	// 2. "Обогащаем" данные для RAG-индекса
	for _, row := range dbRows {

		// 3. Собираем "богатую" строку для ИНДЕКСА
		// (Индекс НЕ содержит "хлебных крошек",
		// он содержит только суть самой работы)
		context := fmt.Sprintf("Работа: %s | Описание: %s",
			row.StandardJobTitle,   // Лемма
			row.Description.String, // "Сырое" название
		)

		response = append(response, api_models.UnmatchedPositionResponse{
			// Python-воркеру нужен 'catalog_id'
			PositionItemID:     row.CatalogID, // 👈 Передаем ID каталога
			JobTitleInProposal: row.StandardJobTitle,
			RichContextString:  context,
		})
	}

	s.logger.Infof("Найдено %d неиндексированных записей каталога для RAG", len(response))
	return response, nil
}

// MarkCatalogItemsAsActive реализует POST /api/v1/catalog/indexed
func (s *TenderProcessingService) MarkCatalogItemsAsActive(
	ctx context.Context,
	catalogIDs []int64,
) error {

	if len(catalogIDs) == 0 {
		s.logger.Warn("MarkCatalogItemsAsActive: получен пустой список ID, действие не требуется.")
		return nil
	}

	// 1. Вызываем наш SQLC-запрос
	err := s.store.SetCatalogStatusActive(ctx, catalogIDs)

	if err != nil {
		s.logger.Errorf("Ошибка MarkCatalogItemsAsActive: %v", err)
		return fmt.Errorf("ошибка БД: %w", err)
	}

	s.logger.Infof("Установлен статус 'active' для %d записей каталога", len(catalogIDs))
	return nil
}

// SuggestMerge реализует POST /api/v1/merges/suggest
//
func (s *TenderProcessingService) SuggestMerge(
	ctx context.Context,
	req api_models.SuggestMergeRequest,
) error {

	// Защита: не предлагать слияние позиции с самой собой
	if req.MainPositionID == req.DuplicatePositionID {
		s.logger.Warnf("Попытка предложить слияние позиции %d с самой собой. Пропущено.", req.MainPositionID)
		return nil // Не ошибка, просто пропускаем
	}

	// 1. Вызываем наш SQLC-запрос
	err := s.store.UpsertSuggestedMerge(ctx, db.UpsertSuggestedMergeParams{
		MainPositionID:      req.MainPositionID,
		DuplicatePositionID: req.DuplicatePositionID,
		SimilarityScore:     float32(req.SimilarityScore),
	})

	if err != nil {
		s.logger.Errorf("Ошибка UpsertSuggestedMerge: %v", err)
		return fmt.Errorf("ошибка БД при создании предложения о слиянии: %w", err)
	}

	s.logger.Infof("Успешно предложено/обновлено слияние: %d -> %d (Score: %.2f)",
		req.DuplicatePositionID, req.MainPositionID, req.SimilarityScore)
	return nil
}