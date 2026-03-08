package enginebus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ardanlabs/kronk/sdk/kronk"
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/google/uuid"
)

type (
	// ChatMessage is an internal chat turn in the session history.
	ChatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	// FileContext is raw file content passed into a chat request as additional context.
	FileContext struct {
		Name    string
		Content string
	}

	Question struct {
		SessionID uuid.UUID
		Content   string
		Fragments []Fragment
		Files     []FileContext
		System    string
	}

	Answer struct {
		Content          string
		PromptTokens     int
		ReasoningTokens  int
		CompletionTokens int
		OutputTokens     int
		TokensPerSecond  float64
		ContextWindow    int
		ContextTokens    int
	}

	// ChatEvent is one streamed item from ChatStream. Exactly one of Token,
	// Reasoning, Answer, or Err is set per event; Answer and Err mark the end
	// of the stream.
	ChatEvent struct {
		Token     string  // non-empty: streaming content token
		Reasoning string  // non-empty: streaming reasoning/thinking token
		Answer    *Answer // non-nil: final answer (stream complete)
		Err       error   // non-nil: error (stream terminated)
	}
)

// Chat performs a non-streaming chat request and returns the complete answer.
func (e *Engine) Chat(ctx context.Context, q Question) (Answer, error) {
	session, ch, cancel, err := e.startChat(ctx, q)
	if err != nil {
		return Answer{}, err
	}
	defer cancel()

	answer, err := collectResponse(e.krnChat, ch, nil, nil)
	if err != nil {
		return Answer{}, fmt.Errorf("model response: %w", err)
	}

	e.persistHistory(session, q.Content, answer.Content)

	return *answer, nil
}

// ChatStream sends a Question and returns a channel of streaming ChatEvents.
func (e *Engine) ChatStream(ctx context.Context, q Question) (<-chan ChatEvent, error) {
	session, ch, cancel, err := e.startChat(ctx, q)
	if err != nil {
		return nil, err
	}

	out := make(chan ChatEvent)

	go func() {
		defer close(out)
		defer cancel()

		onToken := func(token string) { out <- ChatEvent{Token: token} }
		onReasoning := func(r string) { out <- ChatEvent{Reasoning: r} }

		answer, err := collectResponse(e.krnChat, ch, onToken, onReasoning)
		if err != nil {
			out <- ChatEvent{Err: err}
			return
		}

		e.persistHistory(session, q.Content, answer.Content)
		out <- ChatEvent{Answer: answer}
	}()

	return out, nil
}

