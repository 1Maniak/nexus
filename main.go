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

// --- Цвета и параметры LLM ---
const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorGray  = "\033[90m"

	llmModel           = "qwen2.5-3b-instruct-q4_k_m.gguf"
	maxHistoryMessages = 12
	maxRAGResults      = 3
	maxGeneratedTokens = 400
)

func printSystem(msg string) {
	fmt.Println(string(colorGray) + msg + string(colorReset))
}

func printError(msg string) {
	fmt.Println(string(colorRed) + msg + string(colorReset))
}

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
func extractNumber(text string) string {
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

func trimConversation(messages []map[string]string, keepNonSystem int) []map[string]string {
	if len(messages) <= 1 {
		return messages
	}
	system := messages[0]
	rest := messages[1:]
	if len(rest) <= keepNonSystem {
		return messages
	}
	return append([]map[string]string{system}, rest[len(rest)-keepNonSystem:]...)
}

func buildChatPayload(messages []map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"model":             llmModel,
		"messages":          messages,
		"stream":            true,
		"temperature":       0.35,
		"top_p":             0.85,
		"repeat_penalty":    1.25,
		"frequency_penalty": 0.35,
		"presence_penalty":  0.2,
		"max_tokens":        maxGeneratedTokens,
	}
}

func streamChatCompletion(url string, payload map[string]interface{}) (string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	fmt.Print("Nexus: ")
	fullResponse := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if !bytes.HasPrefix([]byte(line), []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace([]byte(line[5:]))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue
		}
		choices, ok := chunk["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := delta["content"].(string)
		if !ok {
			continue
		}
		fmt.Print(content)
		fullResponse += content
	}
	fmt.Println()
	return fullResponse, nil
}

func tryIdentityResponse(input string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(input))
	switch {
	case lower == "привет" || strings.HasPrefix(lower, "привет ") || lower == "здравствуй":
		return "Привет! Я Nexus, помощник Максима. Чем могу помочь?", true
	case lower == "нексус" || lower == "nexus":
		return "Да, я здесь. Я Nexus — твой локальный помощник. Что нужно?", true
	case strings.Contains(lower, "кто я") || strings.Contains(lower, "кто такой я"):
		return "Ты — Максим, мой создатель и разработчик Nexus.", true
	case strings.Contains(lower, "кто ты") || strings.Contains(lower, "кто такой nexus") || strings.Contains(lower, "кто такой нексус"):
		return "Я Nexus — локальный AI-помощник, созданный тобой, Максим. Помогаю с кодом, идеями и проектами.", true
	}
	return "", false
}

func buildRequestMessages(history []map[string]string, extraSystem, userInput string) []map[string]string {
	request := make([]map[string]string, len(history))
	copy(request, history)
	if extraSystem != "" {
		request = append(request, map[string]string{
			"role":    "system",
			"content": extraSystem,
		})
	}
	request = append(request, map[string]string{
		"role":    "user",
		"content": userInput,
	})
	return request
}

