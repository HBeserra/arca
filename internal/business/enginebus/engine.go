// EngineBus is responsible for managing the llm engine and models
package enginebus

import (
	"changeme/internal/business/types/status"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ardanlabs/kronk/sdk/kronk"
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/ardanlabs/kronk/sdk/tools/catalog"
	"github.com/ardanlabs/kronk/sdk/tools/defaults"
	"github.com/ardanlabs/kronk/sdk/tools/libs"
	"github.com/ardanlabs/kronk/sdk/tools/models"
	"github.com/google/uuid"
)

//go:generate mockgen -source=engine.go -destination=mocks/engine.go -package=enginemock_test

type (

	// Engine manages the lifecycle of the LLM engine, including model loading and inference.
	Engine struct {
		logger    *slog.Logger
		store     Store
		krnEmbed  *kronk.Kronk // Embedding model instance
		krnChat   *kronk.Kronk // Chat model instance
		krnRerank *kronk.Kronk // Reranking model instance
		autoLoad  bool

		modelEmbedURL  string
		modelChatURL   string
		modelRerankURL string
	}

	// Vector is a slice of float32 representing the embedding vector for a document chunk.
	Vector []float32

	// RankedDoc holds a document text with its reranker relevance score.
	RankedDoc struct {
		Index          int
		Text           string
		RelevanceScore float64
	}

	// Store defines the interface for persisting sessions, documents, and their embeddings.
	Store interface {
		CreateSession(ctx context.Context, session Session) error
		GetSession(ctx context.Context, sessionID uuid.UUID) (Session, error)
		UpdateSession(ctx context.Context, session Session) error
		DeleteSession(ctx context.Context, sessionID uuid.UUID) error
		ListSessions(ctx context.Context) ([]Session, error)

		CreateDocument(ctx context.Context, doc Document) error
		UpdateDocument(ctx context.Context, doc Document) error
		AddDocumentChunk(ctx context.Context, documentID uuid.UUID, chunk string, vec Vector) error
		ListDocuments(ctx context.Context, sessionID uuid.UUID) ([]Document, error)
		SearchDocuments(ctx context.Context, sessionID uuid.UUID, queryVec []float32) ([]Fragment, error)
	}
)

type Option func(*Engine)

func WithChatModel(url string) Option {
	return func(e *Engine) { e.modelChatURL = url }
}

func WithEmbedModel(url string) Option {
	return func(e *Engine) { e.modelEmbedURL = url }
}

func WithRerankModel(url string) Option {
	return func(e *Engine) { e.modelRerankURL = url }
}

