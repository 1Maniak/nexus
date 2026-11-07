package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

//  Эмбеддинги 
func getEmbedding(text string) ([]float64, error) {
	url := ollamaURL + "/api/embeddings"
	payload := map[string]interface{}{
		"model":  "nomic-embed-text",
		"prompt": text,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	embeddingRaw, ok := result["embedding"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("embedding не найден")
	}

	vec := make([]float64, len(embeddingRaw))
	for i, v := range embeddingRaw {
		val, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("неверный тип в эмбеддинге")
		}
		vec[i] = val
	}
	return vec, nil
}

// Формирование запроса к LLM 
func buildChatPayload(messages []map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"model":             llmModel,
		"messages":          messages,
		"stream":            true,
		"temperature":       0.5,
		"top_k":             40,
		"top_p":             0.85,
		"repeat_penalty":    1.25,
		"frequency_penalty": 0.35,
		"presence_penalty":  0.2,
		"max_tokens":        maxGeneratedTokens,
	}
}

// Потоковый вывод ответа 
func streamChatCompletion(url string, payload map[string]interface{}) (string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	fmt.Print("Nexus: ")
	fullResponse := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if !bytes.HasPrefix([]byte(line), []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace([]byte(line[5:]))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue
		}
		choices, ok := chunk["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := delta["content"].(string)
		if !ok {
			continue
		}
		fmt.Print(content)
		fullResponse += content
	}
	fmt.Println()
	return fullResponse, nil
}

// Сборка сообщений для запроса 
func buildRequestMessages(history []map[string]string, extraSystem, userInput string) []map[string]string {
	request := make([]map[string]string, len(history))
	copy(request, history)
	if extraSystem != "" {
		request = append(request, map[string]string{
			"role":    "system",
			"content": extraSystem,
		})
	}
	request = append(request, map[string]string{
		"role":    "user",
		"content": userInput,
	})
	return request
}
