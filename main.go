package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
	"golang.org/x/net/html"
	_ "modernc.org/sqlite"
)

// --- Цвета ---
const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorGray  = "\033[90m"
	colorGreen = "\033[32m"
	colorCyan  = "\033[36m"
)

func printSystem(msg string) {
	fmt.Println(string(colorGray) + msg + string(colorReset))
}

func printError(msg string) {
	fmt.Println(string(colorRed) + msg + string(colorReset))
}

func printAction(msg string) {
	fmt.Println(string(colorCyan) + "[ACTION] " + msg + string(colorReset))
}

func printSuccess(msg string) {
	fmt.Println(string(colorGreen) + "[OK] " + msg + string(colorReset))
}

// --- Глобальные переменные ---
var lastFilePath string

// --- Эмбеддинги через Ollama ---
func getEmbedding(text string) ([]float64, error) {
	url := "http://localhost:11434/api/embeddings"
	payload := map[string]interface{}{
		"model":  "nomic-embed-text",
		"prompt": text,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	embeddingRaw, ok := result["embedding"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("embedding не найден")
	}

	vec := make([]float64, len(embeddingRaw))
	for i, v := range embeddingRaw {
		val, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("неверный тип в эмбеддинге")
		}
		vec[i] = val
	}
	return vec, nil
}

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

