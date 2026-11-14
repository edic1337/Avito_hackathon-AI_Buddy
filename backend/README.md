# Go backend: эмбеддинги и RAG

HTTP-обработчик `POST /question`, авторизация и подключение к frontend:
[FrontendAPI/README.md](FrontendAPI/README.md).

Модуль `GO` (имя существующего go.mod сохранено), Go 1.27+. Разделение:

- `rag.HTTPEmbedder`: HTTP-инференс Qwen, батчи, нормализация 1024-мерных векторов;
- `rag.Store`: PostgreSQL/pgvector, загрузка, импорт, гибридный поиск и ACL;
- `rag.Retriever`: вопрос → вектор → `[]string` → существующий `ModelAPI`;
- `cmd/rag`: административный CLI для воспроизводимого запуска;
- `specs/rag.md`: контракт поведения и проверки.

В Go нет доменной структуры Chunk. Для загрузки передаются метаданные источника
`Document` и уже разделённые `[]string`. Поиск возвращает `[]string`: каждая строка
содержит найденный текст, название, раздел (при импорте), происхождение и ссылку.
SQL-таблица `rag.chunks` сохранена для совместимости с DDL ветки `RAG`.

## Быстрый запуск

Из корня скопировать `.env.example` в `.env`, затем перейти в `backend`.
Настоящий инференс выполняется отдельным сервисом, в Go не загружаются torch/веса.
Для Docker Desktop с Linux containers:

```sh
cd backend
docker compose -f compose.rag.yml up -d
docker compose -f compose.rag.yml logs -f embedder
```

Первый запуск скачивает веса Qwen, занимает время и требует интернета и памяти.
Дождитесь готовности embedder; `curl http://localhost:8081/health` должен вернуть
успех. PostgreSQL слушает localhost:5432, embedder — localhost:8081. Если порт БД
занят, измените порт в compose и `DATABASE_URL`. Том БД и кеш модели сохраняются.
Compose предназначен для локального демо, а не для публичного размещения.

```sh
go mod download
go run ./cmd/rag migrate
go run ./cmd/rag ingest -file examples/document.json
go run ./cmd/rag search -question "Как получить VPN?" -role intern
```

`search` печатает JSON-массив строк и не требует ключа LLM. Для ответа модели
заполните `OPENROUTER_API_KEY`, `OPENROUTER_API_URL` (полный chat-completions URL)
и `OPENROUTER_MODEL`, затем:

```sh
go run ./cmd/rag ask -question "Как получить VPN?" -role intern
```

CLI читает `../.env`, не перекрывая уже установленные переменные процесса.
Другой файл: `-env /path/to/.env`. Общий таймаут: `-timeout 3m`.
Вызов библиотеки сам `.env` не загружает. Миграции выполняются явно пользователем
с правами DDL; обычные запросы не создают extension или таблицы.

## Подключение к серверному коду

Создайте один пул, Store и HTTPEmbedder при старте сервера. Передавайте request
context во все операции. Не создавайте пул на каждый вопрос.

```go
config, err := rag.ConfigFromEnv()
if err != nil { return err }
pool, err := pgxpool.New(ctx, config.DatabaseURL)
if err != nil { return err }
defer pool.Close() // В сервере — при завершении приложения.
store, err := rag.OpenStore(ctx, pool, config) // Миграции уже применены.
if err != nil { return err }
embedder, err := rag.NewHTTPEmbedder(config)
if err != nil { return err }
retriever, err := rag.NewRetriever(embedder, store)
if err != nil { return err }

// role/team — из доверенной авторизации backend, не из JSON фронтенда.
documents, err := retriever.Retrieve(ctx, question, rag.Access{Role: role, Team: team})
if err != nil { return err }
answer, err := ModelAPI.AskWithRAGContext(ctx, question, documents)
// Или один вызов: retriever.Answer(ctx, question, access).
```

Импорты: `GO/rag`, `GO/ModelAPI`, `github.com/jackc/pgx/v5/pgxpool`.
Старые `ModelAPI.Ask` и `AskWithRAG(question, []string)` продолжают работать.
В HTTP-обработчиках используйте варианты с context для отмены и дедлайнов.

Индексация результата существующего разделения документов:

```go
id, err := store.SaveDocument(ctx, rag.Document{
    SourceKey: "demo/vpn", Title: "VPN", Text: originalText,
    SourceURL: "demo://vpn", Origin: "synthetic", MinRole: "intern",
}, preparedTexts, embedder) // preparedTexts — []string, без заголовочного префикса.
```

К каждой строке добавляется `Title + "\n"` перед вычислением вектора. Разделение,
перекрытия и лимиты токенов выполняет upstream RAG. Для потока с другим способом
подготовки задайте честный `RAG_CHUNKING_CONFIG` и отдельную `RAG_COLLECTION`.
Сервер эмбеддингов должен отклонять слишком длинный вход, не обрезать его молча.

Один `SourceKey` обозначает один исходный документ в коллекции. Повторная загрузка
сохраняет его ID и полностью заменяет тексты в транзакции; дублей и старых хвостов
не остаётся. Пересчёт эмбеддингов при повторной загрузке пока выполняется всегда,
ID отдельных SQL-фрагментов не стабильны. Метаданные/права обновляются вместе
с текстами. Ошибка любой вставки откатывает всё, включая изменение прав.
Отключение: `store.DeactivateDocument(ctx, sourceKey)`; повторная загрузка включает
документ обратно. Это административные операции, не публичные пользовательские API.

