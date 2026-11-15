# Запуск интегрированной версии

См. [TEAM-START.md](TEAM-START.md): Docker Compose, импорт корпусов, настройка доступа и проверки.

# Avito AI Buddy

Go backend for chat and RAG: [backend setup](backend/README.md).
PostgreSQL/pgvector setup, runtime permissions, and database import: [database guide](postgres/README.md).

The RAG data pipeline prepares Markdown documents, chunks text, calculates Qwen embeddings, and exports JSON for PostgreSQL. The current corpus contains 827 public Habr articles, 28 Playbook documents, and 16 onboarding checklist items (16,413 chunks total). The large prepared embedding exports are supplied separately and are not committed to Git.

See the [RAG project guide](docs/PROJECT_GUIDE.md), [export format](docs/EXPORT_FORMAT.md), [database notes](docs/DATABASE.md), [GPU setup](docs/RUN_GPU.md), and [onboarding guide](docs/RUN_ONBOARDING.md).
