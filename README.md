# Avito AI Buddy — ветка RAG

Подготовка базы знаний: Markdown → структурные чанки → локальные эмбеддинги Qwen → JSON для последующего импорта в PostgreSQL/pgvector.

Корпус: **827 публикаций Хабра + 28 файлов Playbook + 16 пунктов чек-листа**. При текущих настройках — 16 413 чанков. Готовые эмбеддинги передаются отдельными JSON и не хранятся в Git.

## Начать работу

```powershell
py -3.13 -m venv .venv
.\.venv\Scripts\python.exe -m pip install --upgrade pip
# CPU; для GPU см. инструкцию ниже
.\.venv\Scripts\python.exe -m pip install torch==2.11.0 --index-url https://download.pytorch.org/whl/cpu
.\.venv\Scripts\python.exe -m pip install -r backend/requirements.txt
if (!(Test-Path .env)) { Copy-Item .env.example .env }
.\.venv\Scripts\python.exe -m unittest discover -s backend/tests -v
```

`.env` читается автоматически. По умолчанию используется CPU. Для NVIDIA установите CUDA-сборку PyTorch и укажите `EMBEDDING_DEVICE=cuda` либо передайте `--device cuda`.

```powershell
.\.venv\Scripts\python.exe -u -m backend.rag.export_corpus --input data/kb/onboarding --output embeddings/onboarding-corpus.json --resume --fail-fast
```

**[Полный разбор иерархии, модулей, настроек и команд](docs/PROJECT_GUIDE.md)**

- [Формат JSON](docs/EXPORT_FORMAT.md)
- [PostgreSQL и будущий поиск](docs/DATABASE.md)
- [Запуск на RTX 3070 Ti](docs/RUN_GPU.md)
- [Playbook и чек-лист отдельно](docs/RUN_ONBOARDING.md)

Реализованы подготовка документов, чанкинг, эмбеддинги и экспорт. Импортёр БД, SQL-репозиторий, LLM-клиент и HTTP API ещё не реализованы. Миграции не применяются автоматически.
