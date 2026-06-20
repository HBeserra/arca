package enginebus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ardanlabs/kronk/sdk/kronk"
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/google/uuid"
)

const maxToolRounds = 10

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
		SessionID   uuid.UUID
		Content     string
		Fragments   []Fragment
		Files       []FileContext
		System      string
		Gateway     ToolGateway
		Language    string // e.g. "English", "Portuguese". Empty defaults to English.
		MaxTokens   int
		Temperature float64
		TopP        float64
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
		TotalContextUsed uint64 // cumulative tokens consumed in this session
	}

	// ChatEvent is one streamed item from ChatStream. Exactly one of Token,
	// Reasoning, Answer, ToolCall, or Err is set per event.
	ChatEvent struct {
		Token     string        // non-empty: streaming content token
		Reasoning string        // non-empty: streaming reasoning/thinking token
		Answer    *Answer       // non-nil: final answer (stream complete)
		ToolCall  *ToolCallInfo // non-nil: tool was invoked (Result empty = starting, non-empty = done)
		Err       error         // non-nil: error (stream terminated)
	}

	// toolCallRequest carries the pending tool invocation from the model.
	toolCallRequest struct {
		ID   string
		Name string
		Args map[string]any
	}
)

// Chat performs a non-streaming chat request and returns the complete answer.
func (e *Engine) Chat(ctx context.Context, q Question) (Answer, error) {
	session, msgs, cancel, err := e.prepareChat(ctx, q)
	if err != nil {
		return Answer{}, err
	}
	defer cancel()

	var toolSchemas []model.D
	if q.Gateway != nil {
		toolSchemas = buildToolSchemas(q.Gateway.Tools())
	}

	e.logger.Debug("chat start",
		"session_id", q.SessionID,
		"question", q.Content,
		"language", q.Language,
		"fragments", len(q.Fragments),
		"tools", len(toolSchemas),
		"history_turns", len(session.ChatHistory)/2,
	)

	var finalAnswer *Answer
	for round := range maxToolRounds {
		e.logger.Debug("chat invoke", "round", round)

		ch, err := e.invokeChat(ctx, msgs, toolSchemas, q)
		if err != nil {
			return Answer{}, fmt.Errorf("chat: invoke: %w", err)
		}

		answer, tc, err := collectResponse(e.krnChat, ch, nil, nil)
		if err != nil {
			return Answer{}, fmt.Errorf("chat: model response: %w", err)
		}

		if tc != nil {
			e.logger.Debug("chat tool call", "round", round, "tool", tc.Name, "id", tc.ID, "args", tc.Args)
			result, execErr := executeToolCall(ctx, q.Gateway, tc)
			if execErr != nil {
				e.logger.Debug("chat tool error", "tool", tc.Name, "error", execErr)
				result = fmt.Sprintf("error: %s", execErr)
			} else {
				e.logger.Debug("chat tool result", "tool", tc.Name, "result_len", len(result))
			}
			msgs = appendToolResult(msgs, tc, result)
			continue
		}

		finalAnswer = answer
		break
	}

	if finalAnswer == nil {
		return Answer{}, fmt.Errorf("chat: exceeded max tool rounds (%d)", maxToolRounds)
	}

	e.logger.Debug("chat done",
		"answer_len", len(finalAnswer.Content),
		"prompt_tokens", finalAnswer.PromptTokens,
		"completion_tokens", finalAnswer.CompletionTokens,
		"tps", finalAnswer.TokensPerSecond,
	)

	finalAnswer.TotalContextUsed = e.persistHistory(session, q.Content, finalAnswer.Content, finalAnswer)
	return *finalAnswer, nil
}

