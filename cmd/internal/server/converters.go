package server

import (
	"encoding/json"
	"time"

	"github.com/sqlc-dev/pqtype"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// parseKeyParameters безопасно парсит pqtype.NullRawMessage в json.RawMessage.
// Возвращает сырой JSON со всеми типами данных (числа, bool, вложенные объекты).
func parseKeyParameters(p pqtype.NullRawMessage, logger *logging.Logger) json.RawMessage {
	// Проверяем, что поле не NULL и JSON не пустой или равен "null"
	if !p.Valid || len(p.RawMessage) == 0 || string(p.RawMessage) == "null" {
		return json.RawMessage("{}")
	}

	// Валидируем, что это корректный JSON
	if !json.Valid(p.RawMessage) {
		logger.Warnf("невалидный JSON в key_parameters")
		return json.RawMessage("{}")
	}

	return json.RawMessage(p.RawMessage)
}

// Функция newLotResponse, которая использует parseKeyParameters
func newLotResponse(lot db.Lot, logger *logging.Logger) LotResponse {
	return LotResponse{
		ID:            lot.ID,
		LotKey:        lot.LotKey,
		LotTitle:      lot.LotTitle,
		TenderID:      lot.TenderID,
		KeyParameters: parseKeyParameters(lot.LotKeyParameters, logger), // ✅ Используем нашу финальную функцию
		CreatedAt:     lot.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     lot.UpdatedAt.Format(time.RFC3339),
		Winners:       []WinnerResponse{}, // ✅ Инициализируем пустым массивом вместо nil
	}
}
