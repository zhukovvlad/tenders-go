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

func (s *TenderProcessingService) GetOrCreateObject(ctx context.Context, qtx db.Querier, title string, address string) (db.Object, error) {
	opLogger := s.logger.WithFields(logrus.Fields{
		"entity":  "object",
		"title":   title,
		"address": address,
	})

	if strings.TrimSpace(title) == "" {
		opLogger.Error("Название объекта (title) не может быть пустым")
		return db.Object{}, fmt.Errorf("название объекта (title) не может быть пустым")
	}
	if strings.TrimSpace(address) == "" { // По вашей схеме address NOT NULL
		opLogger.Error("Адрес объекта (address) не может быть пустым")
		return db.Object{}, fmt.Errorf("адрес объекта (address) не может быть пустым для title '%s'", title)
	}

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

	if strings.TrimSpace(name) == "" {
		opLogger.Error("Имя исполнителя (name) не может быть пустым")
		return db.Executor{}, fmt.Errorf("имя исполнителя (name) не может быть пустым")
	}

	if strings.TrimSpace(phone) == "" {
		opLogger.Error("Телефон исполнителя (phone) не может быть пустым")
		return db.Executor{}, fmt.Errorf("телефон исполнителя (phone) не может быть пустым для name '%s'", name)
	}

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

	// --- Валидация входных данных ---
	if strings.TrimSpace(inn) == "" {
		opLogger.Error("ИНН подрядчика не может быть пустым")
		return db.Contractor{}, fmt.Errorf("ИНН подрядчика не может быть пустым")
	}
	if strings.TrimSpace(title) == "" {
		opLogger.Error("Название подрядчика (title) не может быть пустым")
		return db.Contractor{}, fmt.Errorf("название подрядчика (title) не может быть пустым для ИНН %s", inn)
	}
	if strings.TrimSpace(address) == "" {
		opLogger.Error("Адрес подрядчика (address) не может быть пустым")
		return db.Contractor{}, fmt.Errorf("адрес подрядчика (address) не может быть пустым для ИНН %s", inn)
	}
	if strings.TrimSpace(accreditation) == "" {
		opLogger.Error("Аккредитация подрядчика (accreditation) не может быть пустой")
		return db.Contractor{}, fmt.Errorf("аккредитация подрядчика (accreditation) не может быть пустой для ИНН %s", inn)
	}

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
			}
			if existing.Address != address {
				opLogger.Infof("Адрес подрядчика отличается: '%s' -> '%s'", existing.Address, address)
				updateParams.Address = sql.NullString{String: address, Valid: true}
			}
			if existing.Accreditation != accreditation {
				opLogger.Infof("Аккредитация подрядчика отличается: '%s' -> '%s'", existing.Accreditation, accreditation)
				updateParams.Accreditation = sql.NullString{String: accreditation, Valid: true}
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

// ProcessContractorItems обрабатывает positions и summary для предложения
func (s *TenderProcessingService) ProcessContractorItems(ctx context.Context, qtx db.Querier, proposalID int64, itemsAPI api_models.ContractorItemsContainer) error {
	logger := s.logger.WithField("proposal_id", proposalID).WithField("section", "contractor_items")
	logger.Info("Обработка позиций и итогов")

	// Обработка Positions
	if itemsAPI.Positions != nil {
		// Опционально: удалить старые position_items перед добавлением новых
		// if err := qtx.DeleteAllPositionItemsForProposal(ctx, proposalID); err != nil {
		//     return fmt.Errorf("не удалось удалить старые позиции: %w", err)
		// }
		for key, posAPI := range itemsAPI.Positions {
			posLogger := logger.WithField("position_key", key)

			catPos, err := s.GetOrCreateCatalogPosition(ctx, qtx, posAPI.JobTitleNormalized, posAPI.JobTitle)
			if err != nil {
				return fmt.Errorf("обработка catalog_position для '%s': %w", posAPI.JobTitle, err)
			}

			unitID, err := s.GetOrCreateUnitOfMeasurement(ctx, qtx, posAPI.Unit)
			if err != nil {
				return fmt.Errorf("обработка unit для '%s': %w", *posAPI.Unit, err)
			}

			params := db.UpsertPositionItemParams{
				ProposalID:                    proposalID,
				CatalogPositionID:             catPos.ID,
				PositionKeyInProposal:         key,
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
				// ArticleSMR не было в параметрах UpsertPositionItem, если нужно - добавьте
			}
			if posAPI.ArticleSMR != nil { // Пример как добавить ArticleSMR, если он есть в UpsertPositionItemParams
				// params.ArticleSMR = util.NullableString(posAPI.ArticleSMR)
			}
			// В вашей структуре PositionItem в api_models поле JobTitleNormalized было *string.
			// Оно используется для поиска/создания catalog_position. Если его нужно хранить и в position_items,
			// то добавьте его в UpsertPositionItemParams и передайте util.NullableString(posAPI.JobTitleNormalized)

			if _, err := qtx.UpsertPositionItem(ctx, params); err != nil {
				posLogger.Errorf("Не удалось сохранить позицию: %v", err)
				return fmt.Errorf("сохранение позиции '%s': %w", key, err)
			}
		}
	}
	logger.Info("Позиции успешно обработаны")

	// Обработка Summary
	if itemsAPI.Summary != nil {
		// Опционально: удалить старые summary_lines перед добавлением новых
		// if err := qtx.DeleteAllSummaryLinesForProposal(ctx, proposalID); err != nil {
		//     return fmt.Errorf("не удалось удалить старые итоги: %w", err)
		// }
		for key, sumLineAPI := range itemsAPI.Summary {
			sumLogger := logger.WithField("summary_key", key)
			params := db.UpsertProposalSummaryLineParams{
				ProposalID:        proposalID,
				SummaryKey:        key,
				JobTitle:          sumLineAPI.JobTitle,
				MaterialsCost:     util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Materials)), // В summary обычно unit_cost пустой
				WorksCost:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Works)),
				IndirectCostsCost: util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.IndirectCosts)),
				TotalCost:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Total)), // Предполагаем, что total_cost.total НЕ NULL в JSON для summary
				// Убедитесь, что TotalCost в таблице NOT NULL, или используйте util.NullableFloat64
				// CommentContractor и DeviationFromBaselineCost нужно будет добавить в UpsertProposalSummaryLineParams
				// и передать сюда: util.NullableString(sumLineAPI.CommentContractor), util.NullableFloat64(sumLineAPI.Deviation)
			}
			// Если SuggestedQuantity и OrganizierQuantityCost есть в summary и нужны в БД:
			// params.SuggestedQuantity = util.NullableFloat64(sumLineAPI.SuggestedQuantity)
			// params.OrganzierQuantityCost = util.NullableFloat64(sumLineAPI.OrganzierQuantityCost)

			if _, err := qtx.UpsertProposalSummaryLine(ctx, params); err != nil {
				sumLogger.Errorf("Не удалось сохранить строку итога: %v", err)
				return fmt.Errorf("сохранение строки итога '%s': %w", key, err)
			}
		}
	}
	logger.Info("Итоги успешно обработаны")
	return nil
}

