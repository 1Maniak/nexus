package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func checkPort(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func startServicesWindow() error {
	exePath, _ := os.Executable()
	workDir := filepath.Dir(exePath)

	llamaExe, _ := filepath.Abs(filepath.Join(workDir, AppConfig.LlamaServerPath))
	llamaMod, _ := filepath.Abs(filepath.Join(workDir, AppConfig.LlamaModelPath))

	needOllama := !checkPort("localhost", 11434)
	needLlama := !checkPort("localhost", 8080)

	if !needOllama && !needLlama {
		return nil
	}

	// РЕЖИМ PROD (Скрытый запуск в фоне)
	if appMode == "prod" {
		if needOllama {
			cmd := exec.Command("ollama", "serve")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			cmd.Start()
		}
		if needLlama {
			cmd := exec.Command(llamaExe, "-m", llamaMod, "--port", "8080", "-c", "4096", "-t", "8", "--no-mmap")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			cmd.Start()
		}
		return nil
	}

	// РЕЖИМ DEV (Открываем терминалы)
	batPath := filepath.Join(workDir, "run_llama.bat")
	batContent := fmt.Sprintf("@echo off\r\nchcp 65001 >nul\r\n\"%s\" -m \"%s\" --port 8080 -c 4096 -t 8 --no-mmap", llamaExe, llamaMod)
	os.WriteFile(batPath, []byte(batContent), 0644)

	var args []string
	args = append(args, "-w", "-1", "nt", "-p", "Command Prompt", "-d", workDir)

	if needOllama {
		args = append(args, "cmd", "/k", "ollama serve")
	} else {
		args = append(args, "cmd", "/k", "echo Ollama already running")
	}

	args = append(args, ";", "new-tab", "-p", "Command Prompt", "-d", workDir)

	if needLlama {
		args = append(args, "cmd", "/k", "run_llama.bat")
	} else {
		args = append(args, "cmd", "/k", "echo llama-server already running")
	}

	cmd := exec.Command("wt", args...)
	return cmd.Start()
}

func ensureServices() {
	needOllama := !checkPort("localhost", 11434)
	needLlama := !checkPort("localhost", 8080)

	if !needOllama && !needLlama {
		printSystem("Все сервисы уже запущены")
		return
	}

	printSystem("Запускаю сервисы...")
	if err := startServicesWindow(); err != nil {
		printError("Не удалось запустить сервисы: " + err.Error())
		return
	}

	if needOllama {
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			if checkPort("localhost", 11434) {
				printSystem("Ollama запущен")
				break
			}
		}
	}

	if needLlama {
		printSystem("Ожидаю загрузки llama-server...")
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			if checkPort("localhost", 8080) {
				printSystem("llama-server запущен")
				break
			}
		}
	}
}
