package entities

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// EntityManager управляет операциями с сущностями (объекты, исполнители, подрядчики и т.д.)
type EntityManager struct {
	logger *logging.Logger
}

// NewEntityManager создает новый экземпляр EntityManager
func NewEntityManager(logger *logging.Logger) *EntityManager {
	return &EntityManager{
		logger: logger,
	}
}

func getOrCreateOrUpdate[T any, P any](
	_ context.Context,
	_ db.Querier,
	// Функция для получения существующей сущности
	getFn func() (T, error),
	// Функция для создания новой сущности
	createFn func() (T, error),
	// Функция, которая проверяет, нужно ли обновление.
	// Возвращает:
	// 1. bool - нужно ли обновление.
	// 2. P - параметры для обновления.
	// 3. error - если ошибка.
	diffFn func(existing T) (bool, P, error),
	// Функция для выполнения обновления
	updateFn func(params P) (T, error),
) (T, error) {
	existing, err := getFn()
	if err != nil {
		if err == sql.ErrNoRows {
			// Сущность не найдена, создаем новую
			return createFn()
		}

		var zero T
		return zero, err
	}

	// Сущность найдена, проверяем необходимость обновления
	needsUpdate, updateParams, err := diffFn(existing)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("ошибка при проверке необходимости обновления: %w", err)
	}

	if needsUpdate {
		return updateFn(updateParams)
	}

	return existing, nil
}

func (em *EntityManager) getKindAndStandardTitle(posAPI api_models.PositionItem, lotTitle string) (string, string, error) {

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
		em.logger.Warnf("Поле 'job_title_normalized' отсутствует для '%s'. Используется raw.", trimmedRaw)
		standardJobTitleForDB = strings.ToLower(strings.Join(strings.Fields(trimmedRaw), " "))
	}

	return kind, standardJobTitleForDB, nil
}

func (em *EntityManager) GetOrCreateObject(
	ctx context.Context,
	qtx db.Querier,
	title string,
	address string,
) (db.Object, error) {
	opLogger := em.logger.WithFields(logrus.Fields{
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

// GetOrCreateExecutor находит исполнителя по name. Если не найден, создает нового.
// Если найден, но телефон отличается, обновляет телефон.
func (em *EntityManager) GetOrCreateExecutor(
	ctx context.Context,
	qtx db.Querier,
	name string,
	phone string,
) (db.Executor, error) {
	opLogger := em.logger.WithFields(logrus.Fields{
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

func (em *EntityManager) GetOrCreateContractor(
	ctx context.Context,
	qtx db.Querier,
	inn string,
	title string,
	address string,
	accreditation string,
) (db.Contractor, error) {
	opLogger := em.logger.WithField(
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

func (em *EntityManager) GetOrCreateCatalogPosition(
	ctx context.Context,
	qtx db.Querier,
	posAPI api_models.PositionItem,
	lotTitle string,
) (db.CatalogPosition, bool, error) {

	// Шаг 1: Получаем и kind, и standardJobTitle
	kind, standardJobTitleForDB, err := em.getKindAndStandardTitle(posAPI, lotTitle)
	if err != nil {
		// Эта ошибка теперь не должна возникать, т.к. хелпер обрабатывает пустые строки
		return db.CatalogPosition{}, false, err
	}

	// Если имя пустое (например, заголовок с пустым job_title),
	// мы не должны создавать запись в catalog_positions.
	if standardJobTitleForDB == "" {
		// Возвращаем пустую структуру, `processSinglePosition` пропустит эту позицию
		return db.CatalogPosition{}, false, nil
	}

	opLogger := em.logger.WithFields(logrus.Fields{
		"service_method":          "GetOrCreateCatalogPosition",
		"input_raw_job_title":     posAPI.JobTitle,
		"used_standard_job_title": standardJobTitleForDB,
		"determined_kind":         kind,
	})

	// Используем getOrCreateOrUpdate.
	// P теперь - это существующий тип db.UpdateCatalogPositionDetailsParams
	
	// Флаг для отслеживания создания новой pending_indexing позиции
	var isNewPendingItem bool
	
	result, err := getOrCreateOrUpdate(
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
				isNewPendingItem = true        // Взводим флаг
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
	
	return result, isNewPendingItem, err
}

// GetOrCreateUnitOfMeasurement находит или создает единицу измерения.
// apiUnitName - это указатель на строку с названием единицы измерения из JSON (поле "unit" из PositionItem).
// Возвращает sql.NullInt64, так как unit_id в position_items может быть NULL.
func (em *EntityManager) GetOrCreateUnitOfMeasurement(
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

	opLogger := em.logger.WithFields(logrus.Fields{
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
