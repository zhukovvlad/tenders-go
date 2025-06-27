# ✅ TODO-лист доработок

Этот документ содержит полный список необходимых улучшений для SQL-запросов и схемы базы данных проекта, основанный на проведенном ревью.

---

### 1. Задачи по миграциям (всё можно сделать в одной новой миграции)

Это самые важные доработки, так как они напрямую влияют на производительность. Рекомендуется создать один новый файл миграции (например, `000003_add_performance_indexes.up.sql`) и добавить в него все перечисленное ниже.

* **Включить расширение `pg_trgm`:**
    * **Причина:** Необходимо для быстрого поиска по подстроке (`ILIKE`).
    * **Код:** `CREATE EXTENSION IF NOT EXISTS pg_trgm;`

* **Добавить триграммные индексы для поиска:**
    * **Причина:** Ускорение `Search...` запросов.
    * **Таблица `catalog_positions`:** `CREATE INDEX idx_gin_trgm_catalog_positions_title ON catalog_positions USING GIN (standard_job_title gin_trgm_ops);`
    * **Таблица `contractors`:** `CREATE INDEX idx_gin_trgm_contractors_title ON contractors USING GIN (title gin_trgm_ops);`

* **Добавить индексы для внешних ключей (FK):**
    * **Причина:** Ускорение `List...By...ID` запросов (фильтрации по родительской сущности).
    * **Таблица `persons`:** `CREATE INDEX idx_persons_contractor_id ON persons (contractor_id);`
    * **Таблица `tender_chapters`:** `CREATE INDEX idx_tender_chapters_type_id ON tender_chapters (tender_type_id);`
    * **Таблица `tender_categories`:** `CREATE INDEX idx_tender_categories_chapter_id ON tender_categories (tender_chapter_id);`

* **Добавить специализированные и уникальные индексы:**
    * **Причина:** Ускорение специфичных запросов и обеспечение уникальности бизнес-ключей.
    * **Таблица `proposals`:** `CREATE INDEX idx_proposals_lot_id_is_baseline ON proposals (lot_id, is_baseline);` (для `GetBaselineProposalForLot`)
    * **Таблица `lots_md_documents`:** `CREATE UNIQUE INDEX uq_lot_id_document_name ON lots_md_documents (lot_id, document_name);` (для `UpsertLotsMdDocument`)
    * **Таблица `tenders` (опционально):** `CREATE INDEX idx_tenders_data_prepared_on_date ON tenders (data_prepared_on_date DESC);` (для `ListTenders`)

---

### 2. Задачи по коду и логике приложения (что нужно помнить)

Это не изменения в SQL-файлах, а напоминания для реализации в Go-коде.

* **Обработка ошибок `ON DELETE RESTRICT`:**
    * **Суть:** Ваше приложение должно быть готово к тому, что СУБД вернет ошибку при попытке удалить запись, на которую есть ссылки.
    * **Где это произойдет:**
        * `DeleteTender`: Не сработает, если есть связанные `lots`.
        * `DeleteObject` / `DeleteExecutor`: Не сработает, если есть связанные `tenders`.
        * `DeleteContractor`: Не сработает, если есть связанные `persons`.
        * `DeleteProposal`: Не сработает, если есть связанные `winners`, `position_items` и т.д.
        * `DeleteUnitOfMeasurement`: Не сработает, если есть связанные `position_items`.

* **Осознанное использование `ON DELETE CASCADE` и `ON DELETE SET NULL`:**
    * **Суть:** Вы должны понимать, какие каскадные операции запускает удаление.
    * **Где это произойдет:**
        * `DeleteTenderType`: Удалит всю ветку дочерних разделов и категорий.
        * `DeleteTenderChapter`: Удалит все дочерние категории.
        * `DeleteLot`: Удалит все связанные предложения, документы и чанки.
        * `DeleteTenderCategory`: Не вызовет ошибки, но обнулит `category_id` у связанных тендеров.

* **Контроль за "хрупкими" запросами:**
    * **Суть:** При изменении схемы таблиц с большим количеством колонок нужно проявлять особое внимание.
    * **Где это актуально:**
        * `position_items.UpsertPositionItem`: При добавлении нового поля в таблицу нужно не забыть добавить его и в `INSERT`, и в `DO UPDATE SET`.