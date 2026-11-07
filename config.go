package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorGray  = "\033[90m"

	llmModel           = "qwen2.5-3b-instruct-q4_k_m.gguf"
	maxHistoryMessages = 6
	maxRAGResults      = 2
	maxGeneratedTokens = 2048

	// Пути к сервисам
	ollamaURL = "http://localhost:11434"
	llamaURL  = "http://localhost:8080/v1/chat/completions"
)

// Структура для чтения настроек из JSON
type Config struct {
	LlamaServerPath string `json:"llama_server_path"`
	LlamaModelPath  string `json:"llama_model_path"`
}

var AppConfig Config

var appMode = "dev"

func loadConfig(workDir string) {
	configFile := filepath.Join(workDir, "config.json")
	data, err := os.ReadFile(configFile)

	if err != nil {
		printSystem("Файл config.json не найден. Создаю новый...")
		AppConfig = Config{
			LlamaServerPath: "llama.cpp/llama-server.exe",
			LlamaModelPath:  "llama.cpp/models/qwen2.5-3b-instruct-q4_k_m.gguf",
		}
		defaultData, _ := json.MarshalIndent(AppConfig, "", "  ")
		os.WriteFile(configFile, defaultData, 0644)
		return
	}

	err = json.Unmarshal(data, &AppConfig)
	if err != nil {
		printError("Ошибка чтения config.json: " + err.Error())
		AppConfig.LlamaServerPath = "llama.cpp/llama-server.exe"
		AppConfig.LlamaModelPath = "llama.cpp/models/qwen2.5-3b-instruct-q4_k_m.gguf"
	}
}
