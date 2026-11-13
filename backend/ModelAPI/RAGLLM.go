package ModelAPI

import (
	"fmt"
	"strings"
)

// AskWithRAG отвечает на вопрос пользователя, используя найденные RAG-чанки.
func AskWithRAG(question string, chunks []string) (string, error) {

	var contextBuilder strings.Builder

	for i, chunk := range chunks {
		contextBuilder.WriteString(fmt.Sprintf(
			"[Chunk %d]\n%s\n\n",
			i+1,
			chunk,
		))
	}

	prompt := fmt.Sprintf(`
Ты — AI-ассистент. Ответь на вопрос пользователя, используя предоставленный контекст.

Правила:
1. Основывай ответ на информации из контекста.
2. Не придумывай факты, которых нет в контексте.
3. Если контекста недостаточно для ответа, сообщи об этом.
4. Отвечай понятно и по существу.

Контекст:
%s

Вопрос пользователя:
%s
`, contextBuilder.String(), question)

	return Ask(prompt)
}
