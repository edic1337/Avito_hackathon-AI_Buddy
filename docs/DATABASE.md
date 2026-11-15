# Статус документа

В репозитории есть Go PostgreSQL/pgvector Store и CLI. `import-corpus` загружает
экспорты schema_version=2 потоково; старый `import` предназначен для одиночного
schema_version=1 документа. Форматы описаны в [EXPORT_FORMAT.md](EXPORT_FORMAT.md),
подготовка корпуса — в [PROJECT_GUIDE.md](PROJECT_GUIDE.md). Поддерживаются origin
`public`, `provided` и `synthetic`.

# Схема БД и подключения RAG

Используется отдельная схема `rag` с тремя таблицами. DDL находится в
`postgres/001_rag_schema.sql`; эволюционные изменения — в следующих миграциях.
Go CLI применяет их командой `go run ./cmd/rag migrate`.

## Выбор архитектуры

PostgreSQL + pgvector в общей БД проекта, отдельная схема rag.
Три таблицы: collections, documents, chunks. Авторизация, пользователи,
диалоги и заявки остаются в таблицах коллег; копировать их в RAG не нужно.

| Таблица | Единица хранения | Назначение |
| --- | --- | --- |
| rag.collections | Набор документов с одним профилем индексации | Фиксирует модель и настройки чанкинга |
| rag.documents | Исходный документ внутри коллекции | Хранит содержимое, ссылку и права |
| rag.chunks | Один фрагмент документа | Хранит текст, вектор и полнотекстовый индекс |

Одна collection содержит много documents; один document содержит много chunks.
Коллекция — не отдельная база PostgreSQL. На старте достаточно одной main
или demo. Поиск всегда привязан к одной коллекции.

## Настройки коллекции

- name: например main или demo-qwen-v1.
- embedding_model: Qwen/Qwen3-Embedding-0.6B.
- embedding_revision: точная ревизия весов, если известна; у существующего JSON её нет — NULL.
- embedding_dimension: 1024. Эта DDL специально рассчитана на vector(1024).
- embedding_text_version: 1, текущий формат «Название | Раздел\nТекст».
- query_prompt_name: query.
- chunking_config: текущий объект chunking из JSON: tokenizer_model,
  target_tokens, max_tokens, overlap_tokens.

Все документы коллекции создаются с одинаковыми настройками. Репозиторий
должен сравнить их при запуске/импорте, а не только размерность векторов.
Модель или правила подготовки текста нельзя молча менять в заполненной коллекции.
Создаётся новая коллекция и пересчитываются векторы. Для модели иной размерности
также потребуется изменение таблицы/отдельная таблица, одной новой строки collection недостаточно.

У старого JSON неизвестна точная ревизия весов. Название модели и размерность
не доказывают точное совпадение весов. Старый файл можно использовать в тестовой
коллекции с revision=NULL; в следующих экспортах стоит фиксировать ревизию
и передавать ту же revision в загрузку модели и токенизатора.
Текущий QwenEmbedder ещё не принимает revision — это отдельное изменение кода.

## Связь с готовыми Python-моделями

| Текущие данные | Столбец БД |
| --- | --- |
| Document.title | documents.title |
| Document.text | documents.content |
| Document.source_url/source_path | documents.source_url/source_path |
| Document.origin/min_role/team/tags | Одноимённые поля documents |
| Указанный при импорте логический ключ | documents.source_key |
| EmbeddedChunk.section/text/embedding | chunks.section/text/embedding |
| Номер чанка при enumerate(..., 1) | chunks.position |
| TextChunk(section, text).embedding_text(title) | chunks.embedding_text |
| token_count в JSON / подсчёт тем же токенизатором | chunks.token_count |
| SHA-256 точного embedding_text | chunks.content_hash |

source_key задаёт идентичность документа, например playbook/ds-skills.md.
Не используем хеш текста как идентичность: после редактирования это тот же документ.
Абсолютный путь на компьютере сохраняется только для диагностики в source_path.
Одинаковый документ на двух компьютерах должен получать одинаковый source_key.

SHA-256 документа считаем от канонического JSON [title, text] в UTF-8:
json.dumps([title, text], ensure_ascii=False, separators=(',', ':')).
Изменение названия тоже требует пересчёта векторов: название входит в embedding_text.
Исправление source_url, tags или прав само по себе пересчёта не требует.

