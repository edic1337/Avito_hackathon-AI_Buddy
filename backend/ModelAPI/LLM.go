package ModelAPI

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Внутренние структуры для JSON API.
type apiMessage struct {
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}

type chatRequest struct {
	Model     string       `json:"model"`
	Messages  []apiMessage `json:"messages"`
	Reasoning struct {
		Enabled bool `json:"enabled"`
	} `json:"reasoning"`
}

type chatResponse struct {
	Choices []struct {
		Message apiMessage `json:"message"`
	} `json:"choices"`
}

// Ask отправляет запрос модели и возвращает её ответ.
func Ask(message string) (string, error) {
	apiURL := os.Getenv("OPENROUTER_API_URL")
	model := os.Getenv("OPENROUTER_MODEL")
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY is not set")
	}

	var request chatRequest

	request.Model = model
	request.Reasoning.Enabled = true

	request.Messages = []apiMessage{
		{
			Role:    "user",
			Content: message,
		},
	}

	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	ctx := context.Background()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		apiURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("OpenRouter returned HTTP %d", resp.StatusCode)
	}

	var result chatResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}

	return result.Choices[0].Message.Content, nil
}