## Импорт готового экспорта ветки RAG

Файл `embeddings.json` schema_version=1 уже содержит векторы; embedder для импорта
не нужен. Сохраните файл из ветки `RAG` в UTF-8, не меняя текущую ветку.
Например в PowerShell 7 из корня репозитория:

```powershell
git show origin/RAG:embeddings.json | Set-Content -Encoding utf8 embeddings.json
cd backend
$env:RAG_COLLECTION = 'demo-qwen-v1'
go run ./cmd/rag import -file ../embeddings.json -source-key playbook/ds-skills.md
```

Проверяются schema_version, модель, revision, размерность и профиль chunking,
позиции, положительный token_count и ненулевые конечные векторы. Нормализация
повторяется в float32. Точный token_count берётся из экспорта; Go не проверяет его
токенизатором. Для `SaveDocument([]string)` он неизвестен и хранится как NULL:
миграция 002 снимает NOT NULL, сохраняя проверку положительности известных значений.
Разделы, source_url, synthetic/public, min_role, team и tags сохраняются.

Старый экспорт не содержит revision весов: оставьте `EMBEDDING_REVISION` пустым
и используйте демонстрационную коллекцию. Совпадение имени модели не доказывает
совпадение весов/пулинга разных серверов. Для воспроизводимости зафиксируйте
revision в сервере и экспорте, затем переиндексируйте в новую коллекцию.
Профиль существующей коллекции нельзя молча изменить через OpenStore.

## Поиск и доступы

1. Qwen query-префикс совпадает с `prompt_name="query"` из конфигурации модели.
   Документы отправляются без query-префикса. Endpoint не должен добавлять его
   второй раз; `EMBEDDING_MODEL` должен обозначать совместимый Qwen checkpoint.
2. В SQL сначала отбираются активные документы одной коллекции по роли и команде.
   Роли строго `intern < buddy < manager`, неизвестная роль — ошибка. Пустая
   команда пользователя даёт доступ только к документам без ограничения команды.
   Manager не получает доступ к другой команде автоматически.
3. Exact cosine search + FTS `russian`, кандидаты объединяются через RRF, k=60.
   Каждый возвращённый текст должен пройти `MIN_SIMILARITY` по cosine; FTS/RRF
   не сравниваются с этим порогом. Значение 0.45 стартовое, требует eval-набора.
4. Возвращается до `TOP_K` строк. `RAG_MAX_CONTEXT_RUNES` — бюджет символов Unicode,
   не токенов. Слишком большой фрагмент пропускается целиком, не обрезается посреди
   инструкции. Бюджет не включает system prompt и вопрос.
5. Пустой результат → фиксированный ответ про недостаток информации и наставника,
   без обращения к LLM. Ошибки сети/БД/LLM возвращаются вызывающему коду.

Фильтрация реализована на уровне SQL приложения, не PostgreSQL RLS. Отдельный
endpoint чтения документов, если команда его добавит, тоже должен проверять ACL.
CLI `-role/-team` предназначены для доверенного локального демо.
Синтетическое происхождение явно отмечается в строках контекста, правила LLM
передаются system-сообщением, источники — отдельными данными. Это снижает риск
prompt injection, но не является доказательством защиты модели от всех атак.

## Тесты

```sh
go test ./...
go vet ./...
```

Unit-тесты не загружают модель и используют HTTP-заглушки. Для SQL-тестов нужна
**отдельная тестовая БД** с pgvector; задайте `TEST_DATABASE_URL`. Интеграционные
тесты создают отдельные коллекции и удаляют свои записи, применяют миграции.
Без этой переменной они пропускаются. Пример PowerShell:

```powershell
$env:TEST_DATABASE_URL = 'postgresql://postgres:postgres@localhost:5432/avito_buddy_test'
go test -count=1 '-coverprofile=coverage.out' ./...
go tool cover '-func=coverage.out'
go tool cover '-html=coverage.out' '-o=coverage.html'
```

БД `avito_buddy_test` нужно создать заранее. На поддерживаемой системе с C compiler
дополнительно: `go test -race ./...`. SQL-тесты проверяют роли/команды, FTS/RRF,
порог, повторную загрузку, замену, rollback, конкурентную запись, импорт и коллекции.
Отчёт о фактически выполненных проверках: [VALIDATION.md](VALIDATION.md).
Семантическое качество настоящей модели (recall@k/MRR) этими тестами не измеряется.

## Границы этой задачи

Реализованы библиотека Go, CLI и HTTP-слой `FrontendAPI` с Bearer-авторизацией
для демо. Запуск HTTP-сервера из общего main и корпоративная авторизация,
SSE, frontend, чек-лист адаптации, mock корпоративных действий и reranker
остаются интеграцией команды.
SQL миграции адаптированы из `origin/RAG:postgres/001_rag_schema.sql`.
Python-чанкер не переносился и ветка RAG не изменялась.

Проверенные источники контрактов:
[Qwen query prompt](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B/blob/main/config_sentence_transformers.json),
[TEI OpenAI endpoint](https://huggingface.co/docs/text-embeddings-inference/quick_tour),
[pgvector](https://github.com/pgvector/pgvector),
[pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool).
