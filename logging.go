package main

import "fmt"

func printSystem(msg string) {
	if appMode == "dev" {
		fmt.Println(string(colorGray) + msg + string(colorReset))
	}
}

// НЕ НУЖНО!
// Для отладочного мусора
// func printDebug(msg string) {
// 	if appMode == "dev" {
// 		fmt.Println(string(colorGray) + "[DEBUG] " + msg + string(colorReset))
// 	}
// }

func printError(msg string) {
	// Ошибки показываем ВСЕГДА, даже в prod
	fmt.Println(string(colorRed) + msg + string(colorReset))
}
