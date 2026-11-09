package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func enableANSI() {
	kernel32 := windows.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}

	var mode uint32
	_, _, _ = getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	mode |= 0x0004
	_, _, _ = setConsoleMode.Call(uintptr(handle), uintptr(mode))
}

func main() {
	enableANSI()
	setupCleanup()

	exePath, _ := os.Executable()
	workDir := filepath.Dir(exePath)
	exeName := strings.ToLower(filepath.Base(exePath))

	if strings.Contains(exeName, "dev") {
		appMode = "dev"
	} else {
		appMode = "prod"
	}

	os.MkdirAll(filepath.Join(workDir, "data"), 0755)
	loadConfig(workDir)

	if appMode == "dev" {
		ensureTerminal()
	}

	var err error
	db, err = initDB(filepath.Join(workDir, "data", "nexus_memory.db"))
	if err != nil {
		printError("Ошибка инициализации БД: " + err.Error())
		os.Exit(1)
	}
	defer db.Close()

	go startWebServer()
	time.Sleep(300 * time.Millisecond)
	openBrowser("http://localhost:8081")

	printSystem("Запускаю сервисы ИИ (это может занять время)...")
	ensureServices()

	printSystem("Загружаю память...")
	totalItems := countMemoryItems(db)
	printSystem(fmt.Sprintf("Загружено %d записей", totalItems))
	fmt.Println()

	printSystem("Загружаю базу знаний...")
	if err := loadKnowledgeFromFolder(filepath.Join(workDir, "base")); err != nil {
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

	if appMode == "dev" {
		url := llamaURL
		scanner := bufio.NewScanner(os.Stdin)

		systemContent := `Ты — Nexus, локальный ИИ. Отвечай на русском языке.
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

			var ragContext string
			memories, err := searchMemory(userInput, maxRAGResults)
			if err == nil && len(memories) > 0 {
				ragContext = "Контекст из памяти:\n"
				for i, mem := range memories {
					ragContext += fmt.Sprintf("%d. %s\n", i+1, mem)
				}
			}

			messages = append(messages, map[string]string{"role": "user", "content": userInput})
			requestMessages := buildRequestMessages(messages, ragContext, userInput)

			for {
				start := time.Now()
				fullResponse, err := streamChatCompletion(url, buildChatPayload(requestMessages))
				elapsed := time.Since(start)

				if err != nil {
					printError("Ошибка отправки запроса: " + err.Error())
					break
				}

				tokens := len([]rune(fullResponse)) / 4
				if elapsed.Seconds() > 0 {
					speed := float64(tokens) / elapsed.Seconds()
					printSystem(fmt.Sprintf("Скорость: %.1f ток/с (%.1f сек, ~%d токенов)", speed, elapsed.Seconds(), tokens))
				}

				toolName, toolResult := executeTool(fullResponse)

				if toolName != "" {
					printSystem(fmt.Sprintf("Агент вызвал инструмент [%s]", toolName))
					messages = append(messages, map[string]string{"role": "assistant", "content": fullResponse})
					messages = append(messages, map[string]string{"role": "system", "content": toolResult})
					requestMessages = buildRequestMessages(messages, "", userInput)
					continue
				}

				fmt.Println("Nexus:", fullResponse)
				recordTurn(&messages, userInput, fullResponse)
				_ = addToMemory("Пользователь: "+userInput, "dialog")
				_ = addToMemory("Nexus: "+fullResponse, "dialog")
				break
			}
		}
		if err := scanner.Err(); err != nil {
			printError("Ошибка чтения ввода: " + err.Error())
		}
	} else {
		select {}
	}

	cleanupProcesses()
}