## Загрузка нового Markdown

Пример Python-подготовки документа из ветки RAG (не текущий Go API):

```python
document, chunks = await prepare_document(path, chunker, embedder)
doc_id = await repository.save_document(
    document,
    chunks,
    source_key="playbook/ds-skills.md",
)
```

repository создаётся с выбранной коллекцией. save_document:

1. До транзакции проверяет профиль, роли, размерность, конечность и норму векторов,
   количество чанков, их порядок, token_count и соответствие исходному документу.
   Подготовка эмбеддингов не выполняется внутри открытой транзакции.
2. Начинает транзакцию. Создаёт документ через INSERT ... ON CONFLICT DO NOTHING,
   затем читает его SELECT ... FOR UPDATE по (collection_id, source_key).
   Это сериализует одновременные записи одного источника.
3. Сравнивает content_hash и активность. Метаданные и права обновляет в любом случае.
   При неизменном содержимом и том же профиле уже сохранённые чанки можно оставить.
4. При изменении содержимого обновляет документ, удаляет его старые чанки
   и вставляет весь новый набор. Явно выставляет updated_at=now().
5. COMMIT. При любой ошибке — ROLLBACK: старая версия остаётся целиком.

Поиск в параллельном запросе видит либо старую, либо новую завершённую версию.
ID документа при обновлении сохраняется. ID чанков при замене могут измениться.
Пустой набор чанков по умолчанию считаем ошибкой загрузки; отключение документа
выполняется отдельно через is_active=false.

При неизменном содержимом этот алгоритм экономит SQL-перезапись, но сам по себе
не экономит вычисление эмбеддингов: prepare_document уже их посчитала.
Позже orchestration загрузки сможет проверить хеш до вызова embedder.

## Исторический импорт schema_version=1

`go run ./cmd/rag import` загружает одиночный экспорт до 64 MiB. Для полного
корпуса версии 2 используйте `go run ./cmd/rag import-corpus`, описанный в
`postgres/README.md`. Повторный расчёт 50 демонстрационных векторов не нужен.
Исторический формат версии 1 содержит:

1. Прочитать schema_version=1, embedding_model, embedding_dimension, chunking.
2. Проверить совместимость с выбранной коллекцией и наличие embedding у всех чанков.
3. Восстановить Document из document; из chunks — EmbeddedChunk и position/token_count.
4. Проверить токены/векторы и выполнить ту же транзакцию сохранения.

Для этого JSON можно создать коллекцию demo-qwen-v1 с текущим профилем.
plain_test=true и тестовые origin/min_role сохраняются явно, не превращаются
автоматически в настоящие регламенты или подтверждённые ACL.
Для рабочей базы нужно сначала указать настоящие source_url, origin, min_role и team.
Изменение только этих метаданных не требует пересчёта векторов.

В БД vector хранит координаты float32, поэтому после загрузки возможны небольшие
изменения последних знаков по сравнению с Python float в JSON. Это ожидается;
не нужно проверять побитовое равенство JSON и значения из PostgreSQL.

## Векторный поиск

В Python: нормализуем/проверяем роль, неизвестную роль отклоняем ValueError.
role_rank: intern=1, buddy=2, manager=3. Роль и команда поступают из доверенного
контекста бэкенда, не напрямую из произвольного поля фронтенда.
team=NULL у документа означает доступность любой команде при достаточной роли.
team=None у пользователя разрешает только документы без командного ограничения.
manager не получает автоматический доступ к другим командам.

SQL с именованными параметрами Psycopg; это шаблон запроса, не команда для DBeaver:

```sql
SELECT
    c.id, c.doc_id, d.title, c.section, c.text,
    d.source_url, d.origin,
    1 - (c.embedding <=> %(embedding)s::public.vector(1024)) AS cosine_score
FROM rag.chunks AS c
JOIN rag.documents AS d ON d.id = c.doc_id
WHERE d.collection_id = %(collection_id)s
  AND d.is_active
  AND (CASE d.min_role
         WHEN 'intern' THEN 1
         WHEN 'buddy' THEN 2
         WHEN 'manager' THEN 3
       END) <= %(role_rank)s
  AND (d.team IS NULL OR d.team = %(team)s)
ORDER BY c.embedding <=> %(embedding)s::public.vector(1024), c.id
LIMIT %(limit)s;
```

