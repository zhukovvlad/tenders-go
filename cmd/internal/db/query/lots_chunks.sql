-- lots_chunks.sql
-- Этот файл содержит набор SQL-запросов для работы с чанками документов.
-- Чанки - это небольшие фрагменты текста с их векторными представлениями (эмбеддингами),
-- которые являются основой для семантического поиска в RAG-системе.

-- name: CreateChunk :one
-- Создает новый чанк для документа.
-- Используется для базового добавления, но `UpsertChunk` обычно предпочтительнее.
INSERT INTO lots_chunks (
    lot_document_id,
    chunk_index,
    chunk_text,
    embedding,
    metadata
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: UpsertChunk :one
-- Создает новый чанк или обновляет существующий, если чанк с парой
-- (lot_document_id, chunk_index) уже существует (Upsert).
-- Это основной, идемпотентный способ для конвейера обработки документов.
-- Он гарантирует, что повторная обработка одного и того же документа не создаст дубликаты.
INSERT INTO lots_chunks (
    lot_document_id,
    chunk_index,
    chunk_text,
    embedding,
    metadata
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (lot_document_id, chunk_index) DO UPDATE SET
    chunk_text = EXCLUDED.chunk_text,
    embedding = EXCLUDED.embedding,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetChunkByID :one
-- Получает один чанк по его уникальному внутреннему идентификатору (primary key).
SELECT * FROM lots_chunks
WHERE id = $1;

-- name: ListChunksByDocumentID :many
-- Получает пагинированный список всех чанков для указанного документа.
-- Сортировка по `chunk_index` позволяет восстановить исходный порядок текста.
-- Запрос будет быстрым, так как использует уникальный индекс по (lot_document_id, chunk_index).
SELECT * FROM lots_chunks
WHERE lot_document_id = $1
ORDER BY chunk_index
LIMIT $2
OFFSET $3;

-- name: DeleteAllChunksForDocument :exec
-- Удаляет ВСЕ чанки для указанного документа.
-- Это полезная утилита для полной переиндексации одного документа.
-- Запрос работает быстро, так как использует индекс.
DELETE FROM lots_chunks
WHERE lot_document_id = $1;

-- name: SearchChunksByEmbedding :many
-- ВЫПОЛНЯЕТ СЕМАНТИЧЕСКИЙ ПОИСК.
-- Находит k (limit) наиболее похожих чанков на заданный вектор-запрос (query_embedding).
--
-- КАК ЭТО РАБОТАЕТ:
-- 1. `embedding <=> $1`: Это оператор косинусного расстояния из pgvector.
--    Он вычисляет расстояние между каждым эмбеддингом в таблице и вашим вектором-запросом.
--    Чем меньше расстояние, тем больше семантическая схожесть.
-- 2. `ORDER BY distance`: Сортируем по этому расстоянию, чтобы самые релевантные оказались вверху.
-- 3. `LIMIT $2`: Ограничиваем результат, чтобы получить только `k` самых похожих чанков.
-- Этот запрос будет работать очень быстро благодаря HNSW-индексу, который вы создали.
SELECT
    id,
    lot_document_id,
    chunk_index,
    chunk_text,
    metadata,
    embedding <=> $1 AS distance -- Вычисляем и возвращаем косинусное расстояние
FROM
    lots_chunks
ORDER BY
    distance
LIMIT $2;


/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type UpsertChunkParams struct {
	LotDocumentID int64           `json:"lot_document_id"`
	ChunkIndex    int32           `json:"chunk_index"`
	ChunkText     string          `json:"chunk_text"`
	Embedding     pgvector.Vector `json:"embedding"` // Используется специальный тип из библиотеки pgvector-go
	Metadata      json.RawMessage `json:"metadata"`
}

type SearchChunksByEmbeddingParams struct {
	QueryEmbedding pgvector.Vector `json:"query_embedding"`
	Limit          int32           `json:"limit"`
}

type SearchChunksByEmbeddingRow struct {
	ID            int64           `json:"id"`
	LotDocumentID int64           `json:"lot_document_id"`
	ChunkIndex    int32           `json:"chunk_index"`
	ChunkText     string          `json:"chunk_text"`
	Metadata      json.RawMessage `json:"metadata"`
	Distance      float64         `json:"distance"` // Расстояние возвращается как float
}
*/