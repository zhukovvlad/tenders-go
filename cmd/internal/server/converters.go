package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sqlc-dev/pqtype"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// parseKeyParameters безопасно парсит pqtype.NullRawMessage в map[string]string.
func parseKeyParameters(p pqtype.NullRawMessage, logger *logging.Logger) map[string]string {
    params := make(map[string]string)

    // Проверяем, что поле не NULL и JSON не пустой или равен "null"
    if !p.Valid || len(p.RawMessage) == 0 || string(p.RawMessage) == "null" {
        return params
    }

    var rawParams map[string]interface{}
    if err := json.Unmarshal(p.RawMessage, &rawParams); err != nil {
        logger.Warnf("не удалось распарсить key parameters: %v", err)
        return params
    }

    // Конвертируем все значения в строки
    for k, v := range rawParams {
        params[k] = fmt.Sprintf("%v", v)
    }
    
    return params
}

// Функция newLotResponse, которая использует parseKeyParameters
func newLotResponse(lot db.Lot, logger *logging.Logger) LotResponse {
    return LotResponse{
        ID:              lot.ID,
        LotKey:          lot.LotKey,
        LotTitle:        lot.LotTitle,
        TenderID:        lot.TenderID,
        KeyParameters:   parseKeyParameters(lot.LotKeyParameters, logger), // ✅ Используем нашу финальную функцию
        CreatedAt:       lot.CreatedAt.Format(time.RFC3339),
        UpdatedAt:       lot.UpdatedAt.Format(time.RFC3339),
        Winners:         []WinnerResponse{}, // ✅ Инициализируем пустым массивом вместо nil
    }
}