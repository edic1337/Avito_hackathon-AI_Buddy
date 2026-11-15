package FrontendAPI

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"GO/rag"
)

const maxRequestBytes = 1 << 20
const questionTimeout = 2 * time.Minute

// Права пользователя определяются авторизацией,
// а не параметрами запроса.
type QuestionRequest struct {
	Question string `json:"question"`
}

type QuestionResponse struct {
	Error    string       `json:"error"`
	Question string       `json:"question"`
	Answer   string       `json:"answer"`
	Sources  []rag.Source `json:"sources,omitempty"`
}

type Answerer interface {
	Answer(context.Context, string, rag.Access) (string, error)
}

// Дополнительный интерфейс для ответов с источниками.
// Старые реализации Answerer продолжают работать.
type SourceAnswerer interface {
	AnswerWithSources(
		context.Context,
		string,
		rag.Access,
	) (rag.AnswerResult, error)
}

type QuestionHandler struct {
	answerer     Answerer
	authenticate Authenticator
	timeout      time.Duration
	logger       *slog.Logger
}

func NewQuestionHandler(
	answerer Answerer,
	authenticate Authenticator,
) (*QuestionHandler, error) {
	if answerer == nil {
		return nil, errors.New("answerer is required")
	}

	if retriever, ok := answerer.(*rag.Retriever); ok && retriever == nil {
		return nil, errors.New("retriever is required")
	}

	if authenticate == nil {
		return nil, errors.New("authenticator is required")
	}

	return &QuestionHandler{
		answerer:     answerer,
		authenticate: authenticate,
		timeout:      questionTimeout,
		logger:       slog.Default(),
	}, nil
}

func (h *QuestionHandler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeResponse(
			w,
			http.StatusMethodNotAllowed,
			QuestionResponse{Error: "Method not allowed"},
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	r = r.WithContext(ctx)

	access, err := h.authenticate(r)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnauthenticated):
			w.Header().Set(
				"WWW-Authenticate",
				`Bearer realm="question"`,
			)
			writeResponse(
				w,
				http.StatusUnauthorized,
				QuestionResponse{Error: "Authentication required"},
			)

		case errors.Is(err, ErrForbidden):
			writeResponse(
				w,
				http.StatusForbidden,
				QuestionResponse{Error: "Access denied"},
			)

		default:
			h.writeFailure(w, r, "authenticate", "", err)
		}
		return
	}

	if !validAccess(access) {
		writeResponse(
			w,
			http.StatusForbidden,
			QuestionResponse{Error: "Access denied"},
		)
		return
	}

	contentType, _, err := mime.ParseMediaType(
		r.Header.Get("Content-Type"),
	)
	if err != nil || contentType != "application/json" {
		writeResponse(
			w,
			http.StatusUnsupportedMediaType,
			QuestionResponse{
				Error: "Content-Type must be application/json",
			},
		)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	defer r.Body.Close()

	var request QuestionRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		h.writeDecodeError(w, err)
		return
	}

	// После объекта JSON не должно быть других данных.
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		h.writeDecodeError(w, err)
		return
	}

	request.Question = strings.TrimSpace(request.Question)

	if request.Question == "" {
		writeResponse(
			w,
			http.StatusBadRequest,
			QuestionResponse{
				Error:    "Question is required",
				Question: request.Question,
			},
		)
		return
	}

	if err := ctx.Err(); err != nil {
		h.writeFailure(w, r, "answer", request.Question, err)
		return
	}

	var answer string
	var sources []rag.Source

	if sourceAnswerer, ok := h.answerer.(SourceAnswerer); ok {
		var result rag.AnswerResult

		result, err = sourceAnswerer.AnswerWithSources(
			ctx,
			request.Question,
			access,
		)

		answer = result.Answer
		sources = result.Sources
	} else {
		answer, err = h.answerer.Answer(
			ctx,
			request.Question,
			access,
		)
	}

	if err == nil {
		err = ctx.Err()
	}

	if err != nil {
		h.writeFailure(w, r, "answer", request.Question, err)
		return
	}

	writeResponse(
		w,
		http.StatusOK,
		QuestionResponse{
			Error:    "",
			Question: request.Question,
			Answer:   answer,
			Sources:  sources,
		},
	)
}

func (h *QuestionHandler) writeDecodeError(
	w http.ResponseWriter,
	err error,
) {
	var tooLarge *http.MaxBytesError
	var timeout net.Error

	switch {
	case errors.As(err, &tooLarge):
		writeResponse(
			w,
			http.StatusRequestEntityTooLarge,
			QuestionResponse{Error: "Request body exceeds 1 MiB"},
		)

	case errors.As(err, &timeout) && timeout.Timeout():
		writeResponse(
			w,
			http.StatusRequestTimeout,
			QuestionResponse{Error: "Request body read timed out"},
		)

	default:
		writeResponse(
			w,
			http.StatusBadRequest,
			QuestionResponse{Error: "Invalid JSON or unknown fields"},
		)
	}
}

func (h *QuestionHandler) writeFailure(
	w http.ResponseWriter,
	r *http.Request,
	stage string,
	question string,
	err error,
) {
	if errors.Is(r.Context().Err(), context.Canceled) {
		return
	}

	var timeout net.Error

	status := http.StatusInternalServerError
	message := "Failed to process question"

	if errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &timeout) && timeout.Timeout()) {
		status = http.StatusGatewayTimeout
		message = "Question processing timed out"
	}

	h.logger.ErrorContext(
		r.Context(),
		"question request failed",
		"stage", stage,
		"error", err,
	)

	writeResponse(
		w,
		status,
		QuestionResponse{
			Error:    message,
			Question: question,
		},
	)
}

func writeResponse(
	w http.ResponseWriter,
	status int,
	response QuestionResponse,
) {
	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(response)
}
