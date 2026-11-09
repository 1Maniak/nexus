package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

func executeTool(response string) (string, string) {
	response = strings.TrimSpace(response)
	response = strings.ReplaceAll(response, "\"", "")
	response = strings.ReplaceAll(response, "'", "")

	re := regexp.MustCompile(`(?i)\[(?:TOOL|TOP):\s*(open|read|list|search|time)\]\s*(.*)`)
	matches := re.FindStringSubmatch(response)

	if len(matches) < 3 {
		return "", ""
	}

	action := strings.ToLower(matches[1])
	argument := strings.TrimSpace(matches[2])

	switch action {
	case "open":
		err := openFileOrFolder(argument)
		if err != nil {
			return "open", fmt.Sprintf("Ошибка при открытии: %s", err.Error())
		}
		return "open", fmt.Sprintf("Успешно открыто: %s", argument)

	case "read":
		content, err := os.ReadFile(argument)
		if err != nil {
			return "read", fmt.Sprintf("Ошибка чтения: %s", err.Error())
		}
		return "read", fmt.Sprintf("Содержимое файла %s:\n%s", argument, string(content))

	case "list":
		entries, err := os.ReadDir(argument)
		if err != nil {
			return "list", fmt.Sprintf("Ошибка чтения папки: %s", err.Error())
		}
		var files []string
		for _, entry := range entries {
			files = append(files, entry.Name())
		}
		return "list", fmt.Sprintf("Файлы в папке %s:\n%s", argument, strings.Join(files, "\n"))

	case "search":
		result, err := webSearch(argument)
		if err != nil {
			return "search", fmt.Sprintf("Ошибка поиска: %s", err.Error())
		}
		return "search", "Результаты поиска:\n" + result

	case "time":
		return "time", "Сейчас: " + getCurrentTime()
	}

	return "", ""
}

func openFileOrFolder(path string) error {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "\"")

	printSystem("Пытаюсь открыть путь: " + path) 

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		printError("Файл не найден: " + absPath)
		return fmt.Errorf("путь не существует")
	}

	cmd := exec.Command("cmd", "/c", "start", "", absPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
