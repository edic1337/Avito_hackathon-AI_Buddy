# Экспорт корпуса в JSON для PostgreSQL / pgvector

В проект добавлены `backend/rag/export_corpus.py`, `backend/tests/test_export_corpus.py` и `backend/requirements.txt`. Базовые модели, схема БД и существующий чанкер сохранены. Корпус из 827 публикаций скопирован в `data/kb/habr`.

## Полный расчёт чанков и векторов

Из PowerShell, в корне проекта:

```powershell
Set-Location D:/Avito_hackathon-AI_Buddy
python -m pip install -r backend/requirements.txt
python -m backend.rag.export_corpus --input data/kb/habr --output embeddings/habr-corpus.json --public-habr --resume
```

Используется Qwen/Qwen3-Embedding-0.6B, 1024 координаты, нормализованные векторы, документный prompt пустой. Вопросы при поиске нужно кодировать тем же `QwenEmbedder.embed_query`, где применяется prompt `query`.

Веса и токенизатор уже присутствовали на проверенной машине. Для работы без обращений к Hugging Face:

```powershell
$env:HF_HUB_OFFLINE = '1'
python -m backend.rag.export_corpus --input data/kb/habr --output embeddings/habr-corpus.json --public-habr --resume --offline
```

CUDA в проверенном окружении недоступна (`torch` установлен с CPU). Если в другом окружении есть CUDA-версия PyTorch и совместимая видеокарта, добавьте `--device cuda`. Экспорт по умолчанию использует 4 потока CPU. Размер модельного батча задаёт `EMBEDDING_BATCH_SIZE` (по умолчанию 8).

## Проверка чанкинга без расчёта векторов

```powershell
python -m backend.rag.export_corpus --input data/kb/habr --output embeddings/habr-chunks.json --public-habr --chunks-only --resume
```

Такой файл содержит тексты, метаданные и токенные размеры, но **не готов к вставке в поле `embedding`**. В нём `collection.embeddings_included=false`. Не подменяйте настоящие эмбеддинги нулями или случайными значениями.

## Формат JSON, версия 2

```text
schema_version: 2
collection:
  name, embedding_model, embedding_revision, embedding_dimension
  embedding_text_version, query_prompt_name, chunking_config
  embeddings_included
documents[]:
  source_key, source_path, title, content, source_url
  origin, min_role, team, tags, content_hash, metadata
  chunks[]:
    position, section, text, embedding_text, token_count
    content_hash, embedding[1024]
summary:
  expected_documents, documents, chunks, complete
errors[]
```

`position` начинается с 1. `source_key` — относительный путь к файлу внутри входной папки, не абсолютный путь компьютера. Источник для цитирования — `source_url`, дополнительный ID исходной статьи хранится в `metadata.document_id`.

JSON старого `demo_chunking` использует `schema_version=1` и один `document`; пакетный экспорт имеет другой, явно обозначенный формат. Старый `embeddings.json` не перезаписывается.

## Соответствие таблицам БД

1. Из `collection` вставьте поля таблицы `rag.collections`; служебный флаг `embeddings_included` не является столбцом БД.
2. Для каждого элемента `documents` вставьте поля `rag.documents` с ID коллекции. `metadata` — дополнительные исходные данные; отдельного столбца для них в текущей схеме нет.
3. Из `chunks` вставьте поля `rag.chunks` с полученным `doc_id`. Поле `embedding` передавайте как `vector(1024)` через адаптер pgvector или параметризованный запрос.
4. Перед импортом требуйте `summary.complete=true`, пустой `errors` и `collection.embeddings_included=true`. Для каждого чанка проверьте 1024 конечных числа и L2-норму около 1.
5. Выполняйте импорт документа и его чанков одной транзакцией. `UNIQUE(collection_id, source_key)` и `UNIQUE(doc_id, position)` позволяют реализовать повторный импорт без дублирования. При замене документа удаляйте старые лишние позиции, если новый документ стал короче.

Экспортёр сам в БД не пишет и миграцию не запускает. Таблицы и SQL-ограничения не менялись.

## Метаданные доступа и логическая структура

`--public-habr` — явный адаптер к предоставленному публичному корпусу: выставляет `origin=public`, `min_role=intern`, `team=null`. Он принимает только полные публикации на `https://habr.com`, отклоняет конспекты и конфликтующие исходные ограничения доступа. В каталоге Avito есть совместные публикации из блогов других компаний — их исходные URL сохраняются. Для остальных документов действует существующая строгая проверка YAML, без автоматического назначения доступа.

Служебные `index.md`, `README.md`, `CHUNKING.md` не индексируются. Название и путь заголовков входят в `embedding_text`. Для HTML-таблиц разворачиваются rowspan/colspan и сохраняется соответствие колонок; слишком длинные ячейки делятся с повторением контекста строки и колонки. Раскрывающиеся разделы сохраняют текст. Код внутри fenced-блоков не проходит HTML-преобразование. Исходный Markdown остаётся целиком в `document.content`.

## Остановка и продолжение

После каждого полностью обработанного документа сохраняется JSON в папке `<имя-результата>.checkpoints` рядом с выходным файлом. Параметр `--resume` использует только записи с совпадающим отпечатком исходника, модели и конфигурации. Изменение входных текстов или режима `--chunks-only` потребует пересчёта соответствующих записей. Незавершённый текущий документ считается заново.

Итоговый JSON собирается потоково из промежуточных файлов и заменяется атомарно. На ошибках сохраняется отчёт `errors`, `summary.complete=false`, команда завершится с ненулевым кодом. Принудительное завершение процесса до сборки может оставить предыдущую версию итогового JSON; смотрите лог и запускайте с `--resume`.

## Проверки

```powershell
python -m unittest discover -s backend/tests -v
```

На машине пользователя пройдены 52 теста, включая 5 новых проверок адаптера метаданных, ограничений доступа, таблиц, токенного бюджета и полного сохранения координат в JSON. Модульные проверки используют тестовые векторы; отдельный файл примера с эмбеддингами проверяется на настоящей локальной модели.

## Настройки и структура проекта

Корневой `.env` теперь читается автоматически. Шаблон — `.env.example`; переменные процесса имеют приоритет. Единая установка зависимостей: `python -m pip install -r backend/requirements.txt`. Полный разбор находится в [PROJECT_GUIDE.md](PROJECT_GUIDE.md).
