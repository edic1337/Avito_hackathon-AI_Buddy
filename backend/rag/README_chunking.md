# Чтение Markdown и структурный чанкинг

Содержимое архива копируется в корень проекта с сохранением папок.
Поддерживаются Python 3.10+ и входные файлы .md/.markdown в UTF-8.

## Что реализовано

Один класс MarkdownChunker и две функции чтения: load_document(path),
parse_document(text). В ingestion.py добавлена prepare_document(path, chunker, embedder).
Базы данных, SQL-записи и автоматического обновления индекса пока нет.

Путь данных:

1. load_document читает файл и проверяет метаданные, возвращает Document.
2. chunker.chunk(document) возвращает список TextChunk.
3. embed_document(document.title, chunks, embedder) добавляет векторы.
4. prepare_document объединяет эти шаги и возвращает пару (Document, list[EmbeddedChunk]).

Метаданные origin, min_role, team и source_url остаются в Document.
При будущей SQL-записи их нужно сохранить вместе с чанками. Чанкер не
предоставляет пользователю доступ к документам: SQL-фильтрация будет в поиске.

## Правила чанкинга

- Markdown разбирается markdown-it-py, а не регулярным выражением для заголовков.
- У каждого чанка остаётся путь заголовков, например «Первые дни > Доступ > VPN».
- Поддерживаются заголовки # и setext. Заголовки внутри code fence не становятся разделами.
- Соседние короткие абзацы одного раздела объединяются. Разные разделы не смешиваются.
- По возможности целиком сохраняются абзацы, списки, цитаты, код и таблицы.
- Большие списки сначала разделяются по пунктам верхнего уровня.
- Длинный обычный текст делится по предложениям/пробелам; очень длинные слова — по символам.
- Длинный код делится преимущественно по строкам; каждый фрагмент получает ограждения и язык.
- Длинные таблицы делятся по строкам с повторением шапки. Если даже одна строка с шапкой
  не помещается, возникает понятная ошибка вместо обрезки содержимого.
- Перекрытие добавляется только между текстовыми фрагментами одного раздела, целыми словами.
  Для кода, таблиц и списков искусственное перекрытие не добавляется. Если оно мешает
  вместить следующий целый блок, его пропускаем.
- Лимит проверяется по точной строке «Название | Раздел\nТекст» вместе со специальными
  токенами токенизатора. Это общий формат TextChunk.embedding_text(title), который
  используется и при кодировании, и при подсчёте бюджета.
- Обрезания токенизатором нет; используются исходные Unicode-символы без decode обрезанных токенов.

Это структурный чанкер, а не оценка смысловых границ нейросетью. Прирост качества
поиска нужно будет измерить на eval-наборе. Пустые документы и разделы, состоящие
только из заголовков, дают пустой список чанков. PDF/DOCX и сложные расширения
Markdown пока не поддерживаются. Очень длинные отдельные пункты списка и цитаты
делятся как текст: вложенная разметка в таких редких фрагментах может быть неполной.
Определения ссылок сохраняются в исходном тексте, но reference-ссылки не переписываются
в inline-ссылки в каждом чанке.

## Настройки

| Переменная | По умолчанию |
| --- | --- |
| CHUNK_TARGET_TOKENS | 300 |
| CHUNK_MAX_TOKENS | 400 |
| CHUNK_OVERLAP_TOKENS | 40 |
| TOKENIZER_MODEL | EMBEDDING_MODEL или Qwen/Qwen3-Embedding-0.6B |

Условие: 0 <= overlap < target <= max. Цель — ориентир для заполнения,
max — жёсткий предел. Короткий раздел может быть намного меньше цели.
Если само название и путь заголовков занимают весь бюджет, возвращается ошибка.
Токенизатор должен соответствовать модели эмбеддингов.
RagConfig.from_env читает окружение процесса; .env сам не загружается.

## Формат документа

```yaml
---
title: Инструкция по тестовому сервису
source_url: demo://test-service
origin: synthetic
min_role: intern
team: null
tags: [tools, access]
---
```

