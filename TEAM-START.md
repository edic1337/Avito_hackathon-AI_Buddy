# AI Buddy: командный запуск

Это локальная интеграционная версия. Нужны Docker Desktop с Linux-контейнерами,
доступ к реестрам Docker/Hugging Face и OpenRouter, действующий API-ключ.
Node и Go на Windows устанавливать не требуется. Используются версии проекта:
Go 1.27 и Node 26. Модель вопросов: Qwen/Qwen3-Embedding-0.6B, размерность 1024.
Для генерации ответов используется отдельная модель OpenRouter.

## Обновление работающего Docker-запуска

Обнови файлы в корне репозитория и выполни:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1
docker compose --env-file .env.team -p ai-buddy-local -f compose.team.yml up -d --build api frontend
```

`setup.ps1` добавляет в существующий `.env.team` случайный токен для связи сайта с API,
сохраняя настройки OpenRouter. Compose использует PostgreSQL и кеш модели из проекта `ai-buddy-local`.
Открой [http://localhost:5173](http://localhost:5173), обнови страницу и задай вопрос.

## Первый запуск у коллеги

Склонировать репозиторий и переключиться на ветку с этими изменениями.
Положить полученные от команды файлы в:

- `embeddings/habr-corpus.json`
- `embeddings/onboarding-corpus.json`

Эти файлы не входят в Git. Ожидается 871 документ и 16413 фрагментов.

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1
notepad .env.team
powershell -ExecutionPolicy Bypass -File .\scripts\start.ps1 -ImportCorpus
```

В `.env.team` заполнить `OPENROUTER_API_KEY`, `OPENROUTER_API_URL` и `OPENROUTER_MODEL`.
При создании файла `setup.ps1` переносит эти настройки из `.env`, если они уже заданы.
Первый запуск скачивает образы и веса модели и может занять несколько минут.
Импорт выполняется только с параметром `-ImportCorpus`; обычный запуск его не повторяет.

Открой [http://localhost:5173](http://localhost:5173) и задай вопрос. Сайт отправляет вопросы
с правами стажёра. Прокси подставляет токен при обращении к Go, а API проверяет токен и права.
Порты привязаны к `127.0.0.1`. Для общей ссылки понадобится размещение на сервере с авторизацией.

## Последующие запуски и остановка

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start.ps1
# Остановка без удаления данных:
docker compose --env-file .env.team -p ai-buddy-local -f compose.team.yml stop
# Логи:
docker compose --env-file .env.team -p ai-buddy-local -f compose.team.yml logs --tail 50 api embedder
```

После изменения `.env.team`:
`docker compose --env-file .env.team -p ai-buddy-local -f compose.team.yml up -d --force-recreate api frontend`.
Не выполнять `down -v`: это удалит базу и кеш модели.

## Проверка

1. «Что нужно сделать стажёру в первую неделю?» — ответ и раскрываемые источники.
2. «Как наставник помогает стажёру?» — рекомендации без подмены роли стажёра.
3. «Какой пароль от корпоративного Wi-Fi?» — отсутствие выдуманного пароля.
4. Отметить шаг плана, обновить страницу — отметка сохраняется в этом браузере.
5. Проверить ссылку на статью. `attachment:///` отображается как адрес документа,
   скачивание вложений через API пока не реализовано.

API `/health` проверяет, что сервер запущен, но не подтверждает доступ к LLM.
При 403 от OpenRouter проверь `curl.exe -i https://openrouter.ai/api/v1/models`.
В этой команде доступ к сервису появился с VPN. Контейнеру API тоже нужен доступ к OpenRouter.
Ключ и модель могут иметь отдельные ограничения/квоты. Они не исправляются сборкой сайта.

## Проверки разработчика

```powershell
docker run --rm --mount "type=bind,source=$($PWD.Path)\backend,target=/src" -w /src golang:1.27 go test ./...
cd frontend
npm.cmd ci
npm.cmd run build
npm.cmd run lint
node scripts/check-markdown.mjs
```

Для разработки интерфейса запусти Docker Compose и из `frontend` выполни `npm.cmd run dev`.
Vite откроется на [http://127.0.0.1:5174](http://127.0.0.1:5174) и будет отправлять вопросы
через Docker-сайт на порту 5173.

## Границы реализации

- Модель обрабатывает каждый вопрос отдельно.
- Чат оформляет жирный текст, код и списки. Ссылки открываются из блока источников.
- Браузер хранит отметки учебного плана.
- API фильтрует источники по правам доступа.
- Эмбеддер обрабатывает до 512 токенов на запрос, чтобы снизить расход памяти.
- Инструкции модели уменьшают смешение языков/ролей, но не гарантируют качество каждого ответа.
- Для публичного размещения нужны отдельные настройки HTTPS, авторизации, БД и сети.
