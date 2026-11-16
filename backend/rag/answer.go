package rag

import (
	"context"
	"fmt"
	"strings"

	"GO/ModelAPI"
)

type Source struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Origin string `json:"origin"`
}

type AnswerResult struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources"`
}

// Answer сохраняет совместимость с CLI и существующим интерфейсом.
func (r *Retriever) Answer(
	ctx context.Context,
	question string,
	access Access,
) (string, error) {
	result, err := r.AnswerWithSources(ctx, question, access)
	if err != nil {
		return "", err
	}

	return result.Answer, nil
}

// AnswerWithSources возвращает ответ и материалы, переданные модели.
func (r *Retriever) AnswerWithSources(
	ctx context.Context,
	question string,
	access Access,
) (AnswerResult, error) {
	documents, err := r.Retrieve(ctx, question, access)
	if err != nil {
		return AnswerResult{}, err
	}

	texts := make([]string, 0, len(documents))
	sources := make([]Source, 0, len(documents))

	for _, document := range documents {
		if strings.TrimSpace(document) == "" {
			continue
		}

		texts = append(texts, document)
		sources = append(
			sources,
			sourceFromDocument(document, len(texts)),
		)
	}

	answer, err := ModelAPI.AskWithRAGContext(ctx, question, texts)
	if err != nil {
		return AnswerResult{}, err
	}

	return AnswerResult{
		Answer:  answer,
		Sources: sources,
	}, nil
}

// Извлекает метаданные из заголовка, сформированного Store.Search.
// Номер совпадает с позицией фрагмента в контексте модели.
func sourceFromDocument(document string, number int) Source {
	source := Source{
		Number: number,
		Title:  fmt.Sprintf("Источник %d", number),
	}

	header, _, _ := strings.Cut(document, "\n\n")
	lines := strings.Split(header, "\n")

	if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
		source.Title = strings.TrimSpace(lines[0])
	}

	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, "Происхождение: "):
			source.Origin = strings.TrimSpace(
				strings.TrimPrefix(line, "Происхождение: "),
			)

		case strings.HasPrefix(line, "Источник: "):
			source.URL = strings.TrimSpace(
				strings.TrimPrefix(line, "Источник: "),
			)
		}
	}

	return source
}
