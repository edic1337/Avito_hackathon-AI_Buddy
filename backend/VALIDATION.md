# Проверки реализации

## Повторная проверка после добавления FrontendAPI

14 ноября 2026: нативные `go test -count=1 ./...`, `go build ./...` и
`go vet ./...` успешно выполнены. В этом прогоне блокировки Windows Application
Control уже не было. `TEST_DATABASE_URL` не задан, поэтому SQL integration-тесты
пропущены; внешние модели не использовались. Покрытие нового HTTP-пакета
FrontendAPI — 95,5% операторов, измерено `go test -coverprofile` и `go tool cover`.
Команды воспроизведения и контракт запросов находятся в FrontendAPI/README.md.

Ниже сохранены результаты первоначальной проверки RAG, выполненной до этих изменений.

Дата: 14 ноября 2026. Ветка: Backend. Go 1.27.4, Windows/amd64.

| Проверка | Результат |
| --- | --- |
| `go build ./...` | Успешно |
| `go vet ./...` | Успешно |
| Unit-тесты `rag`, сборка js/wasm и запуск Node | 8 тестовых функций с подслучаями прошли |
| Unit-тесты `ModelAPI`, js/wasm и Node | 3 тестовые функции с подслучаями прошли |
| Миграции и SQL через PGlite + pgvector | Прошли 11 групп проверок, включая загрузку 50 реальных векторов из ветки RAG |
| Нативный `go test ./...` | Тесты компилируются, запуск `.test.exe` блокируется Windows Application Control |
| `go test -coverprofile=coverage.out ./...` | Windows блокирует `cover.exe`; достоверного отчёта покрытия нет |
| Нативные pgx integration/race tests | Не выполнены из-за блокировки тестовых бинарников |
| Docker compose, настоящий HTTP-инференс Qwen, OpenRouter | Не запускались; модели и внешние ключи не использовались |

Unit-тесты проверяют HTTP JSON/заголовки, порядок по index, батчи, Qwen query
префикс, нормализацию, некорректные ответы, таймаут/отмену, конфигурацию, импорт,
отказ при неизвестной роли, пустой контекст без LLM и формат сообщений модели.
В WASM используется штатная внутренняя сеть Go для httptest. Это исполнение Go
тестов на другом target, а не проверка Windows networking или pgx wire protocol.

SQL-проверки используют PGlite 0.5.16 и pglite-pgvector 0.0.17, извлекают запросы
из `rag/store.go` и `rag/search.go`, выполняют обе миграции дважды. Проверены:
профиль коллекции, повторный upsert без дублей, ранжирование RRF, role/team ACL,
cosine threshold, candidate limit, rollback после SQL-ошибки, деактивация, импорт
и поиск по всем 50 векторам из `origin/RAG:embeddings.json`.
Это не заменяет запуск pgx-тестов с настоящим PostgreSQL и конкурентными соединениями.

## Повторение ограниченных проверок на Windows

Обычный путь и команды покрытия находятся в README. Если нативное выполнение
запрещено политикой хоста, можно отдельно повторить unit-тесты:

```powershell
# Из backend, Go и Node должны находиться в PATH.
$ragGoRoot = go env GOROOT
$env:GOOS = 'js'
$env:GOARCH = 'wasm'
go test -c -o rag.test.wasm ./rag
go test -c -o modelapi.test.wasm ./ModelAPI
node scripts/run-wasm.cjs "$ragGoRoot/lib/wasm/wasm_exec_node.js" rag.test.wasm '-test.v' '-test.timeout=30s'
node scripts/run-wasm.cjs "$ragGoRoot/lib/wasm/wasm_exec_node.js" modelapi.test.wasm '-test.v' '-test.timeout=30s'
Remove-Item Env:GOOS, Env:GOARCH
```

Для SQL smoke test достаточно Node, npm и локального ref `origin/RAG`:

```powershell
$ragSqlRuntime = Join-Path $env:TEMP 'avito-rag-pgtest'
npm install --prefix $ragSqlRuntime --ignore-scripts @electric-sql/pglite@0.5.16 @electric-sql/pglite-pgvector@0.0.17
node scripts/verify-sql.cjs $ragSqlRuntime
```

Пакеты Node нужны только для этого дополнительного теста; рабочий Go backend
их не использует. Тест создаёт БД в памяти и не подключается к рабочей PostgreSQL.
Воспроизводимая цель для команды перед сдачей: запустить стандартный `go test`
с `TEST_DATABASE_URL`, измерить покрытие (требование кейса >80%) и проверить
полный сценарий на настоящих сервисах. Процент покрытия здесь не заявляется.

## Проверка на ветке Date: реальная PostgreSQL

14 ноября 2026 выполнена отдельная проверка по задаче БД:

| Проверка | Результат |
| --- | --- |
| PostgreSQL с pgvector | `pgvector/pgvector:0.8.1-pg17`, БД `avito_buddy`, healthcheck прошёл |
| Порт хоста | `127.0.0.1:65432`; публикация на 5432 и 55432 запрещена политикой Windows |
| `go run ./cmd/rag migrate` | обе миграции выполнены в Go 1.27.4 контейнере; созданы `rag.collections`, `rag.documents`, `rag.chunks`; `token_count` допускает NULL |
| Права `avito_buddy_app` | connect, usage схемы, DML трёх таблиц и USAGE identity-последовательностей подтверждены запросами к PostgreSQL |
| Go CLI `import` | тестовый экспорт загружен приложенческой ролью; первая и повторная загрузки вернули document ID `1` |
| Итог данных | один документ `playbook/ds-skills.md`, 50 чанков, metadata `synthetic` / `intern` / `team=NULL` |

Go CLI запускался во временном Go 1.27.4 контейнере, потому что Go и `psql` не
установлены на хосте. Полный набор Go unit/integration-тестов на этой ветке
отдельно не запускался.

## Импорт полных корпусов schema_version=2

15 ноября 2026: ветки `origin/Backend` и `origin/RAG` объединены локально в `Date`.
Локальные JSON в `embeddings/` не включены в Git.

| Проверка | Результат |
| --- | --- |
| Локальная PostgreSQL | PostgreSQL 17.8, pgvector 0.8.1, база `avito_buddy`, localhost:65432 |
| Миграции | Go CLI применил базовую схему, nullable token count, `origin=provided` и `documents.metadata` |
| Предварительная проверка экспорта | Успешно: 871 документ, 16 413 чанков; структура, профили, хеши, позиции и векторы прошли проверку |
| Импорт | 827 public Habr документов (15 633 чанка) + 44 provided документа (780 чанков) |
| Векторы и FTS | `invalid_vectors=0`, нормы 0.99999997–1.00000003, `missing_fts=0` |
| Дубликаты | Повторяющихся `source_key` и пар `(source_key, position)` нет |
| Метаданные и ACL | 44 документа `provided`, остальные `public`; все `intern`, `team=NULL`; metadata периода и source_record читаются |
| Права `avito_buddy_app` | SELECT/INSERT для таблиц и USAGE для identity-последовательности подтверждены |
| Cosine smoke check | Самосовпадение similarity=1.0; это проверка оператора, не оценка качества retrieval |

Импорт выполнен без повторного инференса. Полный Go test suite после добавления
`import-corpus` не запускался; для компиляции и загрузки команда `go run` прошла.
Публичный сетевой доступ к PostgreSQL не настраивался: база оставлена локальной.
