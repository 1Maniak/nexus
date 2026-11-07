package main

import (
	"crypto/md5"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
	"golang.org/x/net/html"
)

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

var stateFile = "data/knowledge_state.json"

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
