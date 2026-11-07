package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// --- Постоянная векторная память ---
type MemoryItem struct {
	ID     string    `json:"id"`
	Text   string    `json:"text"`
	Vector []float64 `json:"vector"`
	Type   string    `json:"type"` // "dialog" или "knowledge"
}

var db *sql.DB

// --- Функции работы с SQLite ---
func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия БД: %w", err)
	}

	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		fmt.Println("Предупреждение: не удалось включить WAL:", err)
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS memory (
		id TEXT PRIMARY KEY,
		text TEXT NOT NULL,
		vector BLOB,
		type TEXT NOT NULL
	);
	CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(text);
	`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания таблиц: %w", err)
	}

	return db, nil
}

func addMemoryItem(db *sql.DB, item MemoryItem) error {
	vectorBlob, err := vectorToBlob(item.Vector)
	if err != nil {
		return fmt.Errorf("ошибка преобразования вектора: %w", err)
	}

	_, err = db.Exec(
		"INSERT OR REPLACE INTO memory (id, text, vector, type) VALUES (?, ?, ?, ?)",
		item.ID, item.Text, vectorBlob, item.Type,
	)
	if err != nil {
		return fmt.Errorf("ошибка вставки записи: %w", err)
	}

	_, err = db.Exec(
		"INSERT OR REPLACE INTO memory_fts (rowid, text) VALUES ((SELECT rowid FROM memory WHERE id = ?), ?)",
		item.ID, item.Text,
	)
	if err != nil {
		fmt.Printf("Предупреждение: не удалось обновить FTS-индекс для %s: %v\n", item.ID, err)
	}
	return nil
}

// func searchMemorySQLite(db *sql.DB, query string, topN int) ([]string, error) {
// 	words := strings.Fields(query)
// 	if len(words) == 0 {
// 		return []string{}, nil
// 	}
// 	matchQuery := strings.Join(words, " ")

// 	rows, err := db.Query(`
// 		SELECT m.text FROM memory m
// 		JOIN memory_fts fts ON m.rowid = fts.rowid
// 		WHERE memory_fts MATCH ?
// 		ORDER BY rank
// 		LIMIT ?`, matchQuery, topN)
// 	if err != nil {
// 		return nil, fmt.Errorf("ошибка поиска: %w", err)
// 	}
// 	defer rows.Close()

// 	var results []string
// 	for rows.Next() {
// 		var text string
// 		err := rows.Scan(&text)
// 		if err != nil {
// 			continue
// 		}
// 		results = append(results, text)
// 	}
// 	if err = rows.Err(); err != nil {
// 		return nil, fmt.Errorf("ошибка при итерации результатов: %w", err)
// 	}
// 	return results, nil
// }

func removeMemoryItem(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM memory WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("ошибка удаления из memory: %w", err)
	}
	_, err = db.Exec("DELETE FROM memory_fts WHERE rowid = (SELECT rowid FROM memory WHERE id = ?)", id)
	if err != nil {
		fmt.Printf("Предупреждение: не удалось удалить из FTS-индекса для %s: %v\n", id, err)
	}
	return nil
}

func countMemoryItems(db *sql.DB) int {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM memory").Scan(&count)
	if err != nil {
		fmt.Printf("Ошибка подсчёта записей: %v\n", err)
		return 0
	}
	return count
}

func vectorToBlob(vec []float64) ([]byte, error) {
	if len(vec) == 0 {
		return nil, nil
	}
	buf := make([]byte, len(vec)*8)
	for i, v := range vec {
		binary.LittleEndian.PutUint64(buf[i*8:], uint64(v))
	}
	return buf, nil
}

func blobToVector(blob []byte) ([]float64, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	if len(blob)%8 != 0 {
		return nil, fmt.Errorf("неправильная длина BLOB: %d, должна быть кратна 8", len(blob))
	}
	vec := make([]float64, len(blob)/8)
	for i := 0; i < len(vec); i++ {
		vec[i] = float64(binary.LittleEndian.Uint64(blob[i*8:]))
	}
	return vec, nil
}

// cosineSimilarity вычисляет косинусное сходство между двумя векторами.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dotProduct / (float64(normA) * float64(normB))
}

// searchByVector использует косинусное сходство между вектором запроса и векторами в памяти.
func searchByVector(db *sql.DB, query string, topN int) ([]string, error) {
	queryVec, err := getEmbedding(query)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения эмбеддинга запроса: %w", err)
	}

	rows, err := db.Query("SELECT text, vector FROM memory")
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса к БД: %w", err)
	}
	defer rows.Close()

	type scoredText struct {
		text  string
		score float64
	}
	var results []scoredText

	for rows.Next() {
		var text string
		var vectorBlob []byte
		if err := rows.Scan(&text, &vectorBlob); err != nil {
			continue
		}
		if len(vectorBlob) == 0 {
			continue
		}
		vec, err := blobToVector(vectorBlob)
		if err != nil {
			continue
		}
		score := cosineSimilarity(queryVec, vec)
		results = append(results, scoredText{text: text, score: score})
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > topN {
		results = results[:topN]
	}

	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.text
	}
	return out, nil
}

// --- Функции памяти ---
// func loadMemory() error {
// 	return nil
// }

func addToMemory(text string, memType string) error {
	vec, err := getEmbedding(text)
	if err != nil {
		return err
	}
	item := MemoryItem{
		ID:     fmt.Sprintf("mem_%d", time.Now().UnixNano()),
		Text:   text,
		Vector: vec,
		Type:   memType,
	}
	return addMemoryItem(db, item)
}

// func addToMemoryWithVector(text string, memType string, vec []float64) error {
// 	item := MemoryItem{
// 		ID:     fmt.Sprintf("mem_%d", time.Now().UnixNano()),
// 		Text:   text,
// 		Vector: vec,
// 		Type:   memType,
// 	}
// 	return addMemoryItem(db, item)
// }

// func removeChunkByID(id string) {
// 	err := removeMemoryItem(db, id)
// 	if err != nil {
// 		printError("Ошибка удаления записи " + id + ": " + err.Error())
// 	}
// }

func searchMemory(query string, topN int) ([]string, error) {
	return searchByVector(db, query, topN)
}
