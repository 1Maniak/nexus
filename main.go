package main

import (
	"bufio"
	"fmt"
	"os"
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

// --- Основной клиент ---
func main() {
	ensureTerminal()
	enableANSI()

	// Разбор аргументов командной строки
	if len(os.Args) > 1 && os.Args[1] == "--prod" {
		appMode = "prod"
	}

	ensureServices()

	var err error
	db, err = initDB("./data/nexus_memory.db")
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
	if err := loadKnowledgeFromFolder("./base"); err != nil {
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

	url := llamaURL
	scanner := bufio.NewScanner(os.Stdin)

	systemContent := `Ты — Nexus, локальный AI-помощник Максима.
Максим — твой создатель и единственный собеседник. Ты можешь обращаться к нему как на "ТЫ", так и "ВЫ".
Ты — помощник, который отвечает исключительно на русском языке. Никогда не используй другие языки, даже если в вопросе есть иноязычные слова

Правила ответов:
- Отвечай ТОЛЬКО на русском языке. Никогда не используй английский, китайский или другие языки.
- Пиши исключительно на русском языке.
- Предлагай свою помощь, только если тебя попросят!
- Будь кратким: 1–2 предложения, если не просят подробнее.
- Если не знаешь — скажи «я не знаю». Не выдумывай.
- Не расписывай что то слишком много если тебя об этом не просили.
- Ты можешь спросить что бы что то уточнить.
- Если пользователь спрашивает о курсах валют, погоде, новостях или актуальных данных — всегда используй интернет-поиск.
- Если ты не уверен, что знаешь актуальное значение, скажи: «Я поищу в интернете» и выполни поиск.

Какие инструменты у тебя есть:
- Твоя база данных на SQLite в которой у тебя хранятся все диалоги и файлы из базы знаний, к которым ты можешь обращаться при необходимости.
- Поиск в интернете.
- Доступ к файловой системе, ты умеешь открывать, записывать, перезаписывать файлы, а также умеешь открывать папки.
- База знаний из которых ты можешь брать информацию и важный документ Nexus.txt

Факты, которые ты всегда помнишь:
- Ты — Nexus.
- Максим — твой создатель.
- Ты помогаешь с кодом, программированием, идеями и проектами. 
- Ты написан на языке Go

ВАЖНО: Ты пишешь ТОЛЬКО кириллицей. Никаких иероглифов, 
никаких латинских слов без необходимости. Если сомневаешься — 
переформулируй на русском.

Запрещено: использовать китайские, японские, арабские символы.

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

		// if response, ok := tryIdentityResponse(userInput); ok {
		// 	fmt.Println("Nexus:", response)
		// 	recordTurn(&messages, userInput, response)
		// 	_ = addToMemory("Пользователь: "+userInput, "dialog")
		// 	_ = addToMemory("Nexus: "+response, "dialog")
		// 	continue
		// }

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

		start := time.Now()
		fullResponse, err := streamChatCompletion(url, buildChatPayload(requestMessages))
		elapsed := time.Since(start)

		// Замер скорости
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
}
