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
	"sync"
	"time"
)

var (
	keepAliveMu   sync.Mutex
	activeClients int
	shutdownTimer *time.Timer
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

		memCount := 0
		if db != nil {
			memCount = countMemoryItems(db)
		}

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

		modelName := filepath.Base(AppConfig.LlamaModelPath)
		if modelName == "." || modelName == "" {
			modelName = "Unknown"
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"memory_count":    memCount,
			"knowledge_files": fileCount,
			"model":           modelName,
			"context_window":  4096,
			"online":          serverOnline,
		})
	})

	http.HandleFunc("/api/keepalive", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		keepAliveMu.Lock()
		activeClients++
		if shutdownTimer != nil {
			shutdownTimer.Stop()
			shutdownTimer = nil
		}
		keepAliveMu.Unlock()

		fmt.Fprintf(w, "data: ping\n\n")
		flusher.Flush()

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				keepAliveMu.Lock()
				activeClients--
				if activeClients <= 0 {
					shutdownTimer = time.AfterFunc(3*time.Second, func() {
						keepAliveMu.Lock()
						isLast := activeClients <= 0
						keepAliveMu.Unlock()
						if isLast {
							printSystem("Все окна закрыты. Выключаю Nexus...")
							cleanupProcesses()
							os.Exit(0)
						}
					})
				}
				keepAliveMu.Unlock()
				return
			case <-ticker.C:
				fmt.Fprintf(w, "data: ping\n\n")
				flusher.Flush()
			}
		}
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

	sysPrompt := `Ты — Nexus, локальный ИИ. Отвечай на русском языке.
ПРАВИЛА:
1. Отвечай КРАТКО.
2. Если используешь инструмент, пиши ТОЛЬКО команду.
3. НЕ ВЫДУМЫВАЙ ошибки.
4. КАТЕГОРИЧЕСКИ ЗАПРЕЩЕНО переводить пути на английский язык. Копируй путь ТОЧНО так, как его написал пользователь, буква в букву. Если в пути есть кириллица, оставляй её.

ИНСТРУМЕНТЫ:
- Открыть файл/папку: [TOOL: open] путь (БЕЗ кавычек)
- Прочитать файл: [TOOL: read] путь (БЕЗ кавычек)
- Список файлов: [TOOL: list] путь (БЕЗ кавычек)
- Поиск: [TOOL: search] запрос
- Время: [TOOL: time]`

	messages := []map[string]string{
		{"role": "system", "content": sysPrompt + "\n" + ragContext},
		{"role": "user", "content": req.Message},
	}

	payload := buildChatPayload(messages)
	payload["stream"] = false

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sendErrorStream(w, flusher, err.Error())
		return
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	firstResponse := ""
	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				firstResponse, _ = msg["content"].(string)
			}
		}
	}

	toolName, toolResult := executeTool(firstResponse)

	if toolName != "" {
		printSystem("Агент использует инструмент: " + toolName)

		messages = append(messages, map[string]string{"role": "assistant", "content": firstResponse})
		messages = append(messages, map[string]string{"role": "system", "content": toolResult})

		payload = buildChatPayload(messages)
		payload["stream"] = true
		jsonData, _ = json.Marshal(payload)
		httpReq, _ = http.NewRequestWithContext(ctx, "POST", llamaURL, bytes.NewBuffer(jsonData))
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err = http.DefaultClient.Do(httpReq)
		if err != nil {
			sendErrorStream(w, flusher, "Ошибка при повторном запросе к LLM: "+err.Error())
			return
		}
		defer resp.Body.Close()
	} else {
		fmt.Fprintf(w, "data: %s\n\n", firstResponse)
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
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