func New(logger *slog.Logger, store Store, opts ...Option) (*Engine, error) {
	e := &Engine{
		logger:        logger,
		store:         store,
		modelEmbedURL: "ggml-org/embeddinggemma-300m-qat-q8_0-GGUF/embeddinggemma-300m-qat-Q8_0.gguf",
		modelChatURL:  "unsloth/gpt-oss-20b-GGUF/gpt-oss-20b-Q8_0.gguf",
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.autoLoad {
		if err := e.Load(context.Background()); err != nil {
			return nil, fmt.Errorf("loading engine: %w", err)
		}
	}

	return e, nil
}

func (e *Engine) Load(ctx context.Context) error {
	e.logger.Info("Loading engine and preparing models")

	setupFuncs := []func(context.Context) error{
		e.loadLibs,
		// e.updateCatalog,
		e.loadModels,
	}

	for _, fn := range setupFuncs {
		if err := fn(ctx); err != nil {
			return fmt.Errorf("engine setup: %w", err)
		}
	}

	return nil
}

func (e *Engine) Eject(ctx context.Context) error {
	errs := []error{}

	if e.krnEmbed != nil {
		if err := e.krnEmbed.Unload(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if e.krnChat != nil {
		if err := e.krnChat.Unload(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if e.krnRerank != nil {
		if err := e.krnRerank.Unload(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors occurred while closing engine: %v", errs)
	}
	return nil
}

func (e *Engine) Close(ctx context.Context) error {
	e.logger.Info("Closing engine and releasing resources")

	if err := e.Eject(ctx); err != nil {
		return fmt.Errorf("error ejecting engine: %w", err)
	}

	return nil
}

func (e *Engine) CreateSession(ctx context.Context, name string, opts ...SessionOption) (Session, error) {
	session := Session{
		ID:            uuid.New(),
		Name:          name,
		CreatedAt:     time.Now(),
		ChatHistory:   []model.D{},
		BatchSize:     4096,
		BatchsOverlap: 512,
	}

	for _, opt := range opts {
		opt(&session)
	}

	err := e.store.CreateSession(ctx, session)
	if err != nil {
		return Session{}, fmt.Errorf("creating session: %w", err)
	}

	return session, nil
}

type AddDocumentInput struct {
	SessionID   uuid.UUID
	Name        string
	Path        string
	Text        string
	ContentType string
}

func (e *Engine) AddDocumentText(ctx context.Context, input AddDocumentInput) error {
	e.logger.Info("Adding text document to engine", "sessionID", input.SessionID, "name", input.Name, "path", input.Path)

	s, err := e.store.GetSession(ctx, input.SessionID)
	if err != nil {
		e.logger.Error("Error retrieving session for adding document", "sessionID", input.SessionID, "error", err)
		return fmt.Errorf("getting session: %w", err)
	}

	documentID := uuid.New()
	doc := Document{
		ID:          documentID,
		SessionID:   input.SessionID,
		Name:        input.Name,
		Path:        input.Path,
		ContentType: input.ContentType,
		Status:      status.Processing,
	}

	err = e.store.CreateDocument(ctx, doc)
	if err != nil {
		e.logger.Error("Error creating document in store", "documentID", documentID, "error", err)
		return fmt.Errorf("creating document: %w", err)
	}

	textBytes := []byte(input.Text)
	chunkIndex := 0
	var previousChunk []byte

	for i := 0; i < len(textBytes); i += s.BatchSize {
		// Calculate end position for current chunk
		end := i + s.BatchSize
		if end > len(textBytes) {
			end = len(textBytes)
		}

		// Create current chunk with overlap from previous chunk
		var currentChunk []byte

		if chunkIndex == 0 {
			// First chunk - no overlap
			currentChunk = textBytes[i:end]
		} else {
			// Subsequent chunks - prepend overlap from previous chunk
			overlapSize := s.BatchsOverlap
			if overlapSize > len(previousChunk) {
				overlapSize = len(previousChunk)
			}

			currentChunk = make([]byte, 0, overlapSize+end-i)
			currentChunk = append(currentChunk, previousChunk[len(previousChunk)-overlapSize:]...)
			currentChunk = append(currentChunk, textBytes[i:end]...)
		}

		chunkStr := string(currentChunk)

		// Get embeddings for this chunk
		vec, err := e.generateEmbedding(ctx, chunkStr)
		if err != nil {
			e.logger.Error("Error generating embedding for document chunk", "documentID", documentID, "chunkIndex", chunkIndex, "error", err)
			return fmt.Errorf("error generating embedding for chunk %d: %w", chunkIndex, err)
		}

		// Store chunk with embeddingsß
		err = e.store.AddDocumentChunk(ctx, documentID, chunkStr, vec)
		if err != nil {
			e.logger.Error("Error adding document chunk to store", "documentID", documentID, "chunkIndex", chunkIndex, "error", err)
			return fmt.Errorf("error inserting chunk %d: %w", chunkIndex, err)
		}

		previousChunk = currentChunk
		chunkIndex++
	}

	doc.Status = status.Completed
	e.logger.Info("Finished processing document", "sessionID", doc.SessionID, "documentID", documentID, "chunkCount", chunkIndex)
	e.store.UpdateDocument(ctx, doc)

	return nil
}

type AddDocumentStreamInput struct {
	SessionID   uuid.UUID
	Name        string
	Path        string
	Content     io.ReadCloser
	ContentType string
}

func (e *Engine) AddDocumentTextStream(ctx context.Context, doc AddDocumentStreamInput) error {
	defer doc.Content.Close()

	// Default chunk configuration
	const batchSize = 4096   // Size of each chunk to read
	const batchOverlap = 512 // Overlap between chunks

	buffer := make([]byte, batchSize)
	var previousChunk []byte
	chunkIndex := 0

	for {
		// Read next chunk
		n, err := doc.Content.Read(buffer)
		if n == 0 && err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error reading content: %w", err)
		}

		// Create current chunk with overlap from previous chunk
		var currentChunk []byte

		if chunkIndex == 0 {
			// First chunk - no overlap
			currentChunk = make([]byte, n)
			copy(currentChunk, buffer[:n])
		} else {
			// Subsequent chunks - prepend overlap from previous chunk
			overlapSize := batchOverlap
			if overlapSize > len(previousChunk) {
				overlapSize = len(previousChunk)
			}

			currentChunk = make([]byte, overlapSize+n)
			copy(currentChunk, previousChunk[len(previousChunk)-overlapSize:])
			copy(currentChunk[overlapSize:], buffer[:n])
		}

		chunkStr := string(currentChunk)

		// Get embeddings for this chunk
		vec, err := e.generateEmbedding(ctx, chunkStr)
		if err != nil {
			return fmt.Errorf("error generating embedding for chunk %d: %w", chunkIndex, err)
		}

		// Store chunk with embeddings
		err = e.store.AddDocumentChunk(ctx, doc.SessionID, chunkStr, vec)
		if err != nil {
			return fmt.Errorf("error inserting chunk %d: %w", chunkIndex, err)
		}

		previousChunk = currentChunk
		chunkIndex++

		// Check for EOF after storing chunk
		if err == io.EOF {
			break
		}
	}

	e.logger.Info("Finished processing stream", "chunkIndex", chunkIndex)
	return nil
}

func (e *Engine) SearchDocs(ctx context.Context, sessionID uuid.UUID, query string) ([]Fragment, error) {
	if e.krnEmbed == nil {
		return nil, fmt.Errorf("embedding model not loaded yet")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vectors, err := e.krnEmbed.Embeddings(ctx, model.D{"input": query})
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	if len(vectors.Data) == 0 || len(vectors.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("no embedding returned for query")
	}

	fragments, err := e.store.SearchDocuments(ctx, sessionID, vectors.Data[0].Embedding)
	if err != nil {
		return nil, fmt.Errorf("searching documents: %w", err)
	}

	return fragments, nil
}

func (e *Engine) generateEmbedding(ctx context.Context, text string) (Vector, error) {
	embedCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	d := model.D{
		"input":              text,
		"truncate":           true,
		"truncate_direction": "right",
	}

	resp, err := e.krnEmbed.Embeddings(embedCtx, d)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}

	if len(resp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty vector")
	}

	// Convert float32 to float64
	vec := make(Vector, len(resp.Data[0].Embedding))
	copy(vec, resp.Data[0].Embedding)

	return vec, nil
}

func (e *Engine) loadLibs(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	libs, err := libs.New(
		libs.WithVersion(defaults.LibVersion("")),
	)
	if err != nil {
		return fmt.Errorf("unable to create libs manager: %w", err)
	}

	if _, err := libs.Download(ctx, kronk.FmtLogger); err != nil {
		return fmt.Errorf("unable to install llama.cpp: %w", err)
	}

	return nil
}

func (e *Engine) updateCatalog(ctx context.Context) error {
	ctlg, err := catalog.New()
	if err != nil {
		return fmt.Errorf("unable to create catalog system: %w", err)
	}

	if err := ctlg.Download(ctx); err != nil {
		return fmt.Errorf("unable to download catalog: %w", err)
	}

	return nil
}

func (e *Engine) loadModels(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	mdls, err := models.New()
	if err != nil {
		return fmt.Errorf("unable to create models api: %w", err)
	}

	infoEmbed, err := mdls.Download(ctx, kronk.FmtLogger, e.modelEmbedURL, "")
	if err != nil {
		return fmt.Errorf("unable to install embed model: %w", err)
	}

	infoChat, err := mdls.Download(ctx, kronk.FmtLogger, e.modelChatURL, "")
	if err != nil {
		return fmt.Errorf("unable to install chat model: %w", err)
	}

	krnEmbed, err := e.newKronk(infoEmbed)
	if err != nil {
		return fmt.Errorf("unable to create embedding model: %w", err)
	}

	krnChat, err := e.newKronk(infoChat)
	if err != nil {
		return fmt.Errorf("unable to create chat model: %w", err)
	}

	e.krnEmbed = krnEmbed
	e.krnChat = krnChat

	if e.modelRerankURL != "" {
		infoRerank, err := mdls.Download(ctx, kronk.FmtLogger, e.modelRerankURL, "")
		if err != nil {
			return fmt.Errorf("unable to install rerank model: %w", err)
		}

		krnRerank, err := e.newKronk(infoRerank)
		if err != nil {
			return fmt.Errorf("unable to create rerank model: %w", err)
		}

		e.krnRerank = krnRerank
	}

	return nil
}

// ChatStream sends messages and returns a channel of streaming chat responses.
func (e *Engine) ChatStream(ctx context.Context, msgs []model.D) (<-chan model.ChatResponse, error) {
	if e.krnChat == nil {
		return nil, fmt.Errorf("chat model not loaded")
	}

	d := model.D{
		"messages":   msgs,
		"max_tokens": 2048,
	}

	ch, err := e.krnChat.ChatStreaming(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("chat stream: %w", err)
	}

	return ch, nil
}

// Rerank reranks documents by relevance to the query.
func (e *Engine) Rerank(ctx context.Context, query string, docs []string) ([]RankedDoc, error) {
	if e.krnRerank == nil {
		return nil, fmt.Errorf("rerank model not loaded")
	}

	d := model.D{
		"query":            query,
		"documents":        docs,
		"top_n":            len(docs),
		"return_documents": true,
	}

	resp, err := e.krnRerank.Rerank(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
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

// Summarize generates a short summary of text using the chat model.
func (e *Engine) Summarize(text string) (string, error) {
	if e.krnChat == nil {
		return "", fmt.Errorf("chat model not loaded")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	maxLen := len(text)
	if maxLen > 2000 {
		maxLen = 2000
	}

	msgs := model.DocumentArray(
		model.TextMessage(model.RoleSystem, "You are a summarizer. Respond with a single concise sentence (max 30 words) summarizing the document."),
		model.TextMessage(model.RoleUser, "Summarize:\n\n"+text[:maxLen]),
	)

	d := model.D{
		"messages":   msgs,
		"max_tokens": 80,
	}

	ch, err := e.krnChat.ChatStreaming(ctx, d)
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
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

// GetSession returns a single session by ID.
func (e *Engine) GetSession(ctx context.Context, sessionID uuid.UUID) (Session, error) {
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

// DeleteSession removes a session by ID.
func (e *Engine) DeleteSession(ctx context.Context, sessionID uuid.UUID) error {
	if err := e.store.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ListSessions returns all sessions from the store.
func (e *Engine) ListSessions(ctx context.Context) ([]Session, error) {
	sessions, err := e.store.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// ListDocuments returns all documents for a session.
func (e *Engine) ListDocuments(ctx context.Context, sessionID uuid.UUID) ([]Document, error) {
	docs, err := e.store.ListDocuments(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return docs, nil
}

func (e *Engine) newKronk(mp models.Path) (*kronk.Kronk, error) {
	if err := kronk.Init(); err != nil {
		return nil, fmt.Errorf("unable to init kronk: %w", err)
	}

	//TODO: move the config to engine level
	cfg := model.Config{
		ContextWindow:     32 * 1024,
		ModelFiles:        mp.ModelFiles,
		CacheTypeK:        model.GGMLTypeQ8_0,
		CacheTypeV:        model.GGMLTypeQ8_0,
		NSeqMax:           2,
		SystemPromptCache: true,
	}

	krn, err := kronk.New(cfg)

	if err != nil {
		return nil, fmt.Errorf("unable to create inference model: %w", err)
	}

	e.logger.Debug(
		"Kronk model loaded",
		"modelFiles", mp.ModelFiles,
		"contextWindow", krn.ModelConfig().ContextWindow,
		"isEmbedModel", krn.ModelInfo().IsEmbedModel,
		"isGPTModel", krn.ModelInfo().IsGPTModel,
		"template", krn.ModelInfo().Template.FileName,
		"systemInfo", krn.SystemInfo(),
	)

	return krn, nil
}
