package main

// --- Цвета и параметры LLM ---
const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorGray  = "\033[90m"

	llmModel           = "qwen2.5-3b-instruct-q4_k_m.gguf"
	maxHistoryMessages = 20
	maxRAGResults      = 3
	maxGeneratedTokens = 2048

	// Пути к сервисам
	ollamaURL   = "http://localhost:11434"
	llamaURL    = "http://localhost:8080/v1/chat/completions"
	llamaServer = `C:\Users\maksi\AI\llama.cpp\llama-server.exe`
	llamaModel  = `C:\Users\maksi\AI\llama.cpp\models\qwen2.5-3b-instruct-q4_k_m.gguf`
)

// Режим работы: "dev" — все логи, "prod" — только ошибки и диалог
var appMode = "dev"
