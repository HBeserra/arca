package services

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"changeme/internal/business/enginebus"

	"github.com/ardanlabs/kronk/sdk/kronk/model"
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

// HistoryMessage is a single chat turn stored per session.
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
	eng     enginebus.ExtEngine
	mu      sync.Mutex
	history map[string][]HistoryMessage
}

// NewQueryService creates a QueryService backed by the given engine.
func NewQueryService(eng enginebus.ExtEngine) *QueryService {
	return &QueryService{
		eng:     eng,
		history: make(map[string][]HistoryMessage),
	}
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

	// 3. Build prompt.
	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant. Answer questions using the provided context. If the context does not contain the answer, say so."
	}

	msgs := model.DocumentArray(
		model.TextMessage(model.RoleSystem, systemPrompt),
	)

	if len(fragments) > 0 {
		var contextBuilder string
		for i, f := range fragments {
			contextBuilder += fmt.Sprintf("[%d] %s\n\n", i+1, f.Text)
		}
		msgs = append(msgs,
			model.TextMessage(model.RoleSystem, "Context:\n"+contextBuilder),
		)
	}

	// Add history.
	q.mu.Lock()
	hist := q.history[sessionID]
	q.mu.Unlock()

	for _, hm := range hist {
		msgs = append(msgs, model.TextMessage(hm.Role, hm.Content))
	}

	msgs = append(msgs, model.TextMessage(model.RoleUser, userMessage))

	// 4. Stream response.
	ch, err := q.eng.ChatStream(ctx, msgs)
	if err != nil {
		return fmt.Errorf("query: chat stream: %w", err)
	}

	var fullResponse string

	for resp := range ch {
		if len(resp.Choice) == 0 {
			continue
		}

		switch resp.Choice[0].FinishReason() {
		case model.FinishReasonError:
			return fmt.Errorf("query: model error: %s", resp.Choice[0].Delta.Content)

		case model.FinishReasonStop:
			goto done

		default:
			token := resp.Choice[0].Delta.Content
			if token == "" {
				continue
			}

			fullResponse += token
			app.Event.Emit("chat:token", ChatTokenEvent{
				SessionID: sessionID,
				Token:     token,
			})
		}
	}

done:
	// Update history.
	q.mu.Lock()
	q.history[sessionID] = append(q.history[sessionID],
		HistoryMessage{Role: model.RoleUser, Content: userMessage},
		HistoryMessage{Role: model.RoleAssistant, Content: fullResponse},
	)
	q.mu.Unlock()

	app.Event.Emit("chat:done", ChatDoneEvent{SessionID: sessionID})

	return nil
}

// GetHistory returns the chat history for a session.
func (q *QueryService) GetHistory(sessionID string) []HistoryMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.history[sessionID]
}

// ClearHistory clears the chat history for a session.
func (q *QueryService) ClearHistory(sessionID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.history, sessionID)
}
