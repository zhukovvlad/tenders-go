-- =====================================================================================
-- Migration 000005: Add system_settings table
--
-- Создает таблицу для хранения глобальных конфигураций системы (System Settings).
-- Позволяет менять параметры (например, порог дедупликации) без перезапуска сервисов.
-- =====================================================================================

CREATE TABLE system_settings (
    key         VARCHAR(50) PRIMARY KEY,
    value_numeric  NUMERIC,             -- Для числовых настроек (float/int)
    value_string   TEXT,                -- Для текстовых настроек
    value_boolean  BOOLEAN,             -- Для флагов (true/false)
    description    TEXT,                -- Описание того, за что отвечает настройка
    created_at     TIMESTAMPTZ NOT NULL DEFAULT (now()),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT (now()),
    updated_by     TEXT,                -- Имя/ID администратора, который последним менял значение

    -- Гарантируем, что ровно одна value-колонка заполнена (не ноль и не больше одной)
    CONSTRAINT "ck_system_settings_has_value"
        CHECK (num_nonnulls(value_numeric, value_string, value_boolean) = 1)
);

-- Триггер автообновления updated_at при любом UPDATE
CREATE OR REPLACE FUNCTION trg_system_settings_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER system_settings_set_updated_at
    BEFORE UPDATE ON system_settings
    FOR EACH ROW
    EXECUTE FUNCTION trg_system_settings_updated_at();

-- =====================================================================================
-- Начальные значения
-- =====================================================================================

-- Порог косинусного расстояния для AI-поиска дубликатов (Python-воркер).
-- По умолчанию 0.15 — чем меньше, тем строже.
INSERT INTO system_settings (key, value_numeric, description, updated_by)
VALUES (
    'dedup_distance_threshold',
    0.15,
    'Порог косинусного расстояния для AI-поиска дубликатов (меньше = строже)',
    'system'
);
