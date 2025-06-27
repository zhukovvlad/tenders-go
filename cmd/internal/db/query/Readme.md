# SQLC Запросы для Проекта "Tenders Go"

Этот документ описывает SQL-запросы, определенные для использования с `sqlc` в проекте "Tenders Go". `sqlc` генерирует типобезопасный Go-код на основе этих SQL-запросов, что значительно упрощает и обезопасивает взаимодействие с базой данных PostgreSQL.

## Организация Файлов

SQL-запросы для каждой таблицы или логической группы находятся в отдельных `.sql` файлах в директории `db/query/`. Файлы со схемой базы данных (миграции `CREATE TABLE ...`) находятся в директории `db/migration/`.

## Общие Паттерны и Замечания

* **`:one`**: Ожидается, что запрос вернет ровно одну строку. Если строк нет, будет возвращена ошибка `sql.ErrNoRows`.
* **`:many`**: Ожидается, что запрос вернет ноль или более строк (слайс).
* **`:exec`**: Запрос выполняется, но не возвращает строк (например, `DELETE` без `RETURNING`).
* **`RETURNING *`**: Часто используется в `INSERT` и `UPDATE` запросах для возврата данных созданной или обновленной строки.
* **`ON CONFLICT (...) DO UPDATE SET ...`**: Этот "Upsert" паттерн используется для атомарного создания или обновления записи по ее бизнес-ключу.
* **Паттерн частичного обновления**: Для `UPDATE`-запросов мы используем конструкцию `field = COALESCE(sqlc.narg(field), field)`. Это позволяет обновлять только те поля, для которых в Go-коде передано не-`nil` значение, что делает API очень гибким.
* **Пагинация**: Все `List*` запросы, возвращающие списки, должны использовать пагинацию с помощью `LIMIT` и `OFFSET` для обеспечения безопасности и предсказуемой производительности.

---

## Запросы по Таблицам

### Иерархия справочников (`tender_types`, `tender_chapters`, `tender_categories`)

Эти таблицы образуют трехуровневую иерархию.

#### Таблица: `tender_types`
*(Файл: `tender_types.sql`)*

* `-- name: CreateTenderType :one`: Создает новый тип тендера.
* `-- name: GetTenderTypeByID :one`: Получает тип по `id`.
* `-- name: GetTenderTypeByTitle :one`: Получает тип по уникальному `title`.
* `-- name: UpsertTenderType :one`: "Найти или создать" тип по `title`.
* `-- name: ListTenderTypes :many`: Пагинированный список всех типов.
* `-- name: UpdateTenderType :one`: Обновляет `title` типа по `id`.
* `-- name: DeleteTenderType :exec`: Удаляет тип по `id`.
    * **Логика удаления**: `ON DELETE CASCADE`. Удаление типа приведет к **каскадному удалению** всех связанных с ним разделов (`tender_chapters`) и, соответственно, всех их категорий (`tender_categories`).

#### Таблица: `tender_chapters`
*(Файл: `tender_chapters.sql`)*

* `-- name: CreateTenderChapter :one`: Создает новый раздел, привязанный к типу.
* ... (Get/Upsert/List/Update) ...
* `-- name: ListTenderChaptersByType :many`: Пагинированный список разделов для указанного `tender_type_id`.
    * **Производительность**: Требует создания индекса `ON tender_chapters (tender_type_id)` для быстрой работы.
* `-- name: DeleteTenderChapter :exec`: Удаляет раздел по `id`.
    * **Логика удаления**: `ON DELETE CASCADE`. Удаление раздела приведет к **каскадному удалению** всех связанных с ним категорий (`tender_categories`).

#### Таблица: `tender_categories`
*(Файл: `tender_categories.sql`)*

* `-- name: CreateTenderCategory :one`: Создает новую категорию, привязанную к разделу.
* ... (Get/Upsert/List/Update) ...
* `-- name: ListTenderCategoriesByChapter :many`: Пагинированный список категорий для указанного `tender_chapter_id`.
    * **Производительность**: Требует создания индекса `ON tender_categories (tender_chapter_id)` для быстрой работы.
* `-- name: DeleteTenderCategory :exec`: Удаляет категорию по `id`.
    * **Логика удаления**: `ON DELETE SET NULL`. Удаление **будет успешным**. У всех тендеров, ссылавшихся на эту категорию, поле `category_id` станет `NULL`.

---

### Основные сущности

#### Таблица: `tenders`
*(Файл: `tenders.sql`)*

* `-- name: UpsertTender :one`: Создает/обновляет тендер по уникальному `etp_id`.
* `-- name: UpdateTenderDetails :one`: **Частично обновляет** детали тендера по `id` (использует `COALESCE`).
* `-- name: ListTenders :many`: Возвращает обогащенный пагинированный список тендеров с `JOIN` и подсчетом предложений.
    * **Производительность**: Сортировка по `data_prepared_on_date` может быть медленной. Требует индекса при больших объемах.
* `-- name: GetTenderDetails :one`: Возвращает полную информацию о тендере с `LEFT JOIN` по всей иерархии справочников.
* `-- name: DeleteTender :exec`: Удаляет тендер по `id`.
    * **Логика удаления**: `ON DELETE RESTRICT`. Запрос **не сработает**, если у тендера есть хотя бы один лот (`lots`).

