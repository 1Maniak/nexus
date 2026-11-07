# Architecture — Nexus

Здесь собраны схемы архитектуры Nexus по версиям.  
Формат основан на Keep a Changelog, проект придерживается Semantic Versioning.

---

## [v1.6.0] — 2026-11-07

### Схема обработки запроса

```mermaid
flowchart TD
    U([Пользователь]) --> WEB[Веб-интерфейс :8081]
    WEB --> RAG[Поиск в памяти RAG]
    WEB --> WEBSEARCH[Поиск в интернете]
    
    RAG --> DB[(SQLite)]
    RAG --> OLLAMA[Ollama Эмбеддинги]
    
    WEBSEARCH --> DDG[DuckDuckGo]
    
    WEB --> LLM[llama.cpp / Qwen 2.5 3B]
    LLM --> WEB
```

### Компоненты системы

```mermaid
graph LR
    subgraph Ядро_Nexus
        WEB[web.go<br/>HTTP Server]
        MAIN[main.go<br/>Core Logic]
        LAUNCH[launcher.go<br/>Auto-start Services]
        CLEAN[cleanup.go<br/>Process Cleanup]
    end

    subgraph Внешние_сервисы
        OLLAMA[Ollama :11434<br/>nomic-embed-text]
        QWEN[llama-server :8080<br/>qwen2.5-3b]
        DDG[DuckDuckGo HTML]
    end

    subgraph Хранилище
        DB[(nexus_memory.db)]
        BASE[папка base/]
    end

    WEB --> MAIN
    LAUNCH --> QWEN
    LAUNCH --> OLLAMA
    MAIN --> DDG
    MAIN --> OLLAMA
    MAIN --> DB
    BASE --> DB
    CLEAN -.-> QWEN
```

---

## [v1.5.1] — 2026-09-11

### Схема обработки запроса (Консольная версия)

```mermaid
flowchart TD
    U([Пользователь]) --> IN[Ввод текста]
    IN --> EXIT{exit?}
    EXIT -- Да --> END([Выход])
    EXIT -- Нет --> TIME{Запрос времени?}
    TIME -- Да --> RT[getCurrentTime] --> SAVE
    TIME -- Нет --> FILE{Команды файлов?}
    FILE -- Да --> FOP[os.ReadFile / WriteFile] --> SAVE
    FILE -- Нет --> SEARCH{Интернет-поиск?}
    SEARCH -- Да --> WS[webSearch DuckDuckGo] --> LLM
    SEARCH -- Нет --> RAG[searchMemory → searchByVector]
    RAG --> LLM[streamChatCompletion]
    LLM --> SAVE[addToMemory]
    SAVE --> U
```

---

## [v1.5.0] — 2026-08-12

### Ключевое изменение: векторный RAG

Переход от FTS5 (поиск по словам) к косинусному сходству (поиск по смыслу).

---

## [v1.0.0] — 2026-07-11

### Первая рабочая версия

- Модель: **Qwen2.5-Coder-7B-Instruct** через llama-server.
- Память: **JSON** (`memory.json`).
- Поиск: **косинусное расстояние** между эмбеддингами.