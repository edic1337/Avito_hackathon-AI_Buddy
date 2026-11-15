# PostgreSQL для RAG

Go backend уже содержит схему в двух миграциях:

- `backend/rag/migrations/001_rag_schema.sql` создаёт `public.vector`, схему
  `rag`, таблицы `collections`, `documents`, `chunks`, связи и индексы.
- `backend/rag/migrations/002_optional_token_count.sql` разрешает `NULL` в
  `chunks.token_count` для Go-загрузки `[]string`, где токенизатор не известен.

Не создавайте таблицы вручную и не добавляйте доменную структуру чанка в Go.
Для Go публичный интерфейс остаётся `[]string`; `rag.chunks` — внутренняя
таблица хранения.

## Локальный запуск

Из корня репозитория поднимите только PostgreSQL; для импорта готовых векторов
сервис эмбеддингов не нужен:

```powershell
$env:RAG_DB_PORT = '65432' # используйте 5432, если этот порт разрешён и свободен
docker compose -f backend/compose.rag.yml up -d postgres
```

Локальный compose задаёт базу `avito_buddy`, привязывает выбранный порт только к
`127.0.0.1` и создаёт административного пользователя `postgres` с
демо-паролем `postgres`. Не публикуйте этот пароль и не используйте эту роль
как учётную запись backend.

Примените обе миграции через Go CLI из `backend`:

```powershell
$env:DATABASE_URL = 'postgresql://postgres:postgres@127.0.0.1:65432/avito_buddy?sslmode=disable'
go run ./cmd/rag migrate
```

## Учётная запись приложения

После миграций, от имени владельца схемы/миграционного пользователя выполните
из корня:

```powershell
Get-Content -Raw backend/scripts/grant-rag-app.sql | docker compose -f backend/compose.rag.yml exec -T postgres psql -U postgres -d avito_buddy -v ON_ERROR_STOP=1
docker compose -f backend/compose.rag.yml exec postgres psql -U postgres -d avito_buddy
# В открывшемся psql задайте скрытый пароль и выйдите:
\password avito_buddy_app
\q
```

Задайте пароль роли интерактивной командой `\password avito_buddy_app` в
`psql`. Не записывайте реальный пароль в репозиторий. Скрипт выдаёт роли
`avito_buddy_app` подключение к базе, `USAGE` на `rag`, чтение/запись/обновление/
удаление таблиц, доступ к identity-последовательностям и типу `public.vector`.
Права по умолчанию нужны для будущих таблиц и последовательностей, созданных
тем же миграционным пользователем.

Для локального демо параметры приложения:

| Параметр | Значение |
| --- | --- |
| Host | `127.0.0.1` |
| Port | `65432` в этом окружении; настраивается через `RAG_DB_PORT` |
| Database | `avito_buddy` |
| User | `avito_buddy_app` |
| Password | задаётся отдельно через `\password` |
| TLS | `sslmode=disable` только для локального compose |

`DATABASE_URL` приложения имеет вид
`postgresql://avito_buddy_app:<url-encoded-password>@127.0.0.1:65432/avito_buddy?sslmode=disable`.
Для удалённой среды адрес, порт, TLS-режим, секрет и способ доставки секрета
должны быть выданы владельцем инфраструктуры; локальные демо-реквизиты туда
не переносятся.

## Проверка тестовых данных

После миграций импортируйте готовый экспорт с ветки RAG, уже включённый в этот
checkout, без повторного расчёта эмбеддингов:

```powershell
cd backend
$env:DATABASE_URL = 'postgresql://avito_buddy_app:<url-encoded-password>@127.0.0.1:65432/avito_buddy?sslmode=disable'
$env:RAG_COLLECTION = 'demo-qwen-v1'
go run ./cmd/rag import -file ../embeddings.json -source-key playbook/ds-skills.md
```

Экспорт помечен `synthetic`, доступен с роли `intern` без ограничения команды.
Это тестовый материал, а не утверждённый регламент Авито. Реальные инструкции
добавляйте только с подтверждёнными `source_url`, `origin`, `min_role` и `team`.
Для проверки наличия строк выполните запрос к БД:

```sql
SELECT d.title, d.origin, d.min_role, d.team, count(c.id) AS chunks
FROM rag.documents AS d
JOIN rag.chunks AS c ON c.doc_id = d.id
WHERE d.source_key = 'playbook/ds-skills.md'
GROUP BY d.id;
```

Для semantic search дополнительно нужен сервис эмбеддингов; его можно запустить
командой `docker compose -f backend/compose.rag.yml up -d embedder`.
