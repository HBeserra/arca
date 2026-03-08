package services

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"changeme/internal/business/enginebus"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// QueryConfig carries RAG and model parameters from the frontend.
type QueryConfig struct {
	TopK                int     `json:"topK"`
	SimilarityThreshold float32 `json:"similarityThreshold"`
	UseReranker         bool    `json:"useReranker"`
	SystemPrompt        string  `json:"systemPrompt"`
	MaxTokens           int     `json:"maxTokens"`
}

// HistoryMessage is a single chat turn for the frontend.
type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatTokenEvent carries a streaming token from the model.
type ChatTokenEvent struct {
	SessionID string `json:"sessionID"`
	Token     string `json:"token"`
}

// Citation identifies a source chunk used in a response.
type Citation struct {
	ID           string  `json:"id"`
	DocumentID   string  `json:"documentID"`
	DocumentName string  `json:"documentName"`
	ChunkIndex   int     `json:"chunkIndex"`
	Excerpt      string  `json:"excerpt"`
	Score        float32 `json:"score"`
}

// ChatCitationEvent carries source citations once retrieval is done.
type ChatCitationEvent struct {
	SessionID string     `json:"sessionID"`
	Citations []Citation `json:"citations"`
}

// ChatReasoningEvent carries a streaming reasoning/thinking token.
type ChatReasoningEvent struct {
	SessionID string `json:"sessionID"`
	Token     string `json:"token"`
}

// ChatDoneEvent signals that streaming has finished.
type ChatDoneEvent struct {
	SessionID string `json:"sessionID"`
}

// ChatErrorEvent is emitted when the query pipeline fails.
type ChatErrorEvent struct {
	SessionID string `json:"sessionID"`
	Error     string `json:"error"`
}

// QueryService is the Wails-facing service for RAG-augmented chat.
type QueryService struct {
	eng enginebus.ExtEngine
}

// NewQueryService creates a QueryService backed by the given engine.
func NewQueryService(eng enginebus.ExtEngine) *QueryService {
	return &QueryService{eng: eng}
}

// Query performs a RAG-augmented chat turn and streams responses via events.
func (q *QueryService) Query(sessionID, userMessage string, cfg QueryConfig) error {
	go func() {
		if err := q.query(sessionID, userMessage, cfg); err != nil {
			application.Get().Event.Emit("chat:error", ChatErrorEvent{
				SessionID: sessionID,
				Error:     err.Error(),
			})
		}
	}()

	return nil
}

func (q *QueryService) query(sessionID, userMessage string, cfg QueryConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	app := application.Get()

	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("query: invalid session id: %w", err)
	}

	// 1. Retrieve relevant fragments via semantic search.
	fragments, err := q.eng.SearchDocs(ctx, sid, userMessage)
	if err != nil {
		return fmt.Errorf("query: search: %w", err)
	}

	// 2. Optionally rerank.
	var citations []Citation

	if len(fragments) > 0 {
		texts := make([]string, len(fragments))
		for i, f := range fragments {
			texts[i] = f.Text
		}

		if cfg.UseReranker && len(texts) > 1 {
			ranked, err := q.eng.Rerank(ctx, userMessage, texts)
			if err == nil {
				reordered := make([]enginebus.Fragment, 0, len(ranked))
				for _, r := range ranked {
					if r.Index < len(fragments) {
						reordered = append(reordered, fragments[r.Index])
					}
				}
				fragments = reordered
			}
		}

		for i, f := range fragments {
			excerpt := f.Text
			if len(excerpt) > 300 {
				excerpt = excerpt[:300] + "…"
			}

			citations = append(citations, Citation{
				ID:           fmt.Sprintf("%s-%d", f.DocumentID.String(), i),
				DocumentID:   f.DocumentID.String(),
				DocumentName: filepath.Base(f.Path),
				ChunkIndex:   i,
				Excerpt:      excerpt,
				Score:        float32(f.Similarity),
			})
		}
	}

	// Emit citations before streaming starts.
	app.Event.Emit("chat:citation", ChatCitationEvent{
		SessionID: sessionID,
		Citations: citations,
	})

	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant. Answer questions using the provided context. If the context does not contain the answer, say so."
	}

	// 3. Stream response — history is managed by the engine in the session store.
	events, err := q.eng.ChatStream(ctx, enginebus.Question{
		SessionID: sid,
		Content:   userMessage,
		Fragments: fragments,
		System:    systemPrompt,
	})
	if err != nil {
		return fmt.Errorf("query: chat stream: %w", err)
	}

	for event := range events {
		if event.Err != nil {
			return fmt.Errorf("query: model error: %w", event.Err)
		}

		if event.Answer != nil {
			break
		}

		if event.Reasoning != "" {
			app.Event.Emit("chat:reasoning", ChatReasoningEvent{
				SessionID: sessionID,
				Token:     event.Reasoning,
			})
		}

		if event.Token != "" {
			app.Event.Emit("chat:token", ChatTokenEvent{
				SessionID: sessionID,
				Token:     event.Token,
			})
		}
	}

	app.Event.Emit("chat:done", ChatDoneEvent{SessionID: sessionID})

	return nil
}

// GetHistory returns the chat history for a session from the engine.
func (q *QueryService) GetHistory(sessionID string) []HistoryMessage {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := q.eng.GetSession(ctx, sid)
	if err != nil {
		return nil
	}

	history := make([]HistoryMessage, len(session.ChatHistory))
	for i, hm := range session.ChatHistory {
		history[i] = HistoryMessage{Role: hm.Role, Content: hm.Content}
	}

	return history
}

// ClearHistory removes all chat history for a session.
func (q *QueryService) ClearHistory(sessionID string) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := q.eng.ClearChatHistory(ctx, sid); err != nil {
		// non-fatal: log via slog if needed
		_ = err
	}
}
