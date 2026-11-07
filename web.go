package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type WebRequest struct {
	Message string `json:"message"`
}

func startWebServer() {
	exePath, _ := os.Executable()
	workDir := filepath.Dir(exePath)
	htmlPath := filepath.Join(workDir, "web", "index.html")

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

		response, err := processWebMessage(req.Message)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": response})
	})

	http.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		go func() {
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

func processWebMessage(userInput string) (string, error) {
	var ragContext string
	memories, err := searchMemory(userInput, 2)
	if err == nil && len(memories) > 0 {
		ragContext = "Контекст из памяти:\n"
		for _, mem := range memories {
			ragContext += "- " + mem + "\n"
		}
	}

	sysPrompt := "Ты Nexus, локальный ИИ-помощник. Отвечай кратко, по делу, на русском языке."
	messages := []map[string]string{
		{"role": "system", "content": sysPrompt + "\n" + ragContext},
		{"role": "user", "content": userInput},
	}

	payload := buildChatPayload(messages)
	payload["stream"] = false

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(llamaURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					return content, nil
				}
			}
		}
	}

	return "", fmt.Errorf("не удалось распарсить ответ от LLM")
}
