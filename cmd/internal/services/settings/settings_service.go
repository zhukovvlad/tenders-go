// Package settings предоставляет сервисный слой для управления системными настройками.
//
// Основная задача — CRUD операции с таблицей system_settings и побочные эффекты
// при изменении критических параметров (например, порога дедупликации).
//
// # Побочный эффект: dedup_distance_threshold
//
// При обновлении настройки "dedup_distance_threshold" на более строгое значение,
// сервис автоматически удаляет все PENDING merge-заявки из suggested_merges,
// чьи similarity_score ниже нового порога. Формула:
//
//	similarity_score < (1.0 - new_distance_threshold)
//
// Это гарантирует, что оператору не будут показываться заявки,
// которые больше не проходят по обновлённому критерию качества.
package settings

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// SettingsService управляет операциями с системными настройками.
type SettingsService struct {
	store  db.Store
	logger logging.Logger
}

// NewSettingsService создаёт новый экземпляр SettingsService.
func NewSettingsService(store db.Store, logger logging.Logger) *SettingsService {
	return &SettingsService{
		store:  store,
		logger: logger,
	}
}

// DedupDistanceThresholdKey — ключ настройки порога дедупликации.
const DedupDistanceThresholdKey = "dedup_distance_threshold"

// UpdateSetting обновляет системную настройку и выполняет побочные эффекты.
//
// Бизнес-логика:
//  1. Валидация: ровно одно из ValueNumeric/ValueString/ValueBoolean должно быть задано.
//  2. Upsert соответствующим SQLC-запросом.
//  3. Если ключ == "dedup_distance_threshold" и ValueNumeric задано —
//     удаляет PENDING merge-заявки, не проходящие по новому порогу.
//  4. Возвращает обновлённую настройку.
func (s *SettingsService) UpdateSetting(
	ctx context.Context,
	req api_models.UpdateSystemSettingRequest,
	updatedBy string,
) (*api_models.SystemSettingResponse, error) {
	logger := s.logger.WithField("method", "UpdateSetting").WithField("key", req.Key)

	// Валидация: ключ не пустой (gin binding:"required" покрывает, но double-check)
	if strings.TrimSpace(req.Key) == "" {
		return nil, apierrors.NewValidationError("ключ настройки (key) не может быть пустым")
	}

	// Валидация: ровно одно значение должно быть задано
	valueCount := 0
	if req.ValueNumeric != nil {
		valueCount++
	}
	if req.ValueString != nil {
		valueCount++
	}
	if req.ValueBoolean != nil {
		valueCount++
	}
	if valueCount == 0 {
		return nil, apierrors.NewValidationError("необходимо указать ровно одно значение (value_numeric, value_string или value_boolean)")
	}
	if valueCount > 1 {
		return nil, apierrors.NewValidationError("допускается только одно значение (value_numeric, value_string или value_boolean), передано: %d", valueCount)
	}

	var setting db.SystemSetting
	var err error

	description := sql.NullString{}
	if req.Description != "" {
		description = sql.NullString{String: req.Description, Valid: true}
	}

	switch {
	case req.ValueNumeric != nil:
		numStr := strconv.FormatFloat(*req.ValueNumeric, 'f', -1, 64)
		setting, err = s.store.UpsertSystemSettingNumeric(ctx, db.UpsertSystemSettingNumericParams{
			Key:          req.Key,
			ValueNumeric: sql.NullString{String: numStr, Valid: true},
			Description:  description,
			UpdatedBy:    updatedBy,
		})

	case req.ValueString != nil:
		setting, err = s.store.UpsertSystemSettingString(ctx, db.UpsertSystemSettingStringParams{
			Key:         req.Key,
			ValueString: sql.NullString{String: *req.ValueString, Valid: true},
			Description: description,
			UpdatedBy:   updatedBy,
		})

	case req.ValueBoolean != nil:
		setting, err = s.store.UpsertSystemSettingBoolean(ctx, db.UpsertSystemSettingBooleanParams{
			Key:          req.Key,
			ValueBoolean: sql.NullBool{Bool: *req.ValueBoolean, Valid: true},
			Description:  description,
			UpdatedBy:    updatedBy,
		})
	}

	if err != nil {
		return nil, fmt.Errorf("ошибка upsert настройки %q: %w", req.Key, err)
	}

	logger.Infof("Настройка %q обновлена пользователем %s", req.Key, updatedBy)

	// --- Побочный эффект: очистка PENDING merges при обновлении порога ---
	if req.Key == DedupDistanceThresholdKey && req.ValueNumeric != nil {
		threshold := *req.ValueNumeric
		logger.Infof("Порог дедупликации изменён на %.4f, очищаем устаревшие PENDING merge-заявки", threshold)

		if err := s.store.DeleteOutdatedPendingMerges(ctx, threshold); err != nil {
			// Настройка уже сохранена, но cleanup не прошёл — возвращаем ошибку,
			// чтобы клиент знал о частичном сбое и мог повторить операцию.
			logger.Errorf("Ошибка при очистке устаревших PENDING merges (threshold=%.4f): %v", threshold, err)
			return nil, fmt.Errorf("настройка сохранена, но ошибка при очистке устаревших merge-заявок: %w", err)
		}

		logger.Infof("Устаревшие PENDING merge-заявки удалены (threshold=%.4f)", threshold)
	}

	return settingToResponse(setting, s.logger), nil
}

// GetSetting возвращает настройку по ключу.
func (s *SettingsService) GetSetting(ctx context.Context, key string) (*api_models.SystemSettingResponse, error) {
	if strings.TrimSpace(key) == "" {
		return nil, apierrors.NewValidationError("ключ настройки (key) не может быть пустым")
	}

	setting, err := s.store.GetSystemSettingByKey(ctx, key)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apierrors.NewNotFoundError("настройка %q не найдена", key)
		}
		return nil, fmt.Errorf("ошибка получения настройки %q: %w", key, err)
	}

	return settingToResponse(setting, s.logger), nil
}

// ListSettings возвращает все системные настройки.
func (s *SettingsService) ListSettings(ctx context.Context) ([]api_models.SystemSettingResponse, error) {
	settings, err := s.store.ListSystemSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения списка настроек: %w", err)
	}

	result := make([]api_models.SystemSettingResponse, 0, len(settings))
	for _, setting := range settings {
		result = append(result, *settingToResponse(setting, s.logger))
	}

	return result, nil
}

// settingToResponse конвертирует DB-модель в API-ответ.
// logger используется для логирования ошибок парсинга (например, повреждённый value_numeric).
func settingToResponse(s db.SystemSetting, logger logging.Logger) *api_models.SystemSettingResponse {
	resp := &api_models.SystemSettingResponse{
		Key:       s.Key,
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
		UpdatedBy: s.UpdatedBy,
	}

	if s.ValueNumeric.Valid {
		if v, err := strconv.ParseFloat(s.ValueNumeric.String, 64); err == nil {
			resp.ValueNumeric = &v
		} else {
			logger.Errorf("Ошибка парсинга value_numeric для настройки %q: значение=%q, ошибка=%v", s.Key, s.ValueNumeric.String, err)
		}
	}
	if s.ValueString.Valid {
		resp.ValueString = &s.ValueString.String
	}
	if s.ValueBoolean.Valid {
		resp.ValueBoolean = &s.ValueBoolean.Bool
	}
	if s.Description.Valid {
		resp.Description = &s.Description.String
	}

	return resp
}
