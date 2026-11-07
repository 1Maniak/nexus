package main

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func setupCleanup() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		cleanupProcesses()
		os.Exit(0)
	}()
}

func cleanupProcesses() {
	// Убиваем llama-server, чтобы освободить порт 8080
	exec.Command("taskkill", "/F", "/IM", "llama-server.exe").Run()

	// Для закрытия Ollama
	exec.Command("taskkill", "/F", "/IM", "ollama.exe").Run()
}