После YAML-блока идёт Markdown. Обязательны title, source_url, origin, min_role.
origin: public/synthetic, min_role: intern/buddy/manager. Пропущенная роль не
превращается автоматически в публичный доступ. team и tags необязательны.
Пример с кодом, списком и таблицей: data/kb/demo_onboarding.md.

## Запуск

Из корня проекта:

```bash
python -m pip install -r backend/requirements-rag.txt
python -m backend.rag.demo_chunking data/kb/demo_onboarding.md
```

Команда выводит чанки и число токенов. Она загружает токенизатор, но не веса модели.
Для обычного Markdown без YAML (например, my_document.md в корне проекта):

```bash
python -m backend.rag.demo_chunking "./my_document.md" --plain
python -m backend.rag.demo_chunking "./my_document.md" --plain --embed
```

--plain используется только для локального теста. Название берётся из имени файла,
source_url — из локального пути, origin=synthetic, min_role=intern, team=None.
Эти метаданные тестовые, а не утверждение о происхождении настоящего документа.
В БД ничего не записывается. Загрузка базы через prepare_document по-прежнему
требует явный YAML-блок. Для документа с YAML флаг --plain не нужен.
После --embed выводятся размерность, L2-норма и первые шесть координат каждого
вектора. Ожидается размерность 1024 и L2-норма 1.000000.

Чтобы также создать настоящие эмбеддинги:

```bash
python -m backend.rag.demo_chunking data/kb/demo_onboarding.md --embed
```

Эта команда уже загружает веса модели эмбеддингов.

## Вызов из Python

```python
from transformers import AutoTokenizer

from backend.rag.chunking import MarkdownChunker
from backend.rag.config import RagConfig
from backend.rag.embeddings import QwenEmbedder
from backend.rag.ingestion import prepare_document

config = RagConfig.from_env()
tokenizer = AutoTokenizer.from_pretrained(config.tokenizer_model)
chunker = MarkdownChunker(
    tokenizer,
    target_tokens=config.chunk_target_tokens,
    max_tokens=config.chunk_max_tokens,
    overlap_tokens=config.chunk_overlap_tokens,
)
embedder = QwenEmbedder(config)

# Внутри вашей async-функции:
document, embedded_chunks = await prepare_document(
    "data/kb/demo_onboarding.md", chunker, embedder
)
```

Токенизатор, чанкер и embedder создаются один раз и переиспользуются.
Пример выше показывает вызов внутри async-функции; для отдельного скрипта есть demo_chunking.py.

## Проверки

```bash
python -m unittest discover -s backend/tests -v
```

39 проверок без весов модели: 15 для эмбеддингов и конфигурации, 24 для чтения,
структурного чанкинга, лимита, перекрытия, Unicode и полного пути подготовки файла.
Unit-тесты используют детерминированный символьный токенизатор и тестовый embedder.
Проверки не измеряют семантическое качество поиска.

Отдельно проверен настоящий токенизатор Qwen (без загрузки весов модели):

```bash
python -m backend.tests.check_chunking_qwen
```

| Вход | Чанков | Максимум токенов |
| --- | --- | --- |
| Длинный русский абзац с эмодзи | 19 | 340 |
| Длинный код | 5 | 399 |
| Длинная таблица | 6 | 400 |
| Длинный список | 11 | 315 |
| Сплошной Unicode-текст без пробелов | 14 | 300 |

Также успешно запущен demo_chunking на приложенном документе: 5 чанков,
43–147 токенов. Загрузка полной модели и настоящие эмбеддинги всё ещё
не проверялись в среде подготовки. Токенизатор проверен с transformers 5.22.2,
markdown-it-py 4.3.0 и PyYAML 6.0.3; это не проверка всего стека эмбеддингов.

Документация библиотек:

- https://markdown-it-py.readthedocs.io/en/latest/using.html
- https://huggingface.co/docs/transformers/main_classes/tokenizer