Возвращаем готовый Chunk из models.py. score=cosine_score для векторного поиска.
collection_id хранится в объекте репозитория, а не выбирается произвольно фронтендом.
Фильтр доступа применяется в SQL до итогового LIMIT; закрытые тексты не передаются LLM.
Это фильтрация уровня приложения, не полноценная PostgreSQL Row-Level Security.
Если коллеги добавят отдельный API чтения документов/источников, там тоже нужна
проверка прав; одного фильтра retrieval для всего приложения недостаточно.

## Полнотекстовый поиск

fts уже готовится PostgreSQL из embedding_text: учитываются название документа,
путь заголовков и сам фрагмент. GIN-индекс включён в DDL.

```sql
WITH query AS (
    SELECT websearch_to_tsquery('russian'::regconfig, %(question)s) AS value
)
SELECT
    c.id, c.doc_id, d.title, c.section, c.text,
    d.source_url, d.origin,
    ts_rank_cd(c.fts, query.value) AS score
FROM rag.chunks AS c
JOIN rag.documents AS d ON d.id = c.doc_id
CROSS JOIN query
WHERE d.collection_id = %(collection_id)s
  AND d.is_active
  AND (CASE d.min_role
         WHEN 'intern' THEN 1
         WHEN 'buddy' THEN 2
         WHEN 'manager' THEN 3
       END) <= %(role_rank)s
  AND (d.team IS NULL OR d.team = %(team)s)
  AND c.fts @@ query.value
ORDER BY score DESC, c.id
LIMIT %(limit)s;
```

FTS-ранг не равен косинусному сходству. При гибридном поиске списки объединяет
Retriever через RRF; порог на cosine_score, а не на FTS/RRF.

## Индексы и масштаб

Для текущего корпуса начинаем с точного векторного поиска, без HNSW.
pgvector по умолчанию выполняет exact search. После замеров можно добавить
HNSW; он приблизительный и требует проверки recall, особенно с фильтрами доступа.
GIN нужен для полнотекстового поиска, B-tree — для связей и отбора документов.
Скор, вопрос и его вектор в chunks не сохраняются: это данные отдельного запроса.

## Подключение из Python

Предлагаемый стек: Psycopg 3 с async-подключениями + pgvector-python.
При старте один пул соединений. На каждом созданном соединении регистрируются
векторные типы через await register_vector_async(conn). Вектор передаётся
параметром как pgvector.Vector(...); SQL не собирается f-строками.
Для Windows практичен бинарный пакет psycopg[binary,pool].

Класс PostgresKnowledgeRepository реализует существующие search_vector/search_text
и добавляет save_document/import_json. Новый базовый класс или ORM не требуется.
Параметры подключения берутся из готового config.database_url.
Таблицы создаются миграцией, а не при каждом запросе.

Распределение ответственности:

| Компонент | Ответственность |
| --- | --- |
| QwenEmbedder | Только вычисление векторов |
| MarkdownChunker | Только разбиение документа |
| prepare_document | Чтение → чанкинг → эмбеддинги |
| PostgresKnowledgeRepository, предстоит написать | SQL, транзакции, фильтры доступа |
| Retriever, предстоит написать | Эмбеддинг вопроса, запуск поиска, объединение результатов |

Доступы к БД: миграционный пользователь создаёт extension/schema/tables.
Приложение получает необходимые права на rag и sequences, но не должно работать
под PostgreSQL-superuser. Это выдача прав бэкенд-сервису; права конечного
пользователя остаются отдельной проверкой role/team в запросах.

## Проверка и следующий шаг

Миграции запускаются явно, а не при каждом запросе. Текущая локальная БД
PostgreSQL с pgvector и импорт полного корпуса проверяются отдельно; см.
`postgres/README.md` и `backend/VALIDATION.md`.

При изменении данных повторный импорт не удаляет источники, отсутствующие во входных
JSON. Интеграционные проверки включают повторную загрузку без дублей, обновление
документа, смену ACL без пересчёта и отсутствие закрытых чанков в выдаче.

Источники:

- https://github.com/pgvector/pgvector
- https://github.com/pgvector/pgvector-python