func recordTurn(messages *[]map[string]string, userInput, response string) {
	*messages = append(*messages,
		map[string]string{"role": "user", "content": userInput},
		map[string]string{"role": "assistant", "content": response},
	)
	*messages = trimConversation(*messages, maxHistoryMessages)
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

	systemContent := `Ты — Nexus, локальный AI-помощник Максима.
Максим — твой создатель и единственный собеседник. Всегда обращайся к нему на «ты».

Правила ответов:
- Отвечай ТОЛЬКО на русском языке. Никогда не используй английский, китайский или другие языки.
- Будь кратким: 1–3 предложения, если не просят подробнее.
- Если не знаешь — скажи «я не знаю». Не выдумывай.

Факты, которые ты всегда помнишь:
- Ты — Nexus.
- Максим — твой создатель.
- Ты помогаешь с кодом, идеями и проектами.

Примеры:
Максим: Привет
Nexus: Привет! Я Nexus, помощник Максима. Чем могу помочь?

Максим: Кто я?
Nexus: Ты — Максим, мой создатель.

Максим: Кто ты?
Nexus: Я Nexus — твой локальный AI-помощник.`

	messages := []map[string]string{
		{
			"role":    "system",
			"content": systemContent,
		},
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

		handled := false
		searchQuery := ""
		lower := strings.ToLower(userInput)

		if response, ok := tryIdentityResponse(userInput); ok {
			fmt.Println("Nexus:", response)
			recordTurn(&messages, userInput, response)
			_ = addToMemory("Пользователь: "+userInput, "dialog")
			_ = addToMemory("Nexus: "+response, "dialog")
			continue
		}

		// --- Обработка запроса времени ---
		if strings.Contains(lower, "время") || strings.Contains(lower, "часы") || strings.Contains(lower, "сколько времени") {
			currentTime := getCurrentTime()
			response := "Сейчас " + currentTime
			fmt.Println("Nexus:", response)
			recordTurn(&messages, userInput, response)
			handled = true
			continue
		}

		// --- Обработка команд чтения и записи файлов ---
		if strings.Contains(strings.ToLower(userInput), "прочитай") ||
			strings.Contains(strings.ToLower(userInput), "открой") ||
			strings.Contains(strings.ToLower(userInput), "покажи файл") {

			// Извлекаем путь к файлу
			parts := strings.Fields(userInput)
			var filePath string
			for i, part := range parts {
				if strings.Contains(strings.ToLower(part), "файл") && i+1 < len(parts) {
					filePath = strings.TrimSpace(parts[i+1])
					break
				}
			}
			if filePath == "" && len(parts) > 1 {
				filePath = strings.TrimSpace(parts[len(parts)-1])
			}
			if filePath == "" {
				fmt.Println("Nexus: Укажите путь к файлу.")
				handled = true
				continue
			}

			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				fmt.Println("Nexus: Файл не найден:", filePath)
				handled = true
				continue
			}

			data, err := os.ReadFile(filePath)
			if err != nil {
				printError("Ошибка чтения файла: " + err.Error())
				handled = true
				continue
			}

			content := string(data)
			fmt.Println("Nexus: Содержимое файла " + filePath + ":\n" + content)
			_ = addToMemory("Прочитан файл: "+filePath+"\n"+content, "knowledge")
			handled = true
			continue
		}

		if strings.Contains(strings.ToLower(userInput), "запиши") ||
			strings.Contains(strings.ToLower(userInput), "сохрани") ||
			strings.Contains(strings.ToLower(userInput), "перезапиши") {

			parts := strings.SplitN(userInput, " ", 3)
			var filePath, content string
			if len(parts) >= 3 {
				if strings.Contains(strings.ToLower(parts[1]), "файл") {
					filePath = parts[2]
					if len(parts) > 3 {
						content = strings.Join(parts[3:], " ")
					} else {
						fmt.Println("Nexus: Укажите содержимое для записи.")
						handled = true
						continue
					}
				} else {
					fmt.Println("Nexus: Используйте формат: 'запиши в файл путь_к_файлу содержимое'")
					handled = true
					continue
				}
			} else {
				fmt.Println("Nexus: Используйте формат: 'запиши в файл путь_к_файлу содержимое'")
				handled = true
				continue
			}

			if filePath == "" {
				fmt.Println("Nexus: Укажите путь к файлу.")
				handled = true
				continue
			}
			if content == "" {
				fmt.Println("Nexus: Укажите содержимое для записи.")
				handled = true
				continue
			}

			err := os.WriteFile(filePath, []byte(content), 0644)
			if err != nil {
				printError("Ошибка записи файла: " + err.Error())
				handled = true
				continue
			}
			fmt.Println("Nexus: Файл успешно записан:", filePath)
			_ = addToMemory("Записан файл: "+filePath+"\n"+content, "knowledge")
			handled = true
			continue
		}

		// --- Обработка явной команды поиска ---
		if strings.HasPrefix(userInput, "!поиск") {
			searchQuery = strings.TrimPrefix(userInput, "!поиск")
			searchQuery = strings.TrimSpace(searchQuery)
		} else if strings.HasPrefix(userInput, "найди") {
			searchQuery = strings.TrimPrefix(userInput, "найди")
			searchQuery = strings.TrimSpace(searchQuery)
		} else {
			// Автоматическое определение поиска (только фактические запросы)
			keywords := []string{"погода", "температура", "курс", "доллар", "евро", "юань", "рубль", "биткоин", "новости", "в интернете"}
			for _, kw := range keywords {
				if strings.Contains(lower, kw) {
					searchQuery = userInput
					break
				}
			}
		}

		if searchQuery != "" {
			if strings.HasPrefix(userInput, "!поиск") || strings.HasPrefix(userInput, "найди") {
				searchQuery = strings.TrimSpace(searchQuery)
				if searchQuery == "" {
					fmt.Println("Nexus: Введите, что искать.")
					handled = true
					continue
				}
			}
			if !handled && searchQuery != "" {
				fmt.Print("Nexus ищет в интернете...\n")
				result, err := webSearch(searchQuery)
				if err != nil {
					printError("Ошибка поиска: " + err.Error())
					handled = true
					continue
				}
				fmt.Println("Готово!")

				number := extractNumber(result)
				contextMsg := "Ты получил результаты поиска по запросу. Используй только эту информацию для ответа. Если точных данных нет, скажи, что не знаешь. Не выдумывай цифры. Отвечай только на русском.\n\nРезультаты поиска:\n" + result
				if number != "" {
					contextMsg += "\n\nВ тексте найдено число (возможно, курс): " + number + ". Используй его, если оно соответствует вопросу."
				}
				requestMessages := buildRequestMessages(messages, contextMsg, userInput)
				fullResponse, err := streamChatCompletion(url, buildChatPayload(requestMessages))
				if err != nil {
					printError("Ошибка отправки запроса: " + err.Error())
					handled = true
					continue
				}
				recordTurn(&messages, userInput, fullResponse)
				_ = addToMemory("Пользователь: "+userInput, "dialog")
				_ = addToMemory("Nexus: "+fullResponse, "dialog")
				handled = true
				continue
			}
		}

		if handled {
			continue
		}

		// --- Обычный диалог с RAG ---
		var ragContext string
		memories, err := searchMemory(userInput, maxRAGResults)
		if err == nil && len(memories) > 0 {
			ragContext = "Контекст из памяти (используй только если релевантно):\n"
			for i, mem := range memories {
				ragContext += fmt.Sprintf("%d. %s\n", i+1, mem)
			}
		}

		requestMessages := buildRequestMessages(messages, ragContext, userInput)
		fullResponse, err := streamChatCompletion(url, buildChatPayload(requestMessages))
		if err != nil {
			printError("Ошибка отправки запроса: " + err.Error())
			continue
		}

		recordTurn(&messages, userInput, fullResponse)
		_ = addToMemory("Пользователь: "+userInput, "dialog")
		_ = addToMemory("Nexus: "+fullResponse, "dialog")
	}

	if err := scanner.Err(); err != nil {
		printError("Ошибка чтения ввода: " + err.Error())
	}
}