func searchMemorySQLite(db *sql.DB, query string, topN int) ([]string, error) {
	words := strings.Fields(query)
	if len(words) == 0 {
		return []string{}, nil
	}
	matchQuery := strings.Join(words, " ")

	rows, err := db.Query(`
		SELECT m.text FROM memory m
		JOIN memory_fts fts ON m.rowid = fts.rowid
		WHERE memory_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, matchQuery, topN)
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска: %w", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var text string
		err := rows.Scan(&text)
		if err != nil {
			continue
		}
		results = append(results, text)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации результатов: %w", err)
	}
	return results, nil
}

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

// --- Функции памяти (используют SQLite) ---
func loadMemory() error {
	return nil
}

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

func addToMemoryWithVector(text string, memType string, vec []float64) error {
	item := MemoryItem{
		ID:     fmt.Sprintf("mem_%d", time.Now().UnixNano()),
		Text:   text,
		Vector: vec,
		Type:   memType,
	}
	return addMemoryItem(db, item)
}

func removeChunkByID(id string) {
	err := removeMemoryItem(db, id)
	if err != nil {
		printError("Ошибка удаления записи " + id + ": " + err.Error())
	}
}

func searchMemory(query string, topN int) ([]string, error) {
	return searchMemorySQLite(db, query, topN)
}

// --- База знаний с инкрементальным обновлением ---
type FileState struct {
	Path       string      `json:"path"`
	Hash       string      `json:"hash"`
	ChunkIDs   []string    `json:"chunk_ids"`
	Embeddings [][]float64 `json:"embeddings"`
}

type KnowledgeState struct {
	Files map[string]FileState `json:"files"`
}

const stateFile = "knowledge_state.json"

func loadKnowledgeState() (*KnowledgeState, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &KnowledgeState{Files: make(map[string]FileState)}, nil
		}
		return nil, err
	}
	var state KnowledgeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func saveKnowledgeState(state *KnowledgeState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile, data, 0644)
}

func computeFileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", md5.Sum(data)), nil
}

func isSupportedExt(ext string) bool {
	supported := map[string]bool{
		".txt": true, ".md": true, ".go": true, ".json": true,
		".pdf": true, ".docx": true, ".csv": true, ".html": true, ".htm": true,
	}
	return supported[ext]
}

func readFileContent(path, ext string) (string, error) {
	switch ext {
	case ".txt", ".md", ".go", ".json":
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case ".pdf":
		return readPDF(path)
	case ".docx":
		return readDOCX(path)
	case ".csv":
		return readCSV(path)
	case ".html", ".htm":
		return readHTML(path)
	default:
		return "", nil
	}
}

func loadKnowledgeFromFolder(folderPath string) error {
	state, err := loadKnowledgeState()
	if err != nil {
		return err
	}

	currentFiles := make(map[string]bool)
	var addedCount int

	err = filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !isSupportedExt(ext) {
			return nil
		}
		currentFiles[path] = true

		hash, err := computeFileHash(path)
		if err != nil {
			printError("Ошибка хеширования " + path + ": " + err.Error())
			return nil
		}

		content, readErr := readFileContent(path, ext)
		if readErr != nil {
			printError("Ошибка чтения " + path + ": " + readErr.Error())
			return nil
		}
		if content == "" {
			return nil
		}

		chunks := splitIntoChunks(content, 800)
		oldState, exists := state.Files[path]

		if exists && oldState.Hash == hash && len(oldState.Embeddings) == len(chunks) {
			for i, chunk := range chunks {
				text := "📄 Файл: " + filepath.Base(path) + "\n" + chunk
				item := MemoryItem{
					ID:     oldState.ChunkIDs[i],
					Text:   text,
					Vector: oldState.Embeddings[i],
					Type:   "knowledge",
				}
				err := addMemoryItem(db, item)
				if err != nil {
					printError("Ошибка добавления кэшированной записи: " + err.Error())
				}
			}
			state.Files[path] = oldState
			return nil
		}

		embeddings := make([][]float64, len(chunks))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)

		for i, chunk := range chunks {
			wg.Add(1)
			go func(idx int, text string) {
				defer wg.Done()
				sem <- struct{}{}
				vec, err := getEmbedding(text)
				if err != nil {
					printError("Ошибка эмбеддинга для чанка: " + err.Error())
				}
				embeddings[idx] = vec
				<-sem
			}(i, chunk)
		}
		wg.Wait()

		for _, vec := range embeddings {
			if vec == nil {
				return fmt.Errorf("не удалось получить эмбеддинги для файла %s", path)
			}
		}

		var chunkIDs []string
		for i, chunk := range chunks {
			text := "📄 Файл: " + filepath.Base(path) + "\n" + chunk
			item := MemoryItem{
				ID:     fmt.Sprintf("mem_%d", time.Now().UnixNano()+int64(i)),
				Text:   text,
				Vector: embeddings[i],
				Type:   "knowledge",
			}
			err := addMemoryItem(db, item)
			if err != nil {
				printError("Ошибка добавления в память: " + err.Error())
				continue
			}
			chunkIDs = append(chunkIDs, item.ID)
			addedCount++
		}

		if exists {
			for _, id := range oldState.ChunkIDs {
				removeMemoryItem(db, id)
			}
		}

		state.Files[path] = FileState{
			Path:       path,
			Hash:       hash,
			ChunkIDs:   chunkIDs,
			Embeddings: embeddings,
		}
		return nil
	})
	if err != nil {
		return err
	}

	for path := range state.Files {
		if !currentFiles[path] {
			for _, id := range state.Files[path].ChunkIDs {
				removeMemoryItem(db, id)
			}
			delete(state.Files, path)
		}
	}

	if err := saveKnowledgeState(state); err != nil {
		return err
	}

	printSystem(fmt.Sprintf("Добавлено %d новых фрагментов, всего в памяти %d", addedCount, countMemoryItems(db)))
	return nil
}

// --- Вспомогательные функции для чтения ---

func readPDF(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fileInfo, err := f.Stat()
	if err != nil {
		return "", err
	}
	r, err := pdf.NewReader(f, fileInfo.Size())
	if err != nil {
		return "", err
	}
	var text string
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		t, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		text += t + "\n"
	}
	return text, nil
}

func readDOCX(path string) (string, error) {
	doc, err := docx.ReadDocxFile(path)
	if err != nil {
		return "", err
	}
	defer doc.Close()
	return doc.Editable().GetContent(), nil
}

func readCSV(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, row := range records {
		for i, col := range row {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(col)
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func readHTML(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	doc, err := html.Parse(f)
	if err != nil {
		return "", err
	}
	var extract func(*html.Node)
	var text strings.Builder
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)
	return text.String(), nil
}

// --- Получение текущего времени ---
func getCurrentTime() string {
	now := time.Now()
	return now.Format("15:04:05 - 02.01.2006")
}

// --- Интернет-поиск через DuckDuckGo ---
func webSearch(query string) (string, error) {
	query = strings.ReplaceAll(query, " ", "+")
	url := "https://html.duckduckgo.com/html/?q=" + query

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.8,en-US;q=0.5,en;q=0.3")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка запроса: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 202 {
		printSystem("Сервер обрабатывает запрос, повтор через 1 сек...")
		time.Sleep(1 * time.Second)
		return webSearch(query)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга: %v", err)
	}

	var results []string
	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if i >= 5 {
			return
		}
		title := s.Find(".result__title").Text()
		link, _ := s.Find(".result__title a").Attr("href")
		desc := s.Find(".result__snippet").Text()
		if title != "" {
			results = append(results, fmt.Sprintf("%d. %s\n   %s\n   %s", i+1, title, desc, link))
		}
	})

	if len(results) == 0 {
		printSystem("Ничего не найдено.")
		return "", fmt.Errorf("ничего не найдено по запросу")
	}

	printSystem(fmt.Sprintf("Найдено %d результатов.", len(results)))
	return strings.Join(results, "\n\n"), nil
}

// --- Извлечение числа из текста (для курсов, цен и т.п.) ---
func extractNumberImproved(text string) string {
	re := regexp.MustCompile(`\d+[.,]\d+`)
	matches := re.FindAllString(text, -1)
	for _, m := range matches {
		s := strings.Replace(m, ",", ".", 1)
		val, err := strconv.ParseFloat(s, 64)
		if err == nil && val > 50 && val < 150 {
			return m
		}
	}
	reInt := regexp.MustCompile(`\d+`)
	matchesInt := reInt.FindAllString(text, -1)
	for _, m := range matchesInt {
		val, err := strconv.Atoi(m)
		if err == nil && val > 50 && val < 150 {
			return m
		}
	}
	return ""
}

func splitIntoChunks(text string, chunkSize int) []string {
	var chunks []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

// --- Агентные инструменты ---
func isSafeToWrite(path string) bool {
	safeExts := map[string]bool{
		".txt": true, ".md": true, ".go": true, ".json": true,
		".yaml": true, ".toml": true, ".csv": true, ".html": true,
		".css": true, ".js": true, ".py": true, ".xml": true,
	}
	ext := strings.ToLower(filepath.Ext(path))
	return safeExts[ext]
}

func executeAction(action string, params map[string]interface{}) (string, error) {
	switch action {
	case "read_file":
		path, ok := params["path"].(string)
		if !ok || path == "" {
			return "", fmt.Errorf("путь не указан")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		lastFilePath = path
		return string(data), nil

	case "write_file":
		path, ok := params["path"].(string)
		if !ok || path == "" {
			return "", fmt.Errorf("путь не указан")
		}
		if !isSafeToWrite(path) {
			return "", fmt.Errorf("запись в файлы с таким расширением запрещена для безопасности")
		}
		content, ok := params["content"].(string)
		if !ok {
			return "", fmt.Errorf("содержимое не указано")
		}
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			return "", err
		}
		lastFilePath = path
		return "Файл перезаписан", nil

	case "append_file":
		path, ok := params["path"].(string)
		if !ok || path == "" {
			return "", fmt.Errorf("путь не указан")
		}
		if !isSafeToWrite(path) {
			return "", fmt.Errorf("запись в файлы с таким расширением запрещена для безопасности")
		}
		content, ok := params["content"].(string)
		if !ok {
			return "", fmt.Errorf("содержимое не указано")
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return "", err
		}
		lastFilePath = path
		return "Текст добавлен в конец файла", nil

	case "open_file":
		path, ok := params["path"].(string)
		if !ok || path == "" {
			return "", fmt.Errorf("путь не указан. Пожалуйста, укажите путь к файлу или папке.")
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("файл или папка не найдены: %v", err)
		}
		if info.IsDir() {
			cmd := exec.Command("explorer", path)
			if err := cmd.Start(); err != nil {
				return "", fmt.Errorf("ошибка открытия папки: %v", err)
			}
			lastFilePath = path
			return "Папка открыта в проводнике", nil
		} else {
			cmd := exec.Command("cmd", "/c", "start", "", path)
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("ошибка открытия файла: %v", err)
			}
			lastFilePath = path
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".exe" || ext == ".bat" || ext == ".cmd" || ext == ".msi" {
				return "Программа запущена", nil
			}
			return "Файл открыт в стандартном приложении", nil
		}

	case "search_web":
		query, ok := params["query"].(string)
		if !ok || query == "" {
			return "", fmt.Errorf("запрос не указан")
		}
		return webSearch(query)

	case "get_time":
		return getCurrentTime(), nil

	default:
		return "", fmt.Errorf("неизвестное действие: %s", action)
	}
}

// --- Основной клиент ---
func main() {
	var err error
	db, err = initDB("./nexus_memory.db")
	if err != nil {
		printError("Ошибка инициализации БД: " + err.Error())
		os.Exit(1)
	}
	defer db.Close()

	printSystem("Загружаю память...")
	totalItems := countMemoryItems(db)
	printSystem(fmt.Sprintf("Загружено %d записей", totalItems))
	fmt.Println()

	printSystem("Загружаю базу знаний...")
	if err := loadKnowledgeFromFolder("C:\\Users\\maksi\\AI\\base"); err != nil {
		printError("Ошибка загрузки базы знаний: " + err.Error())
	} else {
		printSystem("База знаний загружена")
	}
	fmt.Println()

	printSystem("Проверяю эмбеддер через Ollama API...")
	vec, err := getEmbedding("Привет, я Максим.")
	if err != nil {
		printError("Ошибка эмбеддинга: " + err.Error())
	} else {
		printSystem(fmt.Sprintf("Размерность вектора: %d", len(vec)))
	}
	fmt.Println()

	url := "http://localhost:8080/v1/chat/completions"
	scanner := bufio.NewScanner(os.Stdin)

	systemContent := `Ты — Nexus, AI-агент Максима. Ты помогаешь ему с проектированием, кодом, анализом идей. Ты отвечаешь на русском языке.

Ты умеешь делать следующее:
- Читать файлы (read_file)
- Перезаписывать файлы (write_file)
- Добавлять текст в конец файла (append_file)
- Открывать файлы и папки (open_file)
- Искать в интернете (search_web)
- Показывать текущее время (get_time)

Если пользователь спрашивает о твоих способностях — отвечай честно, не отрицай свои возможности. Если запрос требует действия, ты возвращаешь JSON с полем "action" и "params". Если это обычный диалог, возвращаешь JSON с полем "message".

Доступные действия:
- read_file: прочитать файл. Параметр: "path" — путь к файлу.
- write_file: перезаписать файл. Параметр: "path" и "content".
- append_file: добавить текст в конец файла. Параметр: "path" и "content".
- open_file: открыть файл или папку. Параметр: "path" — полный путь.
- search_web: поиск в интернете. Параметр: "query" — что искать.
- get_time: показать текущее время (без параметров).

Примеры:
Запрос: "Прочитай файл Create.txt"
Ответ: {"action": "read_file", "params": {"path": "Create.txt"}}

Запрос: "Перезапиши файл test.txt новым текстом"
Ответ: {"action": "write_file", "params": {"path": "test.txt", "content": "новый текст"}}

Запрос: "Добавь в файл test.txt ещё строку"
Ответ: {"action": "append_file", "params": {"path": "test.txt", "content": "ещё строка"}}

Запрос: "Открой папку C:\Users"
Ответ: {"action": "open_file", "params": {"path": "C:\\Users"}}

Запрос: "Открой файл C:\Users\maksi\AI\test.txt"
Ответ: {"action": "open_file", "params": {"path": "C:\\Users\\maksi\\AI\\test.txt"}}

Запрос: "Который час?"
Ответ: {"action": "get_time", "params": {}}

Запрос: "Привет, как дела?"
Ответ: {"message": "Привет! У меня всё хорошо, чем могу помочь?"}

Ты должен отвечать ТОЛЬКО в формате JSON. Без лишнего текста.`

	messages := []map[string]string{
		{"role": "system", "content": systemContent},
	}

	printSystem("Nexus готов к общению. Напиши 'exit' для выхода.")

	for {
		fmt.Print("Ты: ")
		if !scanner.Scan() {
			break
		}
		userInput := scanner.Text()

		if userInput == "exit" {
			break
		}

		// --- Команда !последний (открыть последний файл/папку) ---
		if strings.HasPrefix(userInput, "!последний") {
			if lastFilePath == "" {
				fmt.Println("Nexus: Нет последнего открытого файла или папки.")
				continue
			}
			printAction("Открытие последнего файла/папки: " + lastFilePath)
			params := map[string]interface{}{"path": lastFilePath}
			result, err := executeAction("open_file", params)
			if err != nil {
				printError("Ошибка: " + err.Error())
				continue
			}
			printSuccess("Готово")
			fmt.Println("Nexus: " + result)
			continue
		}

		// ----- СТАРЫЕ КОМАНДЫ (совместимость) -----
		if strings.HasPrefix(userInput, "!поиск") {
			query := strings.TrimPrefix(userInput, "!поиск")
			query = strings.TrimSpace(query)
			if query == "" {
				fmt.Println("Nexus: Введите, что искать.")
				continue
			}
			printAction("Поиск в интернете (команда): " + query)
			result, err := webSearch(query)
			if err != nil {
				printError("Ошибка поиска: " + err.Error())
				continue
			}
			number := extractNumberImproved(result)
			if number != "" {
				fmt.Println("Nexus: Найдено число (возможно, курс): " + number)
			}
			fmt.Println("Nexus: " + result)
			continue
		}

		// ----- АГЕНТНЫЙ ЦИКЛ (с повторными попытками при ошибке JSON) -----
		var agentResp struct {
			Action  string                 `json:"action"`
			Params  map[string]interface{} `json:"params"`
			Message string                 `json:"message"`
		}
		var content string
		success := false

		for attempt := 0; attempt < 2; attempt++ {
			messages = append(messages, map[string]string{
				"role":    "user",
				"content": userInput,
			})

			payload := map[string]interface{}{
				"model":          "qwen2.5-3b-instruct-q4_k_m.gguf",
				"messages":       messages,
				"stream":         false,
				"temperature":    0.2,
				"top_p":          0.9,
				"repeat_penalty": 1.1,
			}
			jsonData, err := json.Marshal(payload)
			if err != nil {
				printError("Ошибка маршалинга: " + err.Error())
				break
			}

			resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
			if err != nil {
				printError("Ошибка отправки запроса: " + err.Error())
				break
			}
			var responseData map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
				printError("Ошибка чтения ответа: " + err.Error())
				resp.Body.Close()
				break
			}
			resp.Body.Close()

			choices, ok := responseData["choices"].([]interface{})
			if !ok || len(choices) == 0 {
				printError("Нет ответа от модели")
				break
			}
			msg, ok := choices[0].(map[string]interface{})["message"].(map[string]interface{})
			if !ok {
				printError("Неверный формат ответа")
				break
			}
			content, ok = msg["content"].(string)
			if !ok {
				printError("Нет содержимого в ответе")
				break
			}
			content = strings.TrimSpace(content)

			err = json.Unmarshal([]byte(content), &agentResp)
			if err != nil {
				if attempt == 0 {
					re := regexp.MustCompile(`\{[^}]*\}`)
					matches := re.FindAllString(content, -1)
					if len(matches) > 0 {
						jsonCandidate := matches[0]
						err = json.Unmarshal([]byte(jsonCandidate), &agentResp)
						if err == nil {
							success = true
							break
						}
					}
					fmt.Println("Nexus: " + content)
					messages = append(messages, map[string]string{
						"role":    "assistant",
						"content": content,
					})
					_ = addToMemory("Пользователь: "+userInput, "dialog")
					_ = addToMemory("Nexus: "+content, "dialog")
					break
				} else {
					printError("Ошибка парсинга JSON (попытка 2): " + err.Error())
					break
				}
			} else {
				success = true
				break
			}
		}

		if !success && content != "" {
			fmt.Println("Nexus: " + content)
			messages = append(messages, map[string]string{
				"role":    "assistant",
				"content": content,
			})
			_ = addToMemory("Пользователь: "+userInput, "dialog")
			_ = addToMemory("Nexus: "+content, "dialog")
			continue
		}

		if agentResp.Message != "" {
			fmt.Println("Nexus: " + agentResp.Message)
			messages = append(messages, map[string]string{
				"role":    "assistant",
				"content": agentResp.Message,
			})
			_ = addToMemory("Пользователь: "+userInput, "dialog")
			_ = addToMemory("Nexus: "+agentResp.Message, "dialog")
			continue
		}

		if agentResp.Action == "" {
			fmt.Println("Nexus: Я не понял, что нужно сделать.")
			continue
		}

		printAction("Выполняется: " + agentResp.Action)

		if agentResp.Action == "write_file" || agentResp.Action == "append_file" {
			path, ok := agentResp.Params["path"].(string)
			if ok && !isSafeToWrite(path) {
				fmt.Println("Nexus: Запись в файлы с таким расширением запрещена для безопасности.")
				continue
			}
		}

		result, err := executeAction(agentResp.Action, agentResp.Params)
		if err != nil {
			printError("Ошибка выполнения: " + err.Error())
			continue
		}

		printSuccess("Готово")
		fmt.Println("Nexus: " + result)

		messages = append(messages, map[string]string{
			"role":    "assistant",
			"content": result,
		})

		_ = addToMemory("Действие: "+agentResp.Action+" -> "+result, "knowledge")
		_ = addToMemory("Пользователь: "+userInput, "dialog")
		_ = addToMemory("Nexus: "+result, "dialog")
	}

	if err := scanner.Err(); err != nil {
		printError("Ошибка чтения ввода: " + err.Error())
	}
}
