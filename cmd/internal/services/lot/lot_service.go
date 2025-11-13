package lot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"
	"github.com/sqlc-dev/pqtype"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// LotService управляет операциями с лотами
type LotService struct {
	store  db.Store
	logger *logging.Logger
}

// NewLotService создает новый экземпляр LotService
func NewLotService(store db.Store, logger *logging.Logger) *LotService {
	return &LotService{
		store:  store,
		logger: logger,
	}
}

// UpdateLotKeyParameters обновляет ключевые параметры лота, найденного по tender_id и lot_key
func (s *LotService) UpdateLotKeyParameters(
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
func (s *LotService) UpdateLotKeyParametersDirectly(
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
