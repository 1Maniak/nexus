package main

import "time"

func getCurrentTime() string {
	now := time.Now()
	return now.Format("15:04:05 - 02.01.2006")
}

func splitIntoChunks(text string, chunkSize int) []string {
	var chunks []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

func trimConversation(messages []map[string]string, keepNonSystem int) []map[string]string {
	if len(messages) <= 1 {
		return messages
	}
	system := messages[0]
	rest := messages[1:]
	if len(rest) <= keepNonSystem {
		return messages
	}
	return append([]map[string]string{system}, rest[len(rest)-keepNonSystem:]...)
}

func recordTurn(messages *[]map[string]string, userInput, response string) {
	*messages = append(*messages,
		map[string]string{"role": "user", "content": userInput},
		map[string]string{"role": "assistant", "content": response},
	)
	*messages = trimConversation(*messages, maxHistoryMessages)
}
