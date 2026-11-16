package ModelAPI

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	return AskContext(context.Background(), message)
}

func AskContext(ctx context.Context, message string) (string, error) {
	return askMessages(ctx, []apiMessage{{Role: "user", Content: message}})
}

func askMessages(ctx context.Context, messages []apiMessage) (string, error) {
	apiURL := os.Getenv("OPENROUTER_API_URL")
	model := os.Getenv("OPENROUTER_MODEL")
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY is not set")
	}
	if apiURL == "" || model == "" {
		return "", fmt.Errorf("OPENROUTER_API_URL and OPENROUTER_MODEL must be set")
	}

	var request chatRequest

	request.Model = model
	request.Reasoning.Enabled = true

	request.Messages = messages

	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

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
	errorBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return "", fmt.Errorf(
			"OpenRouter returned HTTP %d; reading error response: %w",
			resp.StatusCode, readErr,
		)
	}
	return "", fmt.Errorf(
		"OpenRouter returned HTTP %d: %s",
		resp.StatusCode, string(errorBody),
	)
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