func (s *TenderProcessingService) GetOrCreateCatalogPosition(
	ctx context.Context,
	qtx db.Querier,
	apiJobTitleNormalized *string, // <--- Принимаем указатель на нормализованное название
	rawJobTitle string, // <--- Принимаем "сырое" название
) (db.CatalogPosition, error) {

	var standardJobTitleForDB string

	if apiJobTitleNormalized != nil && strings.TrimSpace(*apiJobTitleNormalized) != "" {
		standardJobTitleForDB = strings.TrimSpace(*apiJobTitleNormalized)
	} else {
		s.logger.Warnf("Поле 'job_title_normalized' отсутствует или пусто для raw_job_title '%s'. Используется нормализация raw_job_title.", rawJobTitle)
		if strings.TrimSpace(rawJobTitle) == "" {
			s.logger.Error("И raw_job_title, и job_title_normalized пусты для позиции каталога.")
			return db.CatalogPosition{}, fmt.Errorf("название работы для позиции каталога не может быть пустым")
		}
		standardJobTitleForDB = strings.ToLower(strings.Join(strings.Fields(rawJobTitle), " "))
	}

	opLogger := s.logger.WithFields(logrus.Fields{
		"service_method":          "GetOrCreateCatalogPosition",
		"input_raw_job_title":     rawJobTitle,
		"used_standard_job_title": standardJobTitleForDB,
	})

	catalogPos, err := qtx.GetCatalogPositionByStandardJobTitle(ctx, standardJobTitleForDB)
	if err != nil {
		if err == sql.ErrNoRows {
			opLogger.Info("Позиция каталога не найдена, создается новая.")
			var descriptionValue sql.NullString
			if strings.TrimSpace(rawJobTitle) != "" {
				descriptionValue = sql.NullString{String: rawJobTitle, Valid: true}
			}
			createdPos, createErr := qtx.CreateCatalogPosition(ctx, db.CreateCatalogPositionParams{
				StandardJobTitle: standardJobTitleForDB,
				Description:      descriptionValue,
			})
			if createErr != nil {
				opLogger.Errorf("Ошибка создания позиции каталога: %v", createErr)
				return db.CatalogPosition{}, fmt.Errorf("ошибка создания позиции каталога '%s': %w", standardJobTitleForDB, createErr)
			}
			opLogger.Infof("Позиция каталога успешно создана, ID: %d", createdPos.ID)
			return createdPos, nil
		}
		opLogger.Errorf("Ошибка получения позиции каталога по standard_job_title: %v", err)
		return db.CatalogPosition{}, fmt.Errorf("ошибка получения позиции каталога по standard_job_title '%s': %w", standardJobTitleForDB, err)
	}
	opLogger.Infof("Найдена существующая позиция каталога, ID: %d", catalogPos.ID)
	return catalogPos, nil
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
