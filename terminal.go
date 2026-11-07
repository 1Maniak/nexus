package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func isWindowsTerminal() bool {
	return os.Getenv("WT_SESSION") != ""
}

func relaunchInWindowsTerminal() {
	exePath, err := os.Executable()
	if err != nil {
		printError("Не удалось определить путь к exe: " + err.Error())
		return
	}

	workDir := filepath.Dir(exePath)
	cmdStr := fmt.Sprintf(`cd /d "%s" && "%s"`, workDir, exePath)

	if len(os.Args) > 1 {
		cmdStr += " " + os.Args[1]
	}

	args := []string{"-w", "-1", "nt", "-p", "Command Prompt", "cmd", "/k", cmdStr}

	cmd := exec.Command("wt", args...)
	if err := cmd.Start(); err != nil {
		printSystem("Windows Terminal не найден, продолжаю в текущем")
		return
	}
	os.Exit(0)
}

func ensureTerminal() {
	if isWindowsTerminal() {
		return
	}
	if _, err := exec.LookPath("wt"); err != nil {
		return
	}
	relaunchInWindowsTerminal()
}
