# ModelAPI

Структура: `backend/ModelAPI/`  
- `LLM.go` — функция `Ask`  
- `RAGLLM.go` — функция `AskWithRAG`

## `Ask(message string) (string, error)`
Отправляет текст модели через OpenRouter и возвращает её ответ (нужны env: `OPENROUTER_API_URL`, `OPENROUTER_MODEL`, `OPENROUTER_API_KEY`).

## `AskWithRAG(question string, chunks []string) (string, error)`
Собирает промпт из RAG-чанков + вопроса и вызывает `Ask` для ответа строго по контексту.
