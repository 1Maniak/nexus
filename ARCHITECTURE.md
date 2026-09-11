# Architecture — Nexus

Здесь собраны схемы архитектуры Nexus по версиям.  
Формат основан на Keep a Changelog, проект придерживается Semantic Versioning.

---

## [v1.5.1] — 2026-09-11

### Схема обработки запроса

```mermaid
flowchart TD
    U([Пользователь]) --> IN[Ввод текста]
    IN --> EXIT{exit?}
    EXIT -- Да --> END([Выход])
    EXIT -- Нет --> TIME{Запрос времени?}

    TIME -- Да --> RT[getCurrentTime] --> SAVE
    TIME -- Нет --> FILE{Команды файлов?}

    FILE -- Чтение --> RF[os.ReadFile]
    RF --> PR[Вывод содержимого] --> SAVE
    FILE -- Запись --> WF[os.WriteFile]
    WF --> OK[Подтверждение] --> SAVE
    FILE -- Нет --> SEARCH{Интернет-поиск?}

    SEARCH -- Да --> WS[webSearch DuckDuckGo]
    WS --> EN[extractNumber]
    EN --> CTX[contextMsg + результат]
    CTX --> LLM
    SEARCH -- Нет --> RAG[searchMemory → searchByVector]

    RAG --> EMB[getEmbedding запроса]
    EMB --> SCAN[SELECT text, vector FROM memory]
    SCAN --> COS[cosineSimilarity для каждой]
    COS --> SORT[Сортировка + топ-3]
    SORT --> RAGC[ragContext]
    RAGC --> LLM

    LLM[streamChatCompletion<br/>Qwen через Ollama] --> STREAM[Потоковый вывод]
    STREAM --> REC[recordTurn + trimConversation]
    REC --> SAVE[addToMemory: user + assistant]
    SAVE --> U
```

### Жизненный цикл запроса

```mermaid
sequenceDiagram
    participant П as Пользователь
    participant N as Nexus
    participant O as Ollama
    participant L as llama-server
    participant D as SQLite
    participant W as DuckDuckGo

    П->>N: Ввод
    N->>N: exit? → break
    N->>N: время / файлы / поиск

    alt Интернет-поиск
        N->>W: webSearch(query)
        W-->>N: HTML с результатами
        N->>N: extractNumber
        N->>N: contextMsg
    else Обычный диалог
        N->>O: getEmbedding(userInput)
        O-->>N: queryVec
        N->>D: SELECT text, vector FROM memory
        D-->>N: все записи
        N->>N: cosineSimilarity + sort + top-3
        N->>N: ragContext
    end

    N->>L: POST /v1/chat/completions (stream)
    L-->>N: потоковые токены
    N-->>П: вывод в консоль
    N->>D: INSERT user + assistant
```

### Компоненты системы

```mermaid
graph LR
    subgraph Ядро_Nexus
        MAIN[main loop]
        TIME[getCurrentTime]
        FILE[file ops: ReadFile / WriteFile]
        SEARCH[webSearch]
        RAG[searchByVector]
        LLM[streamChatCompletion]
    end

    subgraph Внешние_сервисы
        OLLAMA[Ollama :11434<br/>nomic-embed-text]
        QWEN[llama-server :8080<br/>qwen2.5-3b]
        DDG[DuckDuckGo HTML]
    end

    subgraph Хранилище
        DB[(nexus_memory.db<br/>memory + memory_fts)]
        STATE[knowledge_state.json]
        BASE[папка base/]
    end

    TIME --> MAIN
    FILE --> MAIN
    SEARCH --> DDG
    RAG --> OLLAMA
    RAG --> DB
    LLM --> QWEN
    BASE --> STATE
    BASE --> DB
```

### Маршрутизация запроса

```mermaid
stateDiagram-v2
    [*] --> Ввод
    Ввод --> Выход: exit
    Ввод --> Время: время/часы
    Ввод --> Файлы: прочитай/запиши/открой
    Ввод --> Поиск: !поиск/найди/ключевые слова
    Ввод --> RAG: иначе
    Время --> Сохранение
    Файлы --> Сохранение
    Поиск --> LLM: contextMsg
    RAG --> LLM: ragContext
    LLM --> Сохранение
    Сохранение --> Ввод
    Выход --> [*]
```

### Структура памяти (SQLite)

```mermaid
erDiagram
    MEMORY {
        TEXT id PK
        TEXT text
        BLOB vector
        TEXT type
    }
    MEMORY_FTS {
        TEXT text
    }
    MEMORY ||--|| MEMORY_FTS : "rowid sync"
```

### Жизненный цикл базы знаний

```mermaid
flowchart TD
    BASE[папка base/] --> WALK[WalkDir по файлам]
    WALK --> HASH[computeFileHash]
    HASH --> CHECK{hash совпал?}
    CHECK -- Да --> CACHE[взять из knowledge_state.json]
    CHECK -- Нет --> READ[readFileContent<br/>PDF/DOCX/CSV/HTML/TXT]
    READ --> SPLIT[splitIntoChunks 800 символов]
    SPLIT --> EMB[getEmbedding для каждого чанка<br/>5 горутин]
    EMB --> INSERT[INSERT в SQLite<br/>type=knowledge]
    INSERT --> SAVE[saveKnowledgeState]
    CACHE --> SAVE
    SAVE --> DONE([Готово])
```

