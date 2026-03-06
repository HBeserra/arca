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
	Question struct {
		SessionID uuid.UUID
		Content   string
		Fragments []Fragment
	}

	Answer struct {
		Content          string
		Messages         []model.D
		PromptTokens     int
		ReasoningTokens  int
		CompletionTokens int
		OutputTokens     int
		TokensPerSecond  float64
		ContextWindow    int
		ContextTokens    int
	}

	// QuestionEvent is emitted by QuestionStream. Exactly one of Token, Answer,
	// or Err is set per event; Answer and Err mark the end of the stream.
	QuestionEvent struct {
		Token  string
		Answer *Answer
		Err    error
	}
)

func (e *Engine) QuestionSync(ctx context.Context, q Question) (*Answer, error) {
	messages, ch, cancel, err := e.startQuestion(ctx, q)
	if err != nil {
		return nil, err
	}
	defer cancel()

	answer, err := modelResponse(e.krnChat, messages, ch, nil)
	if err != nil {
		return nil, fmt.Errorf("model response: %w", err)
	}

	return answer, nil
}

func (e *Engine) QuestionStream(ctx context.Context, q Question) (<-chan QuestionEvent, error) {
	messages, ch, cancel, err := e.startQuestion(ctx, q)
	if err != nil {
		return nil, err
	}

	out := make(chan QuestionEvent)

	go func() {
		defer close(out)
		defer cancel()

		onToken := func(token string) { out <- QuestionEvent{Token: token} }

		answer, err := modelResponse(e.krnChat, messages, ch, onToken)
		if err != nil {
			out <- QuestionEvent{Err: err}
			return
		}

		out <- QuestionEvent{Answer: answer}
	}()

	return out, nil
}

func (e *Engine) startQuestion(ctx context.Context, q Question) ([]model.D, <-chan model.ChatResponse, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)

	session, err := e.store.GetSession(ctx, q.SessionID)
	if err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("getting session: %w", err)
	}

	session.ChatHistory = append(session.ChatHistory, model.D{
		"role":    "user",
		"content": q.Content,
	})

	d := model.D{
		"messages":    embeddingPrompting(session, q),
		"max_tokens":  2048,
		"temperature": 0.7,
		"top_p":       0.9,
		"top_k":       40,
	}

	ch, err := e.krnChat.ChatStreaming(ctx, d)
	if err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("chat streaming: %w", err)
	}

	return session.ChatHistory, ch, cancel, nil
}

func embeddingPrompting(s Session, q Question) []model.D {
	// func addContextPrompt(documents []duck.Document, messages []model.D) []model.D {
	const prompt = `
		- Use the following Context to answer the user's question.
		- If you don't know the answer, say that you don't know.
		- Responses should be properly formatted to be easily read.
		- Share code if code is presented in the context.
		- Do not include any additional information not present in the context.

		Context:
		
		%s

		Question: %s
		`

	var count int
	var content strings.Builder
	for _, doc := range q.Fragments {
		fmt.Fprintf(&content, "%s\n%s\n", doc.Path, doc.Text)
		count++
		if count == 2 {
			break
		}
	}

	lastUserInput := s.ChatHistory[len(s.ChatHistory)-1]["content"].(string)
	finalPrompt := fmt.Sprintf(prompt, content.String(), lastUserInput)

	s.ChatHistory = append(s.ChatHistory, model.TextMessage("user", finalPrompt))

	return s.ChatHistory
}

func modelResponse(krn *kronk.Kronk, messages []model.D, ch <-chan model.ChatResponse, onToken func(string)) (*Answer, error) {
	var reasoning bool
	var lr model.ChatResponse
	var sb strings.Builder
	var reasoningSB strings.Builder

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

			messages = append(messages,
				model.TextMessage("tool", fmt.Sprintf("Tool call %s: %s(%v)",
					tc.ID, tc.Function.Name, tc.Function.Arguments),
				),
			)
			break loop

		default:
			if resp.Choice[0].Delta.Reasoning != "" {
				reasoningSB.WriteString(resp.Choice[0].Delta.Reasoning)
				reasoning = true
				continue
			}

			if reasoning {
				reasoning = false
				slog.Debug("model reasoning", "content", reasoningSB.String())
				reasoningSB.Reset()
			}

			token := resp.Choice[0].Delta.Content
			sb.WriteString(token)
			if onToken != nil {
				onToken(token)
			}
		}
	}

	// -------------------------------------------------------------------------

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

	messages = append(messages, model.TextMessage("assistant", sb.String()))

	return &Answer{
		Content:          sb.String(),
		Messages:         messages,
		PromptTokens:     lr.Usage.PromptTokens,
		ReasoningTokens:  lr.Usage.ReasoningTokens,
		CompletionTokens: lr.Usage.CompletionTokens,
		OutputTokens:     lr.Usage.OutputTokens,
		TokensPerSecond:  lr.Usage.TokensPerSecond,
		ContextWindow:    contextWindow,
		ContextTokens:    contextTokens,
	}, nil
}
