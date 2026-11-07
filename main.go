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

	// ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
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

	ensureServices()

	go startWebServer()
	time.Sleep(1 * time.Second)
	openBrowser("http://localhost:8081")

	var err error
	db, err = initDB(filepath.Join(workDir, "data", "nexus_memory.db"))
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

	// РЕЖИМ РАЗРАБОТЧИКА
	if appMode == "dev" {
		url := llamaURL
		scanner := bufio.NewScanner(os.Stdin)

		systemContent := `Ты — Nexus, локальный AI-помощник.
Твой создатель и собеседник — Максим (обращайся к нему на "ты").
Отвечай ТОЛЬКО на русском языке. Используй ТОЛЬКО кириллицу.
НИКОГДА не используй китайские, японские иероглифы или английские слова без необходимости.

ПРАВИЛА:
- Если тебя просят написать код, пиши его без лишних вступлений.
- Не выдумывай факты. Если не знаешь — так и скажи.
- Будь кратким.
- Не предлагай свою помощь, если тебя не просили.

ФАКТЫ:
- Ты — Nexus, программа на Go.
- Ты умеешь искать в интернете, читать файлы и хранить память.
- Максим — твой создатель.`

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

			handled := false
			searchQuery := ""
			lower := strings.ToLower(userInput)

			if strings.Contains(lower, "время") || strings.Contains(lower, "часы") || strings.Contains(lower, "сколько времени") {
				currentTime := getCurrentTime()
				response := "Сейчас " + currentTime
				fmt.Println("Nexus:", response)
				recordTurn(&messages, userInput, response)
				handled = true
				continue
			}

			if strings.Contains(strings.ToLower(userInput), "прочитай") || strings.Contains(strings.ToLower(userInput), "открой") || strings.Contains(strings.ToLower(userInput), "покажи файл") {
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

			if strings.Contains(strings.ToLower(userInput), "запиши") || strings.Contains(strings.ToLower(userInput), "сохрани") || strings.Contains(strings.ToLower(userInput), "перезапиши") {
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

			if strings.HasPrefix(userInput, "!поиск") {
				searchQuery = strings.TrimPrefix(userInput, "!поиск")
				searchQuery = strings.TrimSpace(searchQuery)
			} else if strings.HasPrefix(userInput, "найди") {
				searchQuery = strings.TrimPrefix(userInput, "найди")
				searchQuery = strings.TrimSpace(searchQuery)
			} else {
				keywords := []string{
					"погода", "температура",
					"курс", "доллар", "евро", "юань", "рубль", "биткоин",
					"валюта", "американская", "европейская", "китайская",
					"сколько стоит", "цена", "котировка",
					"новости", "в интернете",
				}
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
					printSystem("Nexus ищет в интернете...\n")
					result, err := webSearch(searchQuery)
					if err != nil {
						printError("Ошибка поиска: " + err.Error())
						handled = true
						continue
					}
					printSystem("Готово!")

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

			var ragContext string
			memories, err := searchMemory(userInput, maxRAGResults)
			if err == nil && len(memories) > 0 {
				ragContext = "Контекст из памяти (используй только если релевантно):\n"
				for i, mem := range memories {
					ragContext += fmt.Sprintf("%d. %s\n", i+1, mem)
				}
			}

			requestMessages := buildRequestMessages(messages, ragContext, userInput)

			start := time.Now()
			fullResponse, err := streamChatCompletion(url, buildChatPayload(requestMessages))
			elapsed := time.Since(start)

			tokens := len([]rune(fullResponse)) / 4
			if elapsed.Seconds() > 0 {
				speed := float64(tokens) / elapsed.Seconds()
				printSystem(fmt.Sprintf("Скорость: %.1f ток/с (%.1f сек, ~%d токенов)", speed, elapsed.Seconds(), tokens))
			}

			recordTurn(&messages, userInput, fullResponse)
			_ = addToMemory("Пользователь: "+userInput, "dialog")
			_ = addToMemory("Nexus: "+fullResponse, "dialog")
		}
		if err := scanner.Err(); err != nil {
			printError("Ошибка чтения ввода: " + err.Error())
		}
	} else {
		// РЕЖИМ PROD
		select {}
	}

	cleanupProcesses()
}
