<div align="center">

![Header](https://capsule-render.vercel.app/api?type=waving&color=0:0f0c29,50:302b63,100:ff00cc&height=250&section=header&text=Nexus&fontSize=75&fontColor=ffffff&fontAlignY=35&desc=Local%20AI%20Agent&descAlignY=55&descSize=22)

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-BSL_1.1-orange?style=for-the-badge)](LICENSE)
[![Version](https://img.shields.io/badge/version-v1.8.0-ff00cc?style=for-the-badge&logo=github)](https://github.com/1Maniak/nexus/releases/tag/v1.8.0)
[![Website](https://img.shields.io/badge/Website-nexus--ai-blue?style=for-the-badge&logo=google-chrome&logoColor=white)](https://1maniak.github.io/nexus/)

**Локальный AI-агент с долговременной памятью, RAG-поиском и веб-интерфейсом.**
**Работает полностью офлайн, без облачных зависимостей.**

</div>

---

## 📖 О проекте

**Nexus** - это локальный AI-агент, разработанный для автономной работы. Все данные обрабатываются на устройстве пользователя, ничего не уходит в интернет. Это решение для тех, кто ценит приватность и независимость от зарубежных API.

Агент использует модель **Qwen 2.5 3B** через llama.cpp, хранит диалоги в **SQLite** с векторным поиском и общается с вами через удобный **веб-интерфейс**.

### 🌟 Ключевые возможности

| Фича | Описание |
|------|----------|
| 💬 **Веб-интерфейс** | Удобный чат в браузере со стримингом ответов, темной/светлой темой. |
| 🧠 **Долговременная память** | Все диалоги сохраняются в базе данных SQLite и не теряются. |
| 🔍 **RAG-поиск** | Семантический поиск по смыслу через эмбеддинги и косинусное сходство. |
| 🌐 **Интернет-поиск** | Получение актуальных данных через DuckDuckGo. |
| 📁 **Файлы** | Открытие файлов/папок в системе, чтение 8+ форматов (PDF, DOCX, CSV, TXT и др.). |
| 🤖 **Агентность** | Модель сама решает, когда использовать инструменты (открыть файл, найти в интернете, узнать время). |
| ⚡ **Автоматизация** | Автозапуск сервисов, открытие браузера и умное выключение при закрытии вкладки. |

---

## 🛠 Стек технологий

<div align="center">

![Go](https://img.shields.io/badge/Go-00ADD8?style=flat-square&logo=go&logoColor=white)
![Ollama](https://img.shields.io/badge/Ollama-000000?style=flat-square&logo=ollama&logoColor=white)
![SQLite](https://img.shields.io/badge/SQLite-003B57?style=flat-square&logo=sqlite&logoColor=white)
![HTML5](https://img.shields.io/badge/HTML5-E34F26?style=flat-square&logo=html5&logoColor=white)
![JavaScript](https://img.shields.io/badge/JavaScript-F7DF1E?style=flat-square&logo=javascript&logoColor=black)

</div>

---

## 🏗 Архитектура

```mermaid
flowchart TD
    U([Пользователь]) --> WEB[Веб-интерфейс :8081]
    WEB --> RAG[Поиск в памяти RAG]
    WEB --> TOOLS[Агентные инструменты]
    
    RAG --> DB[(SQLite)]
    RAG --> OLLAMA[Ollama Эмбеддинги]
    
    TOOLS --> DDG[DuckDuckGo]
    TOOLS --> OS[Файловая система]
    
    WEB --> LLM[llama.cpp / Qwen 2.5 3B]
    LLM --> WEB
```

---

## 🚀 Установка и запуск

### 📥 Вариант 1: Для обычных пользователей (Готовый релиз)

Если вы просто хотите пользоваться Nexus, вам **не нужно** ничего компилировать. 

1. Перейдите в раздел **[Releases](https://github.com/1Maniak/nexus/releases)**.
2. Скачайте последний ZIP-архив (например, `Nexus-v1.8.0-Windows.zip`).
3. Распакуйте архив в любую папку.
4. Внутри архива следуйте подробной инструкции из файла `README.txt` (там расписано, как установить Ollama, скачать модель и запустить программу).

---

### 🛠 Вариант 2: Для разработчиков (Сборка из исходников)

Если вы хотите изменить код, добавить свои фичи или просто собрать проект самостоятельно:

**Требования:**
- **Go 1.23+** - [Скачать](https://go.dev/dl/)
- **Ollama** - [Скачать](https://ollama.com/)

**Пошаговая установка:**

**1. Клонируйте репозиторий:**
```bash
git clone https://github.com/1Maniak/nexus.git
cd nexus-client
```

**2. Скачайте модель эмбеддингов:**
```bash
ollama pull nomic-embed-text
```

**3. Подготовьте llama.cpp:**
Создайте папку `llama.cpp` в корне проекта. Положите туда `llama-server.exe` (и все DLL файлы) и папку `models` с файлом `qwen2.5-3b-instruct-q4_k_m.gguf`.

**4. Соберите проект (используя батник):**
Просто запустите скрипт `build.bat` в корневой папке проекта (двойным кликом или через терминал).
Скрипт автоматически остановит старые запущенные процессы (если они висят), соберет `nexus-dev.exe` (с логами в консоли) и `nexus.exe` (тихую релизную версию).

*Ручная сборка (если не хотите использовать батник):*
- Для отладки: `go build -o nexus-dev.exe .`
- Для релиза: `go build -ldflags="-H windowsgui" -o nexus.exe .`

**5. Запустите:**
Запустите `nexus-dev.exe` (рекомендуется для первого раза, чтобы видеть логи) или `nexus.exe`. Программа сама поднимет сервер и откроет браузер на `http://localhost:8081`.

---

## 🕹 Использование

Благодаря агентному циклу, Nexus понимает контекст и сам вызывает нужные инструменты. Вам не нужно писать сложные команды, просто общайтесь с ним как с обычным помощником.

| Пример запроса | Действие Nexus |
|---------|----------|
| `Привет` | Обычный ответ текстом. |
| `Сколько времени?` | Использует инструмент `[TOOL: time]`. |
| `Открой папку C:\Projects` | Использует `[TOOL: open]` и запускает проводник. |
| `Прочитай файл C:\test.txt` | Использует `[TOOL: read]` и выводит содержимое в чат. |
| `Найди в интернете курс доллара` | Использует `[TOOL: search]` через DuckDuckGo. |
| `exit` | Выход из консоли (в dev-режиме). |

*Примечание: Модели 3B иногда сложно даются длинные пути с кириллицей. Для лучшей работы агента рекомендуется использовать версию 7B или мощнее.*

---

## 🗺 Roadmap

- [x] **v1.0.0** - Первая рабочая версия
- [x] **v1.5.0** - Векторный RAG
- [x] **v1.6.0** - Веб-интерфейс и оптимизация
- [x] **v1.7.0** - Стриминг, Drag & Drop, Агентные инструменты
- [ ] **v2.0.0** - Полноценный Tool Calling (JSON-схемы) и 7B модель
- [ ] **NetScope** - Диагностика сети

Подробнее в [docs/ROADMAP.md](docs/ROADMAP.md).

---

<details>
<summary><b>🇬🇧 Click to expand English version</b></summary>

## About

Nexus is a local AI agent designed for autonomous operation without cloud dependencies. All data is processed on your device. The agent uses Qwen 2.5 3B via llama.cpp, stores dialogs in SQLite with vector search, and communicates through a web interface.

## Features

- **Web Interface**: local chat in the browser with streaming, dark/light theme.
- **Long-term memory**: all dialogs stored in SQLite.
- **RAG search**: semantic search via embeddings and cosine similarity.
- **Web search**: fresh data via DuckDuckGo.
- **File operations**: open files/folders, read/write 8+ formats.
- **Agentic loop**: model decides when to use tools (open files, search web).
- **Automation**: auto-launch of services, smart shutdown when tab is closed.

## 🚀 Installation & Launch

### 📥 Option 1: For Regular Users (Ready Release)

If you just want to use Nexus, you **don't need** to compile anything.

1. Go to the **[Releases](https://github.com/1Maniak/nexus/releases)** page.
2. Download the latest ZIP archive (e.g., `Nexus-v1.8.0-Windows.zip`).
3. Extract the archive to any folder.
4. Inside the archive, follow the instructions in the `README.txt` file (it explains how to install Ollama, download the model, and run the program).

---

### 🛠 Option 2: For Developers (Build from Source)

If you want to modify the code, add your features, or just build the project yourself:

**Requirements:**
- **Go 1.23+** - [Download](https://go.dev/dl/)
- **Ollama** - [Download](https://ollama.com/)

**Step-by-step:**

**1. Clone the repository:**
```bash
git clone https://github.com/1Maniak/nexus.git
cd nexus-client
```

**2. Pull embedding model via Ollama:**
```bash
ollama pull nomic-embed-text
```

**3. Prepare llama.cpp:**
Create a `llama.cpp` folder in the project root. Place `llama-server.exe` (and all DLL files) and a `models` folder with `qwen2.5-3b-instruct-q4_k_m.gguf` inside.

**4. Build the project (using build.bat):**
Simply run the `build.bat` script in the project root (double-click or via terminal). 
The script will automatically stop old running processes (if any), compile `nexus-dev.exe` (with console logs), and `nexus.exe` (silent release version).

*Manual build:*
- For debug: `go build -o nexus-dev.exe .`
- For release: `go build -ldflags="-H windowsgui" -o nexus.exe .`

**5. Run:**
Run `nexus-dev.exe` (recommended for the first time to see logs) or `nexus.exe`. The program will start services and open the browser at `http://localhost:8081`.

## Usage

Thanks to the agentic loop, Nexus understands context and calls the necessary tools automatically. You don't need to write complex commands, just chat with it like a normal assistant.

| Example Request | Nexus Action |
|---------|----------|
| `Hello` | Standard text response. |
| `What time is it?` | Uses `[TOOL: time]`. |
| `Open folder C:\Projects` | Uses `[TOOL: open]` and launches Explorer. |
| `Read file C:\test.txt` | Uses `[TOOL: read]` and outputs content to chat. |
| `Search web for USD rate` | Uses `[TOOL: search]` via DuckDuckGo. |
| `exit` | Quits the console (in dev mode). |

*Note: 3B models sometimes struggle with long paths containing Cyrillic characters. For better agent performance, a 7B or larger model is recommended.*

## Roadmap

- [x] **v1.0.0** - First working version
- [x] **v1.5.0** - Vector RAG
- [x] **v1.6.0** - Web interface and optimization
- [x] **v1.7.0** - Streaming, Drag & Drop, Agentic tools
- [ ] **v2.0.0** - Full Tool Calling (JSON schemas) and 7B model
- [ ] **NetScope** - Network diagnostics

### License

Business Source License 1.1 (BSL 1.1). See [LICENSE](LICENSE) for details.

### Contacts

- GitHub: https://github.com/1Maniak
- Telegram: https://t.me/fullstack_life
- VK: https://vk.com/nexus_fullstack_life
- Boosty: https://boosty.to/1maniak

</details>

---

<div align="center">

### 📄 Лицензия

Проект распространяется под лицензией **Business Source License 1.1 (BSL 1.1)**. 

Вы можете свободно использовать Nexus в личных, учебных или внутренних коммерческих целях, изменять его и предлагать улучшения через Pull Requests. Однако использование кода для создания конкурирующего коммерческого продукта (SaaS) без разрешения автора запрещено. Через 4 года каждая версия автоматически переходит на лицензию GPLv3.

Подробнее в файле [LICENSE](LICENSE).

[![Footer](https://capsule-render.vercel.app/api?type=waving&color=0:ff00cc,50:302b63,100:0f0c29&height=100&section=footer)](https://github.com/1Maniak)

</div>