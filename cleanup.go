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
	// Убиваем llama-server 
	cmd1 := exec.Command("taskkill", "/F", "/T", "/IM", "llama-server.exe")
	cmd1.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd1.Run()

	// Убиваем ollama
	cmd2 := exec.Command("taskkill", "/F", "/T", "/IM", "ollama.exe")
	cmd2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd2.Run()
}
