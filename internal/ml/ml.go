// Package ml is the catalog's local machine-learning layer built on Kronk
// (llama.cpp): a text embedder for similarity search and a vision classifier for
// semantic attributes. It follows the canonical Kronk loading pattern — download
// libs, download the model (with auto-discovered mmproj for vision), then
// kronk.New with functional options.
package ml

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/ardanlabs/kronk/sdk/kronk"
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/ardanlabs/kronk/sdk/tools/defaults"
	"github.com/ardanlabs/kronk/sdk/tools/libs"
	"github.com/ardanlabs/kronk/sdk/tools/models"
)

// ensureProcessorEnv works around a Kronk v1.28.0 quirk on Apple Silicon: the
// library downloader defaults to the cpu backend while the runtime loads metal,
// so a default install fails with a missing metal/libggml.dylib. Forcing
// KRONK_PROCESSOR=metal aligns both onto the GPU backend. No-op when already set
// or off darwin/arm64 (other platforms auto-detect correctly).
func ensureProcessorEnv() {
	if os.Getenv("KRONK_PROCESSOR") != "" {
		return
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		_ = os.Setenv("KRONK_PROCESSOR", "metal")
	}
}

// Defaults — override via options.
const (
	DefaultEmbedModel = "ggml-org/embeddinggemma-300m-qat-q8_0-GGUF/embeddinggemma-300m-qat-Q8_0.gguf"
	// A full HuggingFace URL is the most reliable source form: it triggers a
	// direct download with automatic mmproj sibling discovery, independent of the
	// resolver catalog. Swap the URL (or pass WithVisionModel) for another VLM.
	DefaultVisionModel = "https://huggingface.co/ggml-org/Qwen2.5-VL-3B-Instruct-GGUF/resolve/main/Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"
	EmbedDim           = 768
)

// Classification is the structured result of classifying a design image. The JSON
// schema below constrains the vision model to exactly these fields.
type Classification struct {
	Caption  string   `json:"caption"`
	Elements []string `json:"elements"`
	Style    string   `json:"style"`
	Theme    string   `json:"theme"`
	Mood     string   `json:"mood"`
	Tags     []string `json:"tags"`
}

// Engine lazily loads and holds the Kronk models. Methods load their model on
// first use; loads are guarded so concurrent callers wait rather than double-load.
type Engine struct {
	log         *slog.Logger
	libVersion  string
	embedModel  string
	visionModel string

	mu        sync.Mutex
	sysReady  bool
	krnEmbed  *kronk.Kronk
	krnVision *kronk.Kronk
}

// Option configures the Engine.
type Option func(*Engine)

// WithEmbedModel overrides the embedding model source.
func WithEmbedModel(s string) Option { return func(e *Engine) { if s != "" { e.embedModel = s } } }

// WithVisionModel overrides the vision model source.
func WithVisionModel(s string) Option { return func(e *Engine) { if s != "" { e.visionModel = s } } }

