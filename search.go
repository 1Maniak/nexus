package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func webSearch(query string) (string, error) {
	query = strings.ReplaceAll(query, " ", "+")
	url := "https://html.duckduckgo.com/html/?q=" + query

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.8,en-US;q=0.5,en;q=0.3")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка запроса: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 202 {
		printSystem("Сервер обрабатывает запрос, повтор через 1 сек...")
		time.Sleep(1 * time.Second)
		return webSearch(query)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга: %v", err)
	}

	var results []string
	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if i >= 5 {
			return
		}
		title := s.Find(".result__title").Text()
		link, _ := s.Find(".result__title a").Attr("href")
		desc := s.Find(".result__snippet").Text()
		if title != "" {
			results = append(results, fmt.Sprintf("%d. %s\n   %s\n   %s", i+1, title, desc, link))
		}
	})

	if len(results) == 0 {
		printSystem("Ничего не найдено.")
		return "", fmt.Errorf("ничего не найдено по запросу")
	}

	printSystem(fmt.Sprintf("Найдено %d результатов.", len(results)))
	return strings.Join(results, "\n\n"), nil
}