// persistHistory appends the user/assistant turn to the session and saves it.
func (e *Engine) persistHistory(session Session, userContent, assistantContent string) {
	session.ChatHistory = append(session.ChatHistory,
		ChatMessage{Role: model.RoleUser, Content: userContent},
		ChatMessage{Role: model.RoleAssistant, Content: assistantContent},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.store.UpdateSession(ctx, session); err != nil {
		e.logger.Error("failed to persist chat history", "sessionID", session.ID, "error", err)
	}
}

// ClearChatHistory removes all chat history from the session.
func (e *Engine) ClearChatHistory(ctx context.Context, sessionID uuid.UUID) error {
	session, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	session.ChatHistory = []ChatMessage{}
	if err := e.store.UpdateSession(ctx, session); err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

func (e *Engine) startChat(ctx context.Context, q Question) (Session, <-chan model.ChatResponse, context.CancelFunc, error) {
	if e.krnChat == nil {
		if err := e.Load(ctx); err != nil {
			return Session{}, nil, nil, fmt.Errorf("chat model not loaded and failed to load: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)

	session, err := e.store.GetSession(ctx, q.SessionID)
	if err != nil {
		cancel()
		return Session{}, nil, nil, fmt.Errorf("getting session: %w", err)
	}

	msgs := buildMessages(session, q)

	d := model.D{
		"messages":    msgs,
		"max_tokens":  2048,
		"temperature": 0.7,
		"top_p":       0.9,
		"top_k":       40,
	}

	ch, err := e.krnChat.ChatStreaming(ctx, d)
	if err != nil {
		cancel()
		return Session{}, nil, nil, fmt.Errorf("chat streaming: %w", err)
	}

	return session, ch, cancel, nil
}

// buildMessages converts internal types into the kronk message array for the API.
func buildMessages(s Session, q Question) []model.D {
	var msgs []model.D

	// System prompt.
	systemPrompt := q.System
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant. Answer questions using the provided context. If the context does not contain the answer, say so."
	}
	msgs = append(msgs, model.TextMessage(model.RoleSystem, systemPrompt))

	// RAG fragment context (up to 2 fragments).
	if len(q.Fragments) > 0 {
		const contextTemplate = `Use the following Context to answer the user's question.
If you don't know the answer, say that you don't know.
Responses should be properly formatted to be easily read.
Share code if code is presented in the context.
Do not include any additional information not present in the context.

Context:

%s`
		var content strings.Builder
		for i, doc := range q.Fragments {
			if i >= 2 {
				break
			}
			fmt.Fprintf(&content, "%s\n%s\n", doc.Path, doc.Text)
		}
		msgs = append(msgs, model.TextMessage(model.RoleSystem, fmt.Sprintf(contextTemplate, content.String())))
	}

	// Injected file context.
	for _, f := range q.Files {
		msgs = append(msgs, model.TextMessage(model.RoleUser, fmt.Sprintf("File: %s\n\n%s", f.Name, f.Content)))
	}

	// Session history (persisted turns).
	for _, hm := range s.ChatHistory {
		msgs = append(msgs, model.TextMessage(hm.Role, hm.Content))
	}

	// User question.
	msgs = append(msgs, model.TextMessage(model.RoleUser, q.Content))

	return msgs
}

func collectResponse(krn *kronk.Kronk, ch <-chan model.ChatResponse, onToken func(string), onReasoning func(string)) (*Answer, error) {
	var reasoning bool
	var lr model.ChatResponse
	var sb strings.Builder

loop:
	for resp := range ch {
		lr = resp

		switch resp.Choice[0].FinishReason() {
		case model.FinishReasonError:
			return nil, fmt.Errorf("error from model: %s", resp.Choice[0].Delta.Content)

		case model.FinishReasonStop:
			break loop

		case model.FinishReasonTool:
			tc := resp.Choice[0].Delta.ToolCalls[0]
			slog.Info("model tool call", "tool_id", tc.ID, "function", tc.Function.Name, "arguments", tc.Function.Arguments)
			break loop

		default:
			if r := resp.Choice[0].Delta.Reasoning; r != "" {
				reasoning = true
				if onReasoning != nil {
					onReasoning(r)
				}
				continue
			}

			if reasoning {
				reasoning = false
			}

			token := resp.Choice[0].Delta.Content
			sb.WriteString(token)
			if onToken != nil {
				onToken(token)
			}
		}
	}

	contextTokens := lr.Usage.PromptTokens + lr.Usage.CompletionTokens
	contextWindow := krn.ModelConfig().ContextWindow

	slog.Info("model usage",
		"prompt_tokens", lr.Usage.PromptTokens,
		"reasoning_tokens", lr.Usage.ReasoningTokens,
		"completion_tokens", lr.Usage.CompletionTokens,
		"output_tokens", lr.Usage.OutputTokens,
		"context_tokens", contextTokens,
		"context_window", contextWindow,
		"tps", lr.Usage.TokensPerSecond,
	)

	return &Answer{
		Content:          sb.String(),
		PromptTokens:     lr.Usage.PromptTokens,
		ReasoningTokens:  lr.Usage.ReasoningTokens,
		CompletionTokens: lr.Usage.CompletionTokens,
		OutputTokens:     lr.Usage.OutputTokens,
		TokensPerSecond:  lr.Usage.TokensPerSecond,
		ContextWindow:    contextWindow,
		ContextTokens:    contextTokens,
	}, nil
}
