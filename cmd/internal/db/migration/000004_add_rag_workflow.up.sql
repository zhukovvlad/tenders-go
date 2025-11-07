-- =====================================================================================
-- ФАЙЛ МИГРАЦИИ ДЛЯ ВНЕДРЕНИЯ RAG-ВОРКФЛОУ ОБРАБОТКИ ПОЗИЦИЙ
-- Версия: 4 (Hardened v2)
--
-- == ЧТО ЭТО И ЗАЧЕМ? (ПОДРОБНОЕ ОБЪЯСНЕНИЕ) ==
--
-- === Общая цель ===
-- Эта миграция внедряет инфраструктуру для **RAG-обработки *позиций каталога***.
-- Ее задача - "понимать" суть самих *работ*, сравнивать их и находить дубликаты.
--
-- *ВАЖНОЕ ПРИМЕЧАНИЕ:* Это *не* тот RAG, который отвечает на вопросы по
-- *содержанию* тендерной документации. Тот RAG-процесс работает
-- с таблицами `lots_md_documents` и `lots_chunks` (миграция 000001).
--
-- === Проблема ===
-- У нас есть тысячи тендерных предложений (как в `1.json`), где подрядчики
-- по-разному называют одну и ту же работу (например, "устройство стяжки", "заливка стяжки").
-- Мы хотим "понимать" суть этих работ, чтобы их можно было сравнивать и анализировать.
--
-- === Решение ===
-- Мы внедряем RAG (Retrieval-Augmented Generation) для *семантического матчинга*
-- и *дедупликации* строк в `catalog_positions`. Эта система "переводит" хаотичные
-- названия работ на язык векторов (эмбеддингов).
--
-- === Архитектура ===
-- Мы разделяем систему на две части для скорости:
-- 1. Go-сервер: Мгновенно "проглатывает" и парсит JSON-файлы.
-- 2. Python-воркер: В фоновом режиме выполняет "тяжелую" ML-работу (создает векторы
--    и ищет дубликаты).
--
-- Эта миграция (файл 000004) создает в базе данных всю необходимую
-- инфраструктуру для такой разделенной работы.
--
-- === Компонент 1: Модификация `catalog_positions` (Добавление поля `kind`) ===
-- Зачем? Исходные данные "грязные". В `catalog_positions.json` и `1.json` мы видим,
-- что вперемешку с реальными работами ("бетонирование свая") приходят заголовки
-- ("тип 7") и "мусор" ("лот 1 set 1...").
--
-- Что это дает? Это поле — *фильтр*. Go-сервер, используя `is_chapter` из JSON,
-- немедленно "маркирует" каждую строку:
--   - `POSITION`: Реальная работа, по которой можно искать.
--   - `HEADER`: Заголовок раздела.
--   - `LOT_HEADER` / `TRASH`: "Мусор".
--   - `TO_REVIEW`: Не удалось определить (требует ручной проверки).
-- *HARDENING:* Мы используем `CHECK`-ограничение, чтобы в `kind` нельзя было
-- записать ошибочное значение (типа "POSITIN").
--
-- === Компонент 2: Новая таблица `matching_cache` ===
-- Зачем? "Идти в RAG" (создавать вектор + искать в БД) для *каждой* строки при
-- *каждой* загрузке — невероятно медленно.
--
-- Что это дает? Это делает систему *мгновенной*.
-- 1. Go-сервер *впервые* видит строку "стяжка м200", не находит ее в кэше
--    и ставит `catalog_position_id = NULL`.
-- 2. Python-воркер в фоне находит эту строку, делает RAG-поиск, понимает,
--    что она должна ссылаться на `ID 42` ("Устройство стяжки М200").
-- 3. Воркер *записывает в кэш*: `hash("стяжка м200")` -> `ID 42`.
-- 4. Когда Go-сервер *в следующий раз* встречает "стяжка м200", он смотрит
--    в `matching_cache`, мгновенно находит `ID 42` и подставляет его.
-- *HARDENING:* Мы добавляем `norm_version` для "безопасной" смены логики
-- нормализации и `expires_at` (TTL) для авто-очистки "тухлого" кэша.
--
-- === Компонент 3: Новая таблица `suggested_merges` ===
-- Зачем? В `catalog_positions.json` есть дубликаты (ID 43 и 55). Находить их
-- вручную невозможно. Доверять слияние роботу опасно.
--
-- Что это дает? Это асинхронный "Human-in-the-Loop" (человек в контуре) процесс:
-- 1. *Python-воркер* (робот) находит похожие записи и создает "задачу"
--    в `suggested_merges`.
-- 2. *Человек-оператор* (через админку Go-сервера) видит эту задачу
--    и нажимает "Слить".
-- 3. *Go-сервер* (исполнитель) выполняет фактическое слияние в БД.
-- *HARDENING:* Мы добавляем поля аудита (`decided_at`, `decided_by`) и
-- `CHECK`-ограничение на `status` для целостности.
--
-- === Компонент 4: Оптимизация Индексов и VIEW ===
-- * HNSW-индекс (из `000002`) заменен на *частичный* (Partial) `WHERE kind = 'POSITION'`.
--   Он меньше, быстрее и точнее для нашей задачи.
-- * Добавлены индексы для `TO_REVIEW` и `suggested_merges` для быстрой работы админки.
-- * Добавлено `VIEW catalog_positions_clean` для упрощения кода в Go/Python.
-- =====================================================================================


-- --- ШАГ 1: Модификация `catalog_positions` ---

-- Добавляем новую колонку "kind" для классификации записей
ALTER TABLE "catalog_positions"
ADD COLUMN "kind" TEXT NOT NULL DEFAULT 'TO_REVIEW'; -- Безопасный DEFAULT

