package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ardanlabs/kronk/sdk/kronk"
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/ardanlabs/kronk/sdk/tools/catalog"
	"github.com/ardanlabs/kronk/sdk/tools/defaults"
	"github.com/ardanlabs/kronk/sdk/tools/libs"
	"github.com/ardanlabs/kronk/sdk/tools/models"
)

// RankedDoc holds a document text with its reranker relevance score.
type RankedDoc struct {
	Index          int
	Text           string
	RelevanceScore float64
}

// Engine holds loaded Kronk model instances. Models are lazy-loaded on first use.
type Engine struct {
	mu        sync.Mutex
	embedKrn  *kronk.Kronk
	rerankKrn *kronk.Kronk
	chatKrn   *kronk.Kronk

	embedURL  string
	rerankURL string
	chatURL   string

	initialized bool
	mdls        *models.Models
	ctlg        *catalog.Catalog
}

// New returns an Engine with the given model URLs.
func New(embedURL, rerankURL, chatURL string) *Engine {
	return &Engine{
		embedURL:  embedURL,
		rerankURL: rerankURL,
		chatURL:   chatURL,
	}
}

// Init initialises the Kronk runtime (llama.cpp libs + catalog). Call once at startup.
func (e *Engine) Init(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		return nil
	}

	libsMgr, err := libs.New(libs.WithVersion(defaults.LibVersion("")))
	if err != nil {
		return fmt.Errorf("engine: libs: %w", err)
	}

	if _, err := libsMgr.Download(ctx, kronk.FmtLogger); err != nil {
		return fmt.Errorf("engine: libs download: %w", err)
	}

	ctlg, err := catalog.New()
	if err != nil {
		return fmt.Errorf("engine: catalog: %w", err)
	}

	if err := ctlg.Download(ctx); err != nil {
		return fmt.Errorf("engine: catalog download: %w", err)
	}

	mdls, err := models.New()
	if err != nil {
		return fmt.Errorf("engine: models: %w", err)
	}

	if err := kronk.Init(); err != nil {
		return fmt.Errorf("engine: kronk init: %w", err)
	}

	e.ctlg = ctlg
	e.mdls = mdls
	e.initialized = true

	return nil
}

func (e *Engine) loadModel(ctx context.Context, url string) (*kronk.Kronk, error) {
	mp, err := e.mdls.Download(ctx, kronk.FmtLogger, url, "")
	if err != nil {
		return nil, fmt.Errorf("engine: download model %s: %w", url, err)
	}

	cfg := model.Config{ModelFiles: mp.ModelFiles}

	krn, err := kronk.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("engine: load model %s: %w", url, err)
	}

	return krn, nil
}

func (e *Engine) getEmbed(ctx context.Context) (*kronk.Kronk, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.embedKrn != nil {
		return e.embedKrn, nil
	}

	krn, err := e.loadModel(ctx, e.embedURL)
	if err != nil {
		return nil, err
	}

	e.embedKrn = krn

	return krn, nil
}

func (e *Engine) getRerank(ctx context.Context) (*kronk.Kronk, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.rerankKrn != nil {
		return e.rerankKrn, nil
	}

	krn, err := e.loadModel(ctx, e.rerankURL)
	if err != nil {
		return nil, err
	}

	e.rerankKrn = krn

	return krn, nil
}

func (e *Engine) getChat(ctx context.Context) (*kronk.Kronk, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.chatKrn != nil {
		return e.chatKrn, nil
	}

	krn, err := e.loadModel(ctx, e.chatURL)
	if err != nil {
		return nil, err
	}

	e.chatKrn = krn

	return krn, nil
}

// Embed returns the embedding vector for a text string.
func (e *Engine) Embed(ctx context.Context, text string) ([]float32, error) {
	krn, err := e.getEmbed(ctx)
	if err != nil {
		return nil, err
	}

	d := model.D{
		"input":              text,
		"truncate":           true,
		"truncate_direction": "right",
	}

	resp, err := krn.Embeddings(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("engine: embed: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("engine: embed: no data returned")
	}

	return resp.Data[0].Embedding, nil
}

// Rerank reranks documents by relevance to query and returns sorted results.
func (e *Engine) Rerank(ctx context.Context, query string, docs []string) ([]RankedDoc, error) {
	krn, err := e.getRerank(ctx)
	if err != nil {
		return nil, err
	}

	d := model.D{
		"query":            query,
		"documents":        docs,
		"top_n":            len(docs),
		"return_documents": true,
	}

	resp, err := krn.Rerank(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("engine: rerank: %w", err)
	}

	ranked := make([]RankedDoc, 0, len(resp.Data))

	for _, r := range resp.Data {
		ranked = append(ranked, RankedDoc{
			Index:          r.Index,
			Text:           r.Document,
			RelevanceScore: float64(r.RelevanceScore),
		})
	}

	return ranked, nil
}

// ChatStream sends messages and returns a channel of streaming responses.
func (e *Engine) ChatStream(ctx context.Context, msgs []model.D) (<-chan model.ChatResponse, error) {
	krn, err := e.getChat(ctx)
	if err != nil {
		return nil, err
	}

	d := model.D{
		"messages":   msgs,
		"max_tokens": 2048,
	}

	ch, err := krn.ChatStreaming(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("engine: chat stream: %w", err)
	}

	return ch, nil
}

// Summarize generates a short summary of the given text using the chat model.
func (e *Engine) Summarize(text string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	krn, err := e.getChat(ctx)
	if err != nil {
		return "", err
	}

	msgs := model.DocumentArray(
		model.TextMessage(model.RoleSystem, "You are a summarizer. Respond with a single concise sentence (max 30 words) summarizing the document."),
		model.TextMessage(model.RoleUser, "Summarize:\n\n"+text[:min(len(text), 2000)]),
	)

	d := model.D{
		"messages":   msgs,
		"max_tokens": 80,
	}

	ch, err := krn.ChatStreaming(ctx, d)
	if err != nil {
		return "", fmt.Errorf("engine: summarize: %w", err)
	}

	var sb strings.Builder

	for resp := range ch {
		if len(resp.Choice) == 0 {
			continue
		}

		if resp.Choice[0].FinishReason() == model.FinishReasonStop || resp.Choice[0].FinishReason() == model.FinishReasonError {
			break
		}

		sb.WriteString(resp.Choice[0].Delta.Content)
	}

	return sb.String(), nil
}

// Close unloads all loaded models.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ctx := context.Background()

	if e.embedKrn != nil {
		_ = e.embedKrn.Unload(ctx)
	}

	if e.rerankKrn != nil {
		_ = e.rerankKrn.Unload(ctx)
	}

	if e.chatKrn != nil {
		_ = e.chatKrn.Unload(ctx)
	}
}
