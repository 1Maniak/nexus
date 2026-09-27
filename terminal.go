package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// isWindowsTerminal проверяет, запущен ли процесс в Windows Terminal
func isWindowsTerminal() bool {
	return os.Getenv("WT_SESSION") != ""
}

// relaunchInWindowsTerminal перезапускает nexus.exe в wt
func relaunchInWindowsTerminal() {
	exePath, err := os.Executable()
	if err != nil {
		printError("Не удалось определить путь к exe: " + err.Error())
		return
	}

	// Рабочая директория = папка с exe
	workDir := filepath.Dir(exePath)

	args := []string{"-w", "0", "nt", "-p", "Command Prompt",
		"cmd", "/k", "cd /d \"" + workDir + "\" && \"" + exePath + "\""}

	if len(os.Args) > 1 && os.Args[1] == "--prod" {
		args = append(args, "--prod")
	}

	cmd := exec.Command("wt", args...)
	if err := cmd.Start(); err != nil {
		printSystem("Windows Terminal не найден, продолжаю в текущем")
		return
	}

	os.Exit(0)
}

// ensureTerminal проверяет терминал и перезапускает в wt при необходимости
func ensureTerminal() {
	if isWindowsTerminal() {
		return
	}

	if _, err := exec.LookPath("wt"); err != nil {
		return
	}

	relaunchInWindowsTerminal()
}