// ChatStream sends a Question and returns a channel of streaming ChatEvents.
func (e *Engine) ChatStream(ctx context.Context, q Question) (<-chan ChatEvent, error) {
	session, msgs, cancel, err := e.prepareChat(ctx, q)
	if err != nil {
		return nil, err
	}

	var toolSchemas []model.D
	if q.Gateway != nil {
		toolSchemas = buildToolSchemas(q.Gateway.Tools())
	}

	e.logger.Debug("chat stream start",
		"session_id", q.SessionID,
		"question", q.Content,
		"language", q.Language,
		"fragments", len(q.Fragments),
		"tools", len(toolSchemas),
		"history_turns", len(session.ChatHistory)/2,
	)

	out := make(chan ChatEvent)

	go func() {
		defer close(out)
		defer cancel()

		var reasoningBuf strings.Builder
		var tokenBuf strings.Builder

		onToken := func(token string) {
			tokenBuf.WriteString(token)
			out <- ChatEvent{Token: token}
		}
		onReasoning := func(r string) {
			reasoningBuf.WriteString(r)
			out <- ChatEvent{Reasoning: r}
		}

		var finalAnswer *Answer
		for round := range maxToolRounds {
			// Reset per-round buffers.
			reasoningBuf.Reset()
			tokenBuf.Reset()

			e.logger.Debug("chat stream invoke", "round", round)

			ch, err := e.invokeChat(ctx, msgs, toolSchemas, q)
			if err != nil {
				out <- ChatEvent{Err: fmt.Errorf("chat stream: invoke: %w", err)}
				return
			}

			answer, tc, err := collectResponse(e.krnChat, ch, onToken, onReasoning)
			if err != nil {
				out <- ChatEvent{Err: err}
				return
			}

			if reasoningBuf.Len() > 0 {
				e.logger.Debug("chat stream thinking", "round", round, "thinking_len", reasoningBuf.Len())
			}

			if tc != nil {
				e.logger.Debug("chat stream tool call",
					"round", round,
					"tool", tc.Name,
					"id", tc.ID,
					"args", tc.Args,
				)

				out <- ChatEvent{ToolCall: &ToolCallInfo{Name: tc.Name, Args: tc.Args}}

				result, execErr := executeToolCall(ctx, q.Gateway, tc)
				if execErr != nil {
					e.logger.Debug("chat stream tool error", "tool", tc.Name, "error", execErr)
					result = fmt.Sprintf("error: %s", execErr)
				} else {
					e.logger.Debug("chat stream tool result", "tool", tc.Name, "result_len", len(result))
				}

				out <- ChatEvent{ToolCall: &ToolCallInfo{Name: tc.Name, Args: tc.Args, Result: result}}

				msgs = appendToolResult(msgs, tc, result)
				continue
			}

			finalAnswer = answer
			break
		}

		if finalAnswer == nil {
			out <- ChatEvent{Err: fmt.Errorf("exceeded max tool rounds (%d)", maxToolRounds)}
			return
		}

		e.logger.Debug("chat stream done",
			"answer_len", len(finalAnswer.Content),
			"prompt_tokens", finalAnswer.PromptTokens,
			"completion_tokens", finalAnswer.CompletionTokens,
			"tps", finalAnswer.TokensPerSecond,
		)

		finalAnswer.TotalContextUsed = e.persistHistory(session, q.Content, finalAnswer.Content, finalAnswer)
		out <- ChatEvent{Answer: finalAnswer}
	}()

	return out, nil
}

// persistHistory appends the user/assistant turn to the session, accumulates
// token usage, saves it, and returns the updated cumulative context total.
func (e *Engine) persistHistory(session Session, userContent, assistantContent string, answer *Answer) uint64 {
	session.ChatHistory = append(session.ChatHistory,
		ChatMessage{Role: model.RoleUser, Content: userContent},
		ChatMessage{Role: model.RoleAssistant, Content: assistantContent},
	)
	session.ContextUsed += uint64(answer.PromptTokens + answer.CompletionTokens)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.store.UpdateSession(ctx, session); err != nil {
		e.logger.Error("failed to persist chat history", "sessionID", session.ID, "error", err)
	}
	return session.ContextUsed
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

// prepareChat loads the model if needed, fetches the session, and builds the
// initial message list. It returns a cancel function for the derived context.
func (e *Engine) prepareChat(ctx context.Context, q Question) (Session, []model.D, context.CancelFunc, error) {
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

	msgs := e.buildMessages(session, q)

	e.logger.Debug("chat messages built",
		"total_messages", len(msgs),
		"system_prompt_len", len(q.System),
		"user_question_len", len(q.Content),
		"system_prompt", q.System,
		"user_question", q.Content,
	)

	return session, msgs, cancel, nil
}

// invokeChat calls the model with the given message list and optional tool schemas.
func (e *Engine) invokeChat(ctx context.Context, msgs []model.D, toolSchemas []model.D, q Question) (<-chan model.ChatResponse, error) {
	maxTokens := q.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	temperature := q.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	topP := q.TopP
	if topP == 0 {
		topP = 0.9
	}
	d := model.D{
		"messages":    msgs,
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"top_p":       topP,
		"top_k":       40,
	}

	if len(toolSchemas) > 0 {
		d["tools"] = toolSchemas
		d["tool_choice"] = "auto"
	}

	ch, err := e.krnChat.ChatStreaming(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("chat streaming: %w", err)
	}
	return ch, nil
}

// buildMessages converts internal types into the kronk message array for the API.
func (e *Engine) buildMessages(s Session, q Question) []model.D {
	var msgs []model.D

	// System prompt.
	systemPrompt := q.System
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant. Answer questions using the provided context. \nIf the context does not contain the answer, say so."
	}

	// Append language instruction.
	lang := q.Language
	if lang == "" {
		lang = "English"
	}
	systemPrompt += "\n\nAlways respond to the user in " + lang + ". Your entire response must be written in " + lang + "."

	// Append tool awareness hint when tools are available.
	if q.Gateway != nil && len(q.Gateway.Tools()) > 0 {
		var names []string
		for _, t := range q.Gateway.Tools() {
			names = append(names, t.Name())
		}
		systemPrompt += "\n\nYou have access to the following tools: " + strings.Join(names, ", ") + "." +
			" Use them whenever they can help answer the user's question more accurately." +
			" Use list_files to see which files are in the session." +
			" Use find_file to locate a file by name when you don't know its full path." +
			" Use read_file to read file contents — it returns up to 8000 characters; if has_more is true, call it again with a higher offset to read the next chunk."
	}

	if len(q.Fragments) <= 0 {
		msgs = append(msgs, model.TextMessage(model.RoleSystem, systemPrompt))
	}

	// RAG fragment context (up to 2 fragments).
	if len(q.Fragments) > 0 || len(q.Files) > 0 {
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
			fmt.Fprintf(&content, "--- Context Fragment %s:%d ---\n%s\n--- end fragment ---\n\n", doc.Path, doc.StartLine, doc.Text)
		}

		for i, doc := range q.Files {
			if i >= 2 {
				break
			}
			fmt.Fprintf(&content, "--- Context File %s ---\n%s\n--- end file ---\n\n", doc.Name, doc.Content)
		}

		systemPrompt += "\n\n" + fmt.Sprintf(contextTemplate, content.String())

		msgs = append(msgs, model.TextMessage(model.RoleSystem, fmt.Sprintf(contextTemplate, content.String())))
	}

	// Session history (persisted turns).
	for _, hm := range s.ChatHistory {
		msgs = append(msgs, model.TextMessage(hm.Role, hm.Content))
	}

	// User question.
	msgs = append(msgs, model.TextMessage(model.RoleUser, q.Content))

	e.logger.Debug("initial chat messages",
		"message_count", len(msgs),
		"system_prompt_len", len(systemPrompt),
		"user_question_len", len(q.Content),
		"system_prompt", systemPrompt,
		"user_question", q.Content,
	)
	return msgs
}

