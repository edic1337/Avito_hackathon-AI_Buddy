# Запуск экспорта на RTX 3070 Ti (Windows / PowerShell)

## На текущем ПК: отправить проект в Git

```powershell
cd D:/Avito_hackathon-AI_Buddy
git status --short
git add .
git diff --cached --stat | Select-Object -Last 1
git commit -m "Add Habr corpus and batch embedding export"
git push origin RAG
```

Текущая ветка проекта — `RAG`. Корневой `.env` читается автоматически; CPU указан по умолчанию, `--device cuda` переопределяет его. В `.gitignore` исключены новые JSON из папки `embeddings`, папки `*.checkpoints`, окружения, кеши, локальные `.env` и веса моделей. 827 Markdown-документов из `data/kb/habr` нужно включить в коммит. Старый корневой `embeddings.json` удалён как устаревший тестовый артефакт.

## На ПК с видеокартой: клонировать проект

Открой PowerShell в папке, где хочешь хранить проект. Заверши обучение YOLO и другие задачи, занимающие видеопамять.

```powershell
git clone --branch RAG https://github.com/edic1337/Avito_hackathon-AI_Buddy.git
cd Avito_hackathon-AI_Buddy
py -3.13 --version
nvidia-smi
```

Если Python 3.13 отсутствует, установи его из Python.org или через `winget install --exact --id Python.Python.3.13`, затем открой новое окно PowerShell и вернись в папку проекта. Если Git отсутствует — сначала установи Git for Windows.

Если репозиторий уже клонирован, вместо повторного `git clone` перейди в его папку и выполни:

```powershell
git fetch origin
git switch RAG
git pull --ff-only origin RAG
```

## Создать отдельное окружение и установить зависимости

Здесь явно вызывается Python из `.venv`, поэтому активация окружения и изменение ExecutionPolicy не нужны. Окружение YOLO не используется.

```powershell
py -3.13 -m venv .venv
.\.venv\Scripts\python.exe -m pip install --upgrade pip
.\.venv\Scripts\python.exe -m pip install torch==2.11.0 --index-url https://download.pytorch.org/whl/cu128
.\.venv\Scripts\python.exe -m pip install -r backend/requirements.txt
.\.venv\Scripts\python.exe -m pip check
if (!(Test-Path .env)) { Copy-Item .env.example .env }
```

После каждого шага дождись завершения без ошибок. Для CUDA 12.8 нужен совместимый драйвер NVIDIA. Если проверка ниже не проходит, обнови драйвер и исправь окружение до запуска экспорта. CUDA Toolkit отдельно для этой установки PyTorch не требуется.

Проверка реального вычисления на GPU:

```powershell
.\.venv\Scripts\python.exe -c "import torch; print('PyTorch:',torch.__version__,'CUDA:',torch.version.cuda); assert torch.cuda.is_available(), 'CUDA unavailable'; print('GPU:',torch.cuda.get_device_name(0)); x=torch.ones((16,16),device='cuda'); print('GPU check:',(x@x).sum().item())"
```

Ожидается название RTX 3070 Ti и `GPU check: 4096.0`.

## Проверить исходники, настройки и тесты

```powershell
(Get-ChildItem data/kb/habr -File -Filter *.md).Count
$env:EMBEDDING_MODEL = 'Qwen/Qwen3-Embedding-0.6B'
$env:TOKENIZER_MODEL = 'Qwen/Qwen3-Embedding-0.6B'
$env:EMBEDDING_BATCH_SIZE = '8'
$env:CHUNK_TARGET_TOKENS = '300'
$env:CHUNK_MAX_TOKENS = '400'
$env:CHUNK_OVERLAP_TOKENS = '40'
Remove-Item Env:HF_HUB_OFFLINE -ErrorAction SilentlyContinue
Remove-Item Env:TRANSFORMERS_OFFLINE -ErrorAction SilentlyContinue
.\.venv\Scripts\python.exe -m unittest discover -s backend/tests -v
```

В исходниках ожидается 827 файлов, в тестах — 52 успешных проверки. На новом ПК нужен интернет для первоначального скачивания модели (веса около 1,2 ГБ) и зависимостей. Статьи уже находятся в Git и повторно с Хабра не скачиваются.

## Небольшой пробный запуск и полный экспорт

```powershell
.\.venv\Scripts\python.exe -u -m backend.rag.export_corpus --input data/kb/habr --output embeddings/gpu-smoke.json --public-habr --device cuda --limit 3 --fail-fast
.\.venv\Scripts\python.exe -u -m backend.rag.export_corpus --input data/kb/habr --output embeddings/habr-corpus.json --public-habr --device cuda --resume --fail-fast
```

Запускай вторую команду после успешного пробного запуска. Первая проверяет реальные векторы трёх публикаций, вторая обрабатывает весь корпус. Для исходного корпуса и указанных настроек ожидаются 827 документов и 15 633 чанка. Время 20–60 минут для RTX 3070 Ti — предварительная оценка, не измерение на этой видеокарте; скачивание модели не включено.

Если возникнет `CUDA out of memory`, закрой другие GPU-задачи и повтори полный запуск с меньшим батчем:

```powershell
$env:EMBEDDING_BATCH_SIZE = '4'
.\.venv\Scripts\python.exe -u -m backend.rag.export_corpus --input data/kb/habr --output embeddings/habr-corpus.json --public-habr --device cuda --resume --fail-fast
```

Уже завершённые документы сохраняются в `embeddings/habr-corpus.checkpoints`. После остановки повторяй ту же полную команду с `--resume`.

## Проверить итоговый JSON

```powershell
.\.venv\Scripts\python.exe -c "import json,math; from pathlib import Path; d=json.loads(Path('embeddings/habr-corpus.json').read_text(encoding='utf-8')); assert d['schema_version']==2 and d['summary']['complete'] and not d['errors']; assert d['collection']['embeddings_included']; docs=d['documents']; assert len(docs)==827; chunks=[c for doc in docs for c in doc['chunks']]; assert len(chunks)==15633; assert all(len(c['embedding'])==1024 and all(math.isfinite(v) for v in c['embedding']) and abs(math.hypot(*c['embedding'])-1)<1e-6 for c in chunks); print('OK:',len(docs),'documents;',len(chunks),'chunks; dimension=1024')"
```

После успешной проверки перенеси `embeddings/habr-corpus.json` на ПК с БД обычным файлом. Он игнорируется Git. Импортёр в PostgreSQL в текущем проекте ещё не реализован; JSON готовит данные, но не записывает их в БД сам.

Официальные источники установки: https://pytorch.org/get-started/locally/ и https://download.pytorch.org/whl/cu128/torch/.
