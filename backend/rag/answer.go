package rag

import (
	"GO/ModelAPI"
	"context"
)

// Answer retrieves context and invokes the existing LLM client. Empty retrieval
// returns ModelAPI.NoContextAnswer without an LLM request.
func (r *Retriever) Answer(ctx context.Context, question string, access Access) (string, error) {
	documents, err := r.Retrieve(ctx, question, access)
	if err != nil {
		return "", err
	}
	return ModelAPI.AskWithRAGContext(ctx, question, documents)
}
