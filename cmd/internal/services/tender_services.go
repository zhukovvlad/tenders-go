package services

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/util"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

type TenderProcessingService struct {
	store  db.Store // Используем интерфейс Store
	logger *logging.Logger
}

func NewTenderProcessingService(store db.Store, logger *logging.Logger) *TenderProcessingService {
	return &TenderProcessingService{
		store:  store,
		logger: logger,
	}
}

// processSinglePosition обрабатывает одну позицию
func (s *TenderProcessingService) processSinglePosition(ctx context.Context, qtx db.Querier, proposalID int64, positionKey string, posAPI api_models.PositionItem) error {
	// 1. Получаем зависимости
	catPos, err := s.GetOrCreateCatalogPosition(ctx, qtx, posAPI.JobTitleNormalized, posAPI.JobTitle)
	if err != nil {
		return fmt.Errorf("не удалось получить/создать позицию каталога: %w", err)
	}

	unitID, err := s.GetOrCreateUnitOfMeasurement(ctx, qtx, posAPI.Unit)
	if err != nil {
		return fmt.Errorf("не удалось получить/создать единицу измерения: %w", err)
	}

	// 2. Маппинг данных
	params := mapApiPositionToDbParams(proposalID, positionKey, catPos.ID, unitID, posAPI)

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

func (s *TenderProcessingService) GetOrCreateObject(ctx context.Context, qtx db.Querier, title string, address string) (db.Object, error) {
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
) error {
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

// ProcessContractorItems теперь только оркестрирует процесс
func (s *TenderProcessingService) ProcessContractorItems(ctx context.Context, qtx db.Querier, proposalID int64, itemsAPI api_models.ContractorItemsContainer) error {
	logger := s.logger.WithField("proposal_id", proposalID)
	logger.Info("Обработка позиций и итогов")

	if itemsAPI.Positions != nil {
		for key, posAPI := range itemsAPI.Positions {
			// Вызываем хелпер для одной позиции
			if err := s.processSinglePosition(ctx, qtx, proposalID, key, posAPI); err != nil {
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

// Приватный хелпер для инкапсуляции логики нормализации
func (s *TenderProcessingService) normalizeJobTitle(rawJobTitle string, normalized *string) (string, error) {
	if normalized != nil && strings.TrimSpace(*normalized) != "" {
		return strings.TrimSpace(*normalized), nil
	}

	trimmedRaw := strings.TrimSpace(rawJobTitle)
	if trimmedRaw == "" {
		return "", fmt.Errorf("название работы для позиции каталога не может быть пустым")
	}
	s.logger.Warnf("Поле 'job_title_normalized' отсутствует для '%s'. Используется raw.", rawJobTitle)
	return strings.ToLower(strings.Join(strings.Fields(trimmedRaw), " ")), nil
}

func (s *TenderProcessingService) GetOrCreateCatalogPosition(
	ctx context.Context,
	qtx db.Querier,
	apiJobTitleNormalized *string, // <--- Принимаем указатель на нормализованное название
	rawJobTitle string, // <--- Принимаем "сырое" название
) (db.CatalogPosition, error) {

	standardJobTitleForDB, err := s.normalizeJobTitle(rawJobTitle, apiJobTitleNormalized)
	if err != nil {
		return db.CatalogPosition{}, err
	}

	opLogger := s.logger.WithFields(logrus.Fields{
		"service_method":          "GetOrCreateCatalogPosition",
		"input_raw_job_title":     rawJobTitle,
		"used_standard_job_title": standardJobTitleForDB,
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
			return qtx.CreateCatalogPosition(ctx, db.CreateCatalogPositionParams{
				StandardJobTitle: standardJobTitleForDB,
				Description:      sql.NullString{String: rawJobTitle, Valid: true},
			})
		},
		// diffFn: обновление не требуется, поэтому всегда возвращаем false.
		// Тип возвращаемых параметров теперь - UpdateCatalogPositionDetailsParams.
		func(existing db.CatalogPosition) (bool, db.UpdateCatalogPositionDetailsParams, error) {
			return false, db.UpdateCatalogPositionDetailsParams{}, nil
		},
		// updateFn: передаем реальную функцию обновления.
		// Она никогда не будет вызвана из-за ложного значения от diffFn,
		// но она нужна, чтобы код скомпилировался.
		func(params db.UpdateCatalogPositionDetailsParams) (db.CatalogPosition, error) {
			return qtx.UpdateCatalogPositionDetails(ctx, params)
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

func (s *TenderProcessingService) ImportFullTender(
	ctx context.Context,
	payload *api_models.FullTenderData,
) error {
	txErr := s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		// Шаг 1: Обработка основной информации (Объект, Исполнитель, Тендер)
		dbTender, err := s.processCoreTenderData(ctx, qtx, payload)
		if err != nil {
			return err // Ошибка уже содержит нужный контекст
		}

		// Шаг 2: Обработка каждого лота по очереди
		for lotKey, lotAPI := range payload.LotsData {
			if err := s.processLot(ctx, qtx, dbTender.ID, lotKey, lotAPI); err != nil {
				// Оборачиваем ошибку для указания, в каком лоте проблема
				return fmt.Errorf("ошибка при обработке лота '%s': %w", lotKey, err)
			}
		}

		return nil // Транзакция завершится успешно
	})

	if txErr != nil {
		// Логируем финальную ошибку верхнего уровня
		s.logger.Errorf("Не удалось импортировать тендер ETP_ID %s: %v", payload.TenderID, txErr)
		return fmt.Errorf("транзакция импорта тендера провалена: %w", txErr)
	}

	s.logger.Infof("Тендер ETP_ID %s успешно импортирован.", payload.TenderID)
	return nil
}

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

// processLot обрабатывает один лот и все его предложения
func (s *TenderProcessingService) processLot(ctx context.Context, qtx db.Querier, tenderID int64, lotKey string, lotAPI api_models.Lot) error {
	dbLot, err := qtx.UpsertLot(ctx, db.UpsertLotParams{
		TenderID: tenderID,
		LotKey:   lotKey,
		LotTitle: lotAPI.LotTitle,
	})
	if err != nil {
		return fmt.Errorf("не удалось сохранить лот: %w", err)
	}

	// Обработка базового предложения
	if err := s.processProposal(ctx, qtx, dbLot.ID, &lotAPI.BaseLineProposal, true); err != nil {
		return fmt.Errorf("обработка базового предложения: %w", err)
	}

	// Обработка предложений подрядчиков
	for _, proposalDetails := range lotAPI.ProposalData {
		if err := s.processProposal(ctx, qtx, dbLot.ID, &proposalDetails, false); err != nil {
			return fmt.Errorf("обработка предложения от '%s': %w", proposalDetails.Title, err)
		}
	}
	return nil
}

// processProposal — унифицированный метод для обработки любого предложения
func (s *TenderProcessingService) processProposal(ctx context.Context, qtx db.Querier, lotID int64, proposalAPI *api_models.ContractorProposalDetails, isBaseline bool) error {
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
		LotID:                  lotID,
		ContractorID:           dbContractor.ID,
		IsBaseline:             isBaseline,
		ContractorCoordinate:   util.NullableString(&proposalAPI.ContractorCoordinate),
		// ... другие поля ...
	})
	if err != nil {
		return fmt.Errorf("не удалось сохранить предложение: %w", err)
	}

	// Вызываем уже существующие у вас публичные методы, сделав их приватными
	if err := s.ProcessProposalAdditionalInfo(ctx, qtx, dbProposal.ID, proposalAPI.AdditionalInfo); err != nil {
		return err
	}

	if err := s.ProcessContractorItems(ctx, qtx, dbProposal.ID, proposalAPI.ContractorItems); err != nil {
		return err
	}
	return nil
}
