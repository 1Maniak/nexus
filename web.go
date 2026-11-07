package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type WebRequest struct {
	Message   string `json:"message"`
	WebSearch bool   `json:"web_search"`
}

func startWebServer() {
	exePath, _ := os.Executable()
	workDir := filepath.Dir(exePath)

	htmlPath := filepath.Join(workDir, "web", "index.html")
	photoPath := filepath.Join(workDir, "web", "photo")
	basePath := filepath.Join(workDir, "base")

	http.Handle("/photo/", http.StripPrefix("/photo/", http.FileServer(http.Dir(photoPath))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, htmlPath)
	})

	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req WebRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		handleChatStream(r.Context(), w, flusher, req)
	})

	http.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			http.Error(w, "Файл слишком большой", http.StatusBadRequest)
			return
		}

		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Ошибка получения файла", http.StatusBadRequest)
			return
		}
		defer file.Close()

		os.MkdirAll(basePath, 0755)
		dstPath := filepath.Join(basePath, handler.Filename)
		dst, err := os.Create(dstPath)
		if err != nil {
			http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			http.Error(w, "Ошибка копирования", http.StatusInternalServerError)
			return
		}

		go func() {
			err := loadKnowledgeFromFolder(basePath)
			if err != nil {
				printError("Ошибка индексации нового файла: " + err.Error())
			}
		}()

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// ЗАЩИТА ОТ NIL БД
		if db == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"memory_count":    0,
				"knowledge_files": 0,
				"model":           llmModel,
				"context_window":  4096,
				"online":          false,
			})
			return
		}

		memCount := countMemoryItems(db)
		kState, _ := loadKnowledgeState()
		fileCount := 0
		if kState != nil {
			fileCount = len(kState.Files)
		}

		serverOnline := false
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get("http://localhost:8080/health")
		if err == nil {
			if resp.StatusCode == 200 {
				serverOnline = true
			}
			resp.Body.Close()
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"memory_count":    memCount,
			"knowledge_files": fileCount,
			"model":           llmModel,
			"context_window":  4096,
			"online":          serverOnline,
		})
	})

	http.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		go func() {
			time.Sleep(500 * time.Millisecond)
			cleanupProcesses()
			os.Exit(0)
		}()
	})

	printSystem("Веб-интерфейс доступен по адресу: http://localhost:8081")

	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		printError("Ошибка запуска веб-сервера: " + err.Error())
	}
}

func handleChatStream(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, req WebRequest) {
	var ragContext string
	memories, err := searchMemory(req.Message, 2)
	if err == nil && len(memories) > 0 {
		ragContext = "Контекст из памяти:\n"
		for _, mem := range memories {
			ragContext += "- " + mem + "\n"
		}
	}

	var searchContext string
	if req.WebSearch {
		printSystem("Nexus ищет в интернете (Web UI)...")
		result, err := webSearch(req.Message)
		if err == nil {
			searchContext = "Результаты поиска в интернете (используй их для ответа):\n" + result + "\n"
		} else {
			searchContext = "Ошибка поиска в интернете.\n"
		}
	}

	sysPrompt := "Ты Nexus, локальный ИИ-помощник. Отвечай кратко, по делу, на русском языке."
	finalPrompt := sysPrompt + "\n" + ragContext + "\n" + searchContext

	messages := []map[string]string{
		{"role": "system", "content": finalPrompt},
		{"role": "user", "content": req.Message},
	}

	payload := buildChatPayload(messages)
	payload["stream"] = true

	jsonData, err := json.Marshal(payload)
	if err != nil {
		sendErrorStream(w, flusher, err.Error())
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", llamaURL, bytes.NewBuffer(jsonData))
	if err != nil {
		sendErrorStream(w, flusher, err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		sendErrorStream(w, flusher, err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		sendErrorStream(w, flusher, fmt.Sprintf("сервер вернул статус %d", resp.StatusCode))
		return
	}

	reader := bufio.NewReader(resp.Body)
	var fullResponse strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok {
							fullResponse.WriteString(content)
							fmt.Fprintf(w, "data: %s\n\n", content)
							flusher.Flush()
						}
					}
				}
			}
		}
	}

	_ = addToMemory("Пользователь: "+req.Message, "dialog")
	_ = addToMemory("Nexus: "+fullResponse.String(), "dialog")

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func sendErrorStream(w http.ResponseWriter, flusher http.Flusher, errMsg string) {
	fmt.Fprintf(w, "data: [ERROR] %s\n\n", errMsg)
	flusher.Flush()
}
