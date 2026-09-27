package main

import (
	"fmt"
	"net"
	"os/exec"
	"time"
)

// checkPort проверяет, слушается ли порт
func checkPort(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// startServicesWindow открывает ОДНО окно wt с двумя вкладками:
// вкладка 1 - Ollama, вкладка 2 - llama-server
func startServicesWindow() error {
	llamaCmd := fmt.Sprintf(`"%s" -m "%s" --port 8080 -c 4096 -t 8 --no-mmap`,
		llamaServer, llamaModel)

	// Проверяем, что нужно запустить
	needOllama := !checkPort("localhost", 11434)
	needLlama := !checkPort("localhost", 8080)

	if !needOllama && !needLlama {
		return nil
	}

	// Собираем аргументы для wt
	var args []string
	args = append(args, "nt", "-p", "Command Prompt")

	// Первая вкладка
	if needOllama {
		args = append(args, "cmd", "/k", "ollama serve")
	} else {
		args = append(args, "cmd", "/k", "echo Ollama already running")
	}

	// Вторая вкладка
	args = append(args, ";", "new-tab", "-p", "Command Prompt")
	if needLlama {
		args = append(args, "cmd", "/k", llamaCmd)
	} else {
		args = append(args, "cmd", "/k", "echo llama-server already running")
	}

	cmd := exec.Command("wt", args...)
	return cmd.Start()
}

// ensureServices запускает сервисы, если они не запущены
func ensureServices() {
	// Проверяем, нужно ли что-то запускать
	needOllama := !checkPort("localhost", 11434)
	needLlama := !checkPort("localhost", 8080)

	if !needOllama && !needLlama {
		printSystem("Все сервисы уже запущены")
		return
	}

	printSystem("Запускаю сервисы в отдельном окне...")
	if err := startServicesWindow(); err != nil {
		printError("Не удалось открыть окно сервисов: " + err.Error())
		return
	}

	// Ждём Ollama
	if needOllama {
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			if checkPort("localhost", 11434) {
				printSystem("Ollama запущен")
				break
			}
		}
	}

	// Ждём llama-server (дольше, модель грузится)
	if needLlama {
		printSystem("Ожидаю загрузки llama-server (это может занять 10-30 секунд)...")
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			if checkPort("localhost", 8080) {
				printSystem("llama-server запущен")
				break
			}
		}
	}
}