// New creates the ML engine (no models loaded yet).
func New(log *slog.Logger, opts ...Option) *Engine {
	ensureProcessorEnv()
	e := &Engine{
		log:         log,
		libVersion:  defaults.LibVersion(""),
		embedModel:  DefaultEmbedModel,
		visionModel: DefaultVisionModel,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// ensureSystem downloads the llama.cpp libraries and initialises Kronk once.
// Caller must hold e.mu.
func (e *Engine) ensureSystem(ctx context.Context) error {
	if e.sysReady {
		return nil
	}
	lb, err := libs.New(libs.WithVersion(e.libVersion))
	if err != nil {
		return fmt.Errorf("ml: libs new: %w", err)
	}
	if _, err := lb.Download(ctx, kronk.FmtLogger); err != nil {
		return fmt.Errorf("ml: download libs: %w", err)
	}
	if err := kronk.Init(); err != nil {
		return fmt.Errorf("ml: kronk init: %w", err)
	}
	e.sysReady = true
	return nil
}

// downloadModel fetches a model (and its mmproj, if any) and returns its paths.
func (e *Engine) downloadModel(ctx context.Context, source string) (models.Path, error) {
	mdls, err := models.New()
	if err != nil {
		return models.Path{}, fmt.Errorf("ml: models new: %w", err)
	}
	mp, err := mdls.Download(ctx, kronk.FmtLogger, source)
	if err != nil {
		return models.Path{}, fmt.Errorf("ml: download %q: %w", source, err)
	}
	return mp, nil
}

// EnsureEmbed loads the embedding model if it is not already loaded.
func (e *Engine) EnsureEmbed(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.krnEmbed != nil {
		return nil
	}
	if err := e.ensureSystem(ctx); err != nil {
		return err
	}
	mp, err := e.downloadModel(ctx, e.embedModel)
	if err != nil {
		return err
	}
	krn, err := kronk.New(
		model.WithModelFiles(mp.ModelFiles),
		model.WithAutoTune(true),
	)
	if err != nil {
		return fmt.Errorf("ml: new embed model: %w", err)
	}
	e.krnEmbed = krn
	e.log.Info("ml: embedding model loaded", "source", e.embedModel)
	return nil
}

// EnsureVision loads the vision model (with its mmproj) if not already loaded.
func (e *Engine) EnsureVision(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.krnVision != nil {
		return nil
	}
	if err := e.ensureSystem(ctx); err != nil {
		return err
	}
	mp, err := e.downloadModel(ctx, e.visionModel)
	if err != nil {
		return err
	}
	if mp.ProjFile == "" {
		return fmt.Errorf("ml: vision model %q has no mmproj (not a multimodal model?)", e.visionModel)
	}
	krn, err := kronk.New(
		model.WithModelFiles(mp.ModelFiles),
		model.WithProjFile(mp.ProjFile),
		model.WithAutoTune(true),
	)
	if err != nil {
		return fmt.Errorf("ml: new vision model: %w", err)
	}
	e.krnVision = krn
	e.log.Info("ml: vision model loaded", "source", e.visionModel)
	return nil
}

// Embed returns the embedding vector for text, loading the model on first use.
func (e *Engine) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := e.EnsureEmbed(ctx); err != nil {
		return nil, err
	}
	resp, err := e.krnEmbed.Embeddings(ctx, model.D{"input": text, "truncate": true})
	if err != nil {
		return nil, fmt.Errorf("ml: embed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("ml: embed: empty response")
	}
	return resp.Data[0].Embedding, nil
}

const classifyPrompt = `You are cataloguing a machine-embroidery design from its rendered image.
Describe what is depicted as accurately as possible. Respond with: a single concise caption;
the concrete visual elements present; the visual style; the overall theme; the mood; and 3 to 8
short lowercase search tags.`

// Classify sends a rendered PNG to the vision model and returns structured
// attributes, constrained to the schema via Kronk's json_schema grammar.
func (e *Engine) Classify(ctx context.Context, png []byte) (Classification, error) {
	if err := e.EnsureVision(ctx); err != nil {
		return Classification{}, err
	}

	d := model.D{
		"messages":    model.ImageMessage(classifyPrompt, png, "png"),
		"json_schema": classificationSchema(),
		"temperature": 0.2,
		"top_p":       0.9,
		"max_tokens":  1024,
	}

	ch, err := e.krnVision.ChatStreaming(ctx, d)
	if err != nil {
		return Classification{}, fmt.Errorf("ml: classify stream: %w", err)
	}

	var sb strings.Builder
	for resp := range ch {
		if len(resp.Choices) == 0 {
			continue
		}
		c := resp.Choices[0]
		// Delta is a *ResponseMessage and is nil on non-content events (e.g. the
		// final finish_reason event).
		content := ""
		if c.Delta != nil {
			content = c.Delta.Content
		}
		if c.FinishReason() == "error" {
			return Classification{}, fmt.Errorf("ml: classify: model error: %s", content)
		}
		sb.WriteString(content)
	}

	var out Classification
	if err := json.Unmarshal([]byte(strings.TrimSpace(sb.String())), &out); err != nil {
		return Classification{}, fmt.Errorf("ml: classify: decode %q: %w", sb.String(), err)
	}
	return out, nil
}

// Complete generates a text completion constrained to the given JSON schema,
// reusing the vision model as a text LLM (Qwen2.5-VL handles text-only chat). Used
// for proposing the virtual-folder taxonomy. Loads the vision model on first use.
func (e *Engine) Complete(ctx context.Context, prompt string, schema map[string]any) (string, error) {
	if err := e.EnsureVision(ctx); err != nil {
		return "", err
	}

	d := model.D{
		"messages":    []model.D{{"role": "user", "content": prompt}},
		"temperature": 0.3,
		"top_p":       0.9,
		"max_tokens":  2048,
	}
	if len(schema) > 0 {
		d["json_schema"] = model.D(schema)
	}

	ch, err := e.krnVision.ChatStreaming(ctx, d)
	if err != nil {
		return "", fmt.Errorf("ml: complete stream: %w", err)
	}

	var sb strings.Builder
	for resp := range ch {
		if len(resp.Choices) == 0 {
			continue
		}
		c := resp.Choices[0]
		content := ""
		if c.Delta != nil {
			content = c.Delta.Content
		}
		if c.FinishReason() == "error" {
			return "", fmt.Errorf("ml: complete: model error: %s", content)
		}
		sb.WriteString(content)
	}
	return strings.TrimSpace(sb.String()), nil
}

// Close unloads any loaded models.
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var firstErr error
	for _, krn := range []*kronk.Kronk{e.krnEmbed, e.krnVision} {
		if krn == nil {
			continue
		}
		if err := krn.Unload(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	e.krnEmbed, e.krnVision = nil, nil
	return firstErr
}

// classificationSchema is the JSON schema Kronk converts to a GBNF grammar so the
// model can only emit a Classification.
func classificationSchema() model.D {
	str := model.D{"type": "string"}
	strArr := model.D{"type": "array", "items": model.D{"type": "string"}}
	return model.D{
		"type": "object",
		"properties": model.D{
			"caption":  str,
			"elements": strArr,
			"style":    str,
			"theme":    str,
			"mood":     str,
			"tags":     strArr,
		},
		"required": []string{"caption", "elements", "style", "theme", "mood", "tags"},
	}
}
