package importer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/entities"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// TenderImportService отвечает ТОЛЬКО за импорт тендерных данных,
// включая импорт тендера, объектов, лотов, предложений, позиций и итоговых строк.
type TenderImportService struct {
	store  db.Store        // SQLC-совместимый store, обеспечивающий транзакции
	logger *logging.Logger // Обёртка над logrus с поддержкой полей

	// Единственная зависимость - менеджер сущностей
	Entities *entities.EntityManager
} // NewTenderImportService создает новый экземпляр TenderImportService.
// Получает все зависимости извне (Dependency Injection).
func NewTenderImportService(
	store db.Store,
	logger *logging.Logger,
	entityManager *entities.EntityManager,
) *TenderImportService {
	return &TenderImportService{
		store:    store,
		logger:   logger,
		Entities: entityManager,
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
func (s *TenderImportService) ImportFullTender(
	ctx context.Context,
	payload *api_models.FullTenderData,
	rawJSON []byte,
) (int64, map[string]int64, bool, error) {

	s.logger.Infof("Начинаем импорт тендера %s, размер JSON: %d байт, количество лотов: %d", 
		payload.TenderID, len(rawJSON), len(payload.LotsData))

	var newTenderDBID int64
	lotIDs := make(map[string]int64)
	anyNewPendingItems := false

	txErr := s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		s.logger.Info("Транзакция начата")
		
		// Шаг 1: Обработка основной информации о тендере
		s.logger.Info("Шаг 1: Обработка основной информации о тендере")
		dbTender, err := s.processCoreTenderData(ctx, qtx, payload)
		if err != nil {
			s.logger.Errorf("Ошибка на шаге 1: %v", err)
			return err
		}
		newTenderDBID = dbTender.ID
		s.logger.Infof("Тендер создан с DB ID: %d", newTenderDBID)

		// Шаг 2: Обработка лотов
		s.logger.Infof("Шаг 2: Обработка %d лотов", len(payload.LotsData))
		lotIndex := 0
		for lotKey, lotAPI := range payload.LotsData {
			lotIndex++
			s.logger.Infof("Обрабатываем лот %d/%d (ключ: %s)", lotIndex, len(payload.LotsData), lotKey)
			
			lotDBID, lotHasNewPending, err := s.processLot(ctx, qtx, dbTender.ID, lotKey, lotAPI)
			if err != nil {
				s.logger.Errorf("Ошибка при обработке лота '%s': %v", lotKey, err)
				return fmt.Errorf("ошибка при обработке лота '%s': %w", lotKey, err)
			}
			lotIDs[lotKey] = lotDBID
			if lotHasNewPending {
				anyNewPendingItems = true
			}
			s.logger.Infof("Лот %s обработан, DB ID: %d", lotKey, lotDBID)
		}
		s.logger.Info("Все лоты обработаны успешно")
		
		// Шаг 3: UPSERT "сырого" JSON в tender_raw_data в рамках той же транзакции.
		// sqlc сгенерировал тип параметра как json.RawMessage — передаём rawJSON как есть.
		s.logger.Infof("Шаг 3: Сохраняем исходный JSON для тендера ID: %d (размер: %d байт)", newTenderDBID, len(rawJSON))
		if _, err := qtx.UpsertTenderRawData(ctx, db.UpsertTenderRawDataParams{
			TenderID: newTenderDBID,
			RawData:  json.RawMessage(rawJSON),
		}); err != nil {
			s.logger.Errorf("Ошибка при сохранении tender_raw_data для тендера ID %d: %v", newTenderDBID, err)
			return fmt.Errorf("не удалось сохранить исходный JSON (tender_raw_data): %w", err)
		}
		s.logger.Infof("Исходный JSON успешно сохранен для тендера ID: %d", newTenderDBID)
		s.logger.Info("Транзакция завершена успешно")

		return nil // транзакция завершится успешно
	})

	if txErr != nil {
		s.logger.Errorf("Не удалось импортировать тендер ETP_ID %s: %v", payload.TenderID, txErr)
		return 0, nil, false, fmt.Errorf("транзакция импорта тендера провалена: %w", txErr)
	}

	s.logger.Infof("Тендер ETP_ID %s успешно импортирован с ID базы данных: %d, новые pending позиции: %v", payload.TenderID, newTenderDBID, anyNewPendingItems)
	return newTenderDBID, lotIDs, anyNewPendingItems, nil
}