-- Добавляем жесткое CHECK-ограничение для целостности данных
ALTER TABLE "catalog_positions"
ADD CONSTRAINT "ck_catalog_positions_kind"
CHECK ("kind" IN ('POSITION', 'HEADER', 'LOT_HEADER', 'TRASH', 'TO_REVIEW'));

-- Обновляем комментарий для новой колонки
COMMENT ON COLUMN "catalog_positions"."kind"
IS 'Тип записи: POSITION (работа), HEADER (заголовок), LOT_HEADER (заголовок лота), TRASH (мусор), TO_REVIEW (на ручную проверку)';

-- Добавляем индекс для быстрой фильтрации по типу (особенно для 'TO_REVIEW' в админке)
CREATE INDEX IF NOT EXISTS "idx_catalog_positions_kind" ON "catalog_positions" ("kind");

-- Добавляем частичный индекс для быстрой выборки очереди "TO_REVIEW" в админке
CREATE INDEX IF NOT EXISTS "idx_cp_kind_review"
ON "catalog_positions" (id) -- Индексируем ID для выборки
WHERE "kind" = 'TO_REVIEW';


-- --- ШАГ 2: Обновление HNSW-индекса (замена индекса из `000002`) ---

-- Сначала удаляем старый, "полный" HNSW-индекс из миграции 000002
DROP INDEX IF EXISTS "catalog_positions_embedding_idx";

-- Создаем новый, *частичный* HNSW-индекс, который работает ТОЛЬКО с "чистыми" позициями
CREATE INDEX IF NOT EXISTS "idx_cp_kind_pos_hnsw"
ON "catalog_positions" USING HNSW (embedding vector_cosine_ops)
WHERE "kind" = 'POSITION';

COMMENT ON INDEX "idx_cp_kind_pos_hnsw" IS 'Частичный HNSW-индекс ТОЛЬКО для записей kind=POSITION, ускоряет RAG-поиск.';


-- --- ШАГ 3: Создание таблицы `matching_cache` ---

CREATE TABLE "matching_cache" (
  "job_title_hash" TEXT NOT NULL,
  
  -- Версия алгоритма нормализации
  "norm_version" SMALLINT NOT NULL DEFAULT 1,
  
  "job_title_text" TEXT,
  "catalog_position_id" BIGINT NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  
  -- TTL для автоматического "протухания" кэша
  "expires_at" TIMESTAMPTZ,

  -- Ключ теперь составной: хеш + версия
  PRIMARY KEY ("job_title_hash", "norm_version"),
  FOREIGN KEY ("catalog_position_id") REFERENCES "catalog_positions"("id") ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS "idx_matching_cache_catalog_id" ON "matching_cache" ("catalog_position_id");
CREATE INDEX IF NOT EXISTS "idx_matching_cache_expires_at" ON "matching_cache" ("expires_at"); -- Для быстрой очистки TTL

COMMENT ON TABLE "matching_cache" IS 'Кэш (ключ-значение) для связи хеша "грязной" строки (job_title_normalized) с "чистым" catalog_position_id.';
COMMENT ON COLUMN "matching_cache"."norm_version" IS 'Версия алгоритма нормализации/хеширования. При смене алгоритма - версия инкрементируется.';
COMMENT ON COLUMN "matching_cache"."expires_at" IS 'Время жизни (TTL) этой записи кэша.';


-- --- ШАГ 4: Создание таблицы `suggested_merges` ---

CREATE TABLE "suggested_merges" (
  "id" BIGSERIAL PRIMARY KEY,
  "main_position_id" BIGINT NOT NULL,
  "duplicate_position_id" BIGINT NOT NULL,
  "similarity_score" REAL NOT NULL,
  
  -- 'PENDING', 'APPROVED', 'REJECTED'
  "status" TEXT NOT NULL DEFAULT 'PENDING',
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  
  -- Поля аудита
  "decided_at" TIMESTAMPTZ,
  "decided_by" TEXT, -- ID или email оператора

  FOREIGN KEY ("main_position_id") REFERENCES "catalog_positions"("id") ON DELETE CASCADE,
  FOREIGN KEY ("duplicate_position_id") REFERENCES "catalog_positions"("id") ON DELETE CASCADE,
  CONSTRAINT "uq_suggestion" UNIQUE ("main_position_id", "duplicate_position_id")
);

-- Добавляем CHECK-ограничение для статуса
ALTER TABLE "suggested_merges"
ADD CONSTRAINT "ck_suggested_merges_status"
CHECK ("status" IN ('PENDING', 'APPROVED', 'REJECTED'));

-- Добавляем индекс для быстрой выборки очереди 'PENDING' в админке
CREATE INDEX IF NOT EXISTS "idx_suggested_merges_status_created"
ON "suggested_merges" ("status", "created_at" DESC)
WHERE "status" = 'PENDING';

COMMENT ON TABLE "suggested_merges" IS 'Задачи для оператора по слиянию дубликатов в catalog_positions, созданные Python-воркером.';
COMMENT ON COLUMN "suggested_merges"."status" IS 'Статус задачи: PENDING, APPROVED, REJECTED.';
COMMENT ON COLUMN "suggested_merges"."decided_by" IS 'Оператор (или система), принявший решение.';


-- --- ШАГ 5: Создание VIEW для "чистого" поиска ---

CREATE OR REPLACE VIEW "catalog_positions_clean" AS
SELECT *
FROM "catalog_positions"
WHERE "kind" = 'POSITION';

COMMENT ON VIEW "catalog_positions_clean" IS 'Упрощенное представление (VIEW) для RAG-поиска, показывает только "чистые" позиции (kind=POSITION).';