// collectResponse drains the model stream and returns either a complete Answer
// or a pending toolCallRequest when the model requests a tool.
func collectResponse(krn *kronk.Kronk, ch <-chan model.ChatResponse, onToken func(string), onReasoning func(string)) (*Answer, *toolCallRequest, error) {
	var reasoning bool
	var lr model.ChatResponse
	var sb strings.Builder
	var pending *toolCallRequest

loop:
	for resp := range ch {
		lr = resp

		switch resp.Choice[0].FinishReason() {
		case model.FinishReasonError:
			return nil, nil, fmt.Errorf("error from model: %s", resp.Choice[0].Delta.Content)

		case model.FinishReasonStop:
			break loop

		case model.FinishReasonTool:
			tc := resp.Choice[0].Delta.ToolCalls[0]
			slog.Debug("model requested tool call",
				"tool_id", tc.ID,
				"function", tc.Function.Name,
				"arguments", tc.Function.Arguments,
			)
			pending = &toolCallRequest{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: tc.Function.Arguments,
			}
			break loop

		default:
			if r := resp.Choice[0].Delta.Reasoning; r != "" {
				if !reasoning {
					slog.Debug("model thinking started")
					reasoning = true
				}
				if onReasoning != nil {
					onReasoning(r)
				}
				continue
			}

			if reasoning {
				slog.Debug("model thinking ended")
				reasoning = false
			}

			token := resp.Choice[0].Delta.Content
			sb.WriteString(token)
			if onToken != nil {
				onToken(token)
			}
		}
	}

	if pending != nil {
		return nil, pending, nil
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
	}, nil, nil
}

// executeToolCall finds the named tool in the gateway and runs it.
func executeToolCall(ctx context.Context, gw ToolGateway, tc *toolCallRequest) (string, error) {
	if gw == nil {
		return "", fmt.Errorf("no tool gateway configured")
	}
	for _, t := range gw.Tools() {
		if t.Name() == tc.Name {
			return t.Execute(ctx, tc.Args)
		}
	}
	return "", fmt.Errorf("tool %q not found", tc.Name)
}

// appendToolResult injects the assistant tool_calls message and the tool result
// message into msgs, returning the extended slice.
func appendToolResult(msgs []model.D, tc *toolCallRequest, result string) []model.D {
	argsJSON, _ := json.Marshal(tc.Args)

	// Assistant message declaring the tool call.
	msgs = append(msgs, model.D{
		"role": model.RoleAssistant,
		"tool_calls": []model.D{
			{
				"id":   tc.ID,
				"type": "function",
				"function": model.D{
					"name":      tc.Name,
					"arguments": string(argsJSON),
				},
			},
		},
	})

	// Tool result message.
	msgs = append(msgs, model.D{
		"role":         "tool",
		"tool_call_id": tc.ID,
		"content":      result,
	})

	return msgs
}

// buildToolSchemas converts Tool implementations to model.D tool definitions.
func buildToolSchemas(tools []Tool) []model.D {
	schemas := make([]model.D, len(tools))
	for i, t := range tools {
		schemas[i] = t.Schema()
	}
	return schemas
}