#### Таблица: `lots`
*(Файл: `lots.sql`)*

* `-- name: UpsertLot :one`: Создает/обновляет лот по уникальной паре (`tender_id`, `lot_key`).
* `-- name: UpdateLotDetails :one`: **Частично обновляет** детали лота по `id` (использует `COALESCE`).
* `-- name: ListLotsByTenderID :many`: Пагинированный список лотов для тендера.
* `-- name: DeleteLot :exec`: Удаляет лот по `id`.
    * **Логика удаления**: `ON DELETE CASCADE`. Удаление лота приведет к **каскадному удалению** всех связанных `proposals`, `lots_md_documents` и `lots_chunks`.

#### Таблицы: `objects`, `executors`
*(Файлы: `objects.sql`, `executors.sql`)*

* ... (стандартные Create/Get/List) ...
* `-- name: UpdateObject :one`, `-- name: UpdateExecutor :one`: **Частично обновляют** запись по `id` (используют `COALESCE`).
* `-- name: DeleteObject :exec`, `-- name: DeleteExecutor :exec`: Удаляют запись по `id`.
    * **Логика удаления**: `ON DELETE RESTRICT`. Запросы **не сработают**, если на запись ссылается хотя бы один тендер.

---

### Сущности подрядчиков и предложений

#### Таблица: `contractors`
*(Файл: `contractors.sql`)*

* ... (стандартные Create/Get/List) ...
* `-- name: UpdateContractor :one`: **Частично обновляет** запись по `id` (использует `COALESCE`).
* `-- name: SearchContractorsByTitle :many`: Поиск по `ILIKE`.
    * **Производительность**: Требует `pg_trgm` индекса для быстрой работы.
* `-- name: DeleteContractor :exec`: Удаляет подрядчика по `id`.
    * **Логика удаления**: `ON DELETE RESTRICT`. Запрос **не сработает**, если у подрядчика есть контактные лица (`persons`).

#### Таблица: `persons`
*(Файл: `persons.sql`)*

* ... (стандартные Create/Get/List) ...
* `-- name: ListPersonsByContractor :many`: Список лиц для подрядчика.
    * **Производительность**: Требует создания индекса `ON persons (contractor_id)` для быстрой работы.
* `-- name: UpdatePerson :one`: **Частично обновляет** запись по `id` (использует `COALESCE`).
* `-- name: DeletePerson :exec`: Удаляет контактное лицо по `id`. Простое удаление без побочных эффектов.

#### Таблица: `proposals`
*(Файл: `proposals.sql`)*

* `-- name: UpsertProposal :one`: Создает/обновляет предложение по (`lot_id`, `contractor_id`).
* `-- name: GetBaselineProposalForLot :one`: Получает базовое предложение для лота.
    * **Производительность**: Требует индекса `ON proposals (lot_id, is_baseline)` для быстрой работы.
* `-- name: UpdateProposalDetails :one`: **Частично обновляет** запись по `id` (использует `COALESCE`).
* `-- name: ListRichProposalsForLot :many`, `-- name: ListProposalsForTender :many`: Сложные аналитические запросы с пагинацией.
    * **Производительность**: Сортировка по вычисляемым полям может быть медленной.
* `-- name: DeleteProposal :exec`: Удаляет предложение по `id`.
    * **Логика удаления**: `ON DELETE RESTRICT`. Запрос **не сработает**, если на предложение есть ссылки из `winners`, `position_items` и т.д.

#### Таблица: `winners`
*(Файл: `winners.sql`)*

* `-- name: UpsertWinner :one`: Помечает предложение как выигрышное или обновляет детали по `proposal_id`.
* `-- name: UpdateWinnerDetails :one`: **Частично обновляет** запись по `id` (использует `COALESCE`).
* `-- name: ListWinnersForLot :many`: Пагинированный список победителей для лота.
* `-- name: DeleteWinner :exec`: "Отменяет" победу, удаляя запись по `proposal_id`.

---

### Детализация предложений и RAG

#### Таблицы: `position_items`, `proposal_summary_lines`, `proposal_additional_info`, `units_of_measurement`
*(Файлы: `...sql`)*

* Эти таблицы в основном используют `Upsert...` по (`proposal_id`, `key`) и `DeleteAll...ForProposal` для массовой загрузки/обновления данных.
* Все `List...` запросы пагинированы для безопасности.
* `Update...` запросы используют `COALESCE` для гибкости.
* `position_items.UpsertPositionItem` очень большой и требует особого внимания при изменении схемы.

#### Таблицы RAG: `lots_md_documents`, `lots_chunks`
*(Файлы: `...sql`)*

* `-- name: UpsertLotsMdDocument :one`: Создает/обновляет исходный документ по (`lot_id`, `document_name`).
    * **Производительность**: Требует создания уникального индекса `ON lots_md_documents (lot_id, document_name)`.
* `-- name: UpsertChunk :one`: Создает/обновляет чанк по (`lot_document_id`, `chunk_index`).
* `-- name: SearchChunksByEmbedding :many`: **Ключевой запрос**. Выполняет семантический поиск по векторам, используя оператор `<=>` и HNSW-индекс.