---

## [v1.5.0] — 2026-08-12

### Ключевое изменение: векторный RAG

Переход от FTS5 (поиск по словам) к косинусному сходству (поиск по смыслу).

### Схема обработки запроса

```mermaid
flowchart TD
    U([Пользователь]) --> IN[Ввод текста]
    IN --> TIME{Время?}
    TIME -- Да --> RT[getCurrentTime]
    TIME -- Нет --> FILE{Файлы?}
    FILE -- Да --> FOP[Чтение / запись]
    FILE -- Нет --> SEARCH{Поиск?}
    SEARCH -- Да --> WS[webSearch]
    WS --> LLM
    SEARCH -- Нет --> RAG[searchByVector<br/>косинусное сходство]
    RAG --> LLM[streamChatCompletion<br/>Qwen + Ollama]
    LLM --> U
```

---

## [v1.4.1] — 2026-08-08

### Ключевое изменение: стабилизация

- Управление файловой системой переработано.
- Агентный цикл временно удалён.
- Системный промпт обновлён.

### Схема обработки запроса

```mermaid
flowchart TD
    U([Пользователь]) --> IN[Ввод текста]
    IN --> FTS[searchMemorySQLite<br/>FTS5-поиск]
    FTS --> LLM[streamChatCompletion<br/>Qwen 2.5-3B]
    LLM --> U
```

---

## [v1.4.0] — 2026-08-08

### Ключевые изменения

- Переход на Qwen2.5-3B.
- GPU-ускорение через Vulkan.
- Полноценная работа с файлами: чтение, запись, добавление, открытие.
- Команда `!последний`.

### Схема обработки запроса

```mermaid
flowchart TD
    U([Пользователь]) --> IN[Ввод текста]
    IN --> FTS[searchMemorySQLite<br/>FTS5]
    FTS --> LLM[streamChatCompletion<br/>Qwen 2.5-3B]
    LLM --> U
```

---

## [v1.3.0] — 2026-08-08

### Ключевое изменение: начало тестирования Qwen2.5-3B

### Схема обработки запроса

```mermaid
flowchart LR
    U([Пользователь]) --> FTS[searchMemorySQLite<br/>FTS5]
    FTS --> LLM[Qwen 2.5-3B]
    LLM --> U
```

---

## [v1.2.0] — 2026-08-08

### Ключевое изменение: закрепление идентичности агента

- Улучшен системный промпт.
- Инициализирован Git-репозиторий.
- Добавлен CHANGELOG.

### Схема обработки запроса

```mermaid
flowchart LR
    U([Пользователь]) --> FTS[searchMemorySQLite<br/>FTS5]
    FTS --> LLM[Qwen2.5-Coder-7B]
    LLM --> U
```

---

## [v1.1.0] — 2026-08-05

### Ключевые изменения

- **Переход с JSON на SQLite** (`nexus_memory.db`).
- Полнотекстовый поиск **FTS5**.
- Интернет-поиск через **DuckDuckGo**.
- Автоматическое определение поисковых запросов по ключевым словам.
- Команда `!поиск`.
- Извлечение чисел из результатов поиска.
- Скрипт `start.bat` для автозапуска.

### Схема обработки запроса

```mermaid
flowchart TD
    U([Пользователь]) --> Q{Поиск?}
    Q -- Да --> W[webSearch<br/>DuckDuckGo]
    Q -- Нет --> DB[(SQLite FTS5<br/>nexus_memory.db)]
    W --> LLM[streamChatCompletion<br/>Qwen2.5-Coder-7B]
    DB --> LLM
    LLM --> U
```

### Структура хранилища

```mermaid
flowchart TD
    BASE[папка base/] --> READ[чтение файлов]
    READ --> SPLIT[чанки]
    SPLIT --> EMB[Ollama эмбеддинги]
    EMB --> DB[(SQLite<br/>memory + memory_fts)]
    DIALOG[диалоги] --> DB
```

---

## [v1.0.0] — 2026-07-11

### Первая рабочая версия

- Модель: **Qwen2.5-Coder-7B-Instruct (Q5_K_M)** через llama-server.
- Память: **JSON** (`memory.json`).
- Поиск: **косинусное расстояние** между эмбеддингами.
- База знаний из папки `base`.
- Инкрементальное обновление через хеши (`knowledge_state.json`).

### Схема обработки запроса

```mermaid
flowchart TD
    U([Пользователь]) --> EMB[getEmbedding<br/>Ollama + nomic-embed-text]
    EMB --> COS[cosineSimilarity]
    JSON[(memory.json<br/>диалоги + знания)] --> COS
    COS --> LLM[streamChatCompletion<br/>Qwen2.5-Coder-7B]
    LLM --> JSON
    LLM --> U
```

### Структура хранилища

```mermaid
flowchart TD
    BASE[папка base/] --> READ[чтение файлов<br/>txt md go json pdf docx csv html]
    READ --> SPLIT[чанки]
    SPLIT --> EMB[Ollama эмбеддинги]
    EMB --> JSON[(memory.json)]
    DIALOG[диалоги] --> JSON
    HASH[computeFileHash MD5] --> STATE[knowledge_state.json]
    STATE --> JSON
```