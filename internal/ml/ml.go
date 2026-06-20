// Package ml is the catalog's local machine-learning layer built on Kronk
// (llama.cpp): a text embedder for similarity search and a vision classifier for
// semantic attributes. It follows the canonical Kronk loading pattern — download
// libs, download the model (with auto-discovered mmproj for vision), then
// kronk.New with functional options.
package ml

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/ardanlabs/kronk/sdk/kronk"
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/ardanlabs/kronk/sdk/tools/defaults"
	"github.com/ardanlabs/kronk/sdk/tools/libs"
	"github.com/ardanlabs/kronk/sdk/tools/models"
)

// withDefaultTimeout ensures ctx has a deadline — Kronk's inference calls require
// one. If the caller already set a deadline it is kept; otherwise d is applied.
func withDefaultTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

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
	// Vision model presets (full HuggingFace URLs — the reliable source form that
	// triggers a direct download with automatic mmproj sibling discovery). The
	// default is the balanced one; users switch via the UI / WithVisionModel.
	VisionFast       = "https://huggingface.co/ggml-org/SmolVLM2-2.2B-Instruct-GGUF/resolve/main/SmolVLM2-2.2B-Instruct-Q4_K_M.gguf"
	VisionBalanced   = "https://huggingface.co/ggml-org/Qwen2-VL-2B-Instruct-GGUF/resolve/main/Qwen2-VL-2B-Instruct-Q4_K_M.gguf"
	VisionQuality    = "https://huggingface.co/ggml-org/Qwen2.5-VL-3B-Instruct-GGUF/resolve/main/Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"
	DefaultVisionModel = VisionBalanced

	EmbedDim = 768

	// classifyImageSize is the max edge (px) the thumbnail is downscaled to before
	// classification. Vision-token count scales with image size and dominates
	// inference time, so this is the single biggest speed lever: 256px is ~4.5x
	// faster than 512 with no meaningful quality loss for embroidery line art. The
	// stored thumbnail stays full-size for the UI.
	classifyImageSize = 256
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

// Preset is a selectable vision model (shown in the UI picker).
type Preset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// VisionPresets returns the selectable vision models, fastest first.
func VisionPresets() []Preset {
	return []Preset{
		{ID: "fast", Label: "Rápido", Description: "SmolVLM2 2.2B — o mais rápido", URL: VisionFast},
		{ID: "balanced", Label: "Equilibrado", Description: "Qwen2-VL 2B — bom custo/qualidade", URL: VisionBalanced},
		{ID: "quality", Label: "Qualidade", Description: "Qwen2.5-VL 3B — melhor qualidade, mais lento", URL: VisionQuality},
	}
}

// Engine lazily loads and holds the Kronk models. Methods load their model on
// first use; loads are guarded so concurrent callers wait rather than double-load.
type Engine struct {
	log         *slog.Logger
	libVersion  string
	embedModel  string
	visionModel string // desired vision model source (URL)
	concurrency int    // NSeqMax / worker-pool size for parallel inference

	mu           sync.Mutex
	sysReady     bool
	krnEmbed     *kronk.Kronk
	krnVision    *kronk.Kronk
	loadedVision string // URL currently loaded in krnVision (for hot-swap)
}

// VisionModel reports the desired vision model source.
func (e *Engine) VisionModel() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.visionModel
}

// SetVisionModel changes the desired vision model. The switch is lazy: the new
// model loads on the next classification (EnsureVision reloads when the loaded
// model no longer matches). Callers must ensure no classification is in flight.
func (e *Engine) SetVisionModel(url string) {
	if url == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.visionModel = url
}

// Loaded reports whether any model is currently loaded in memory.
func (e *Engine) Loaded() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.krnEmbed != nil || e.krnVision != nil
}

// Unload frees both models from memory; they reload on demand on the next use.
func (e *Engine) Unload(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var firstErr error
	if e.krnEmbed != nil {
		if err := e.krnEmbed.Unload(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		e.krnEmbed = nil
	}
	if e.krnVision != nil {
		if err := e.krnVision.Unload(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		e.krnVision = nil
		e.loadedVision = ""
	}
	return firstErr
}

// Option configures the Engine.
type Option func(*Engine)

// WithEmbedModel overrides the embedding model source.
func WithEmbedModel(s string) Option { return func(e *Engine) { if s != "" { e.embedModel = s } } }

// WithVisionModel overrides the vision model source.
func WithVisionModel(s string) Option { return func(e *Engine) { if s != "" { e.visionModel = s } } }

// WithConcurrency sets how many inferences run in parallel (model NSeqMax and the
// caller's worker-pool size). Higher is faster but uses more VRAM per slot.
func WithConcurrency(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.concurrency = n
		}
	}
}

// Concurrency reports the configured parallel-inference width.
func (e *Engine) Concurrency() int { return e.concurrency }

// defaultConcurrency picks the parallel-inference width. Default is 1 (serial):
// benchmarked on Apple Silicon, a single 3B vision inference already saturates the
// GPU, so NSeqMax>1 only adds VRAM contention and is *slower*. Power users with a
// bigger GPU can opt in via $STITCHVAULT_AI_WORKERS.
func defaultConcurrency() int {
	if v := os.Getenv("STITCHVAULT_AI_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

// New creates the ML engine (no models loaded yet).
func New(log *slog.Logger, opts ...Option) *Engine {
	ensureProcessorEnv()
	e := &Engine{
		log:         log,
		libVersion:  defaults.LibVersion(""),
		embedModel:  DefaultEmbedModel,
		visionModel: DefaultVisionModel,
		concurrency: defaultConcurrency(),
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
	opts := []model.Option{
		model.WithModelFiles(mp.ModelFiles),
		model.WithAutoTune(true),
	}
	if e.concurrency > 1 {
		opts = append(opts, model.WithNSeqMax(e.concurrency))
	}
	krn, err := kronk.New(opts...)
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
	if e.krnVision != nil && e.loadedVision == e.visionModel {
		return nil
	}
	if e.krnVision != nil {
		// The desired model changed — unload the old one before loading the new.
		_ = e.krnVision.Unload(ctx)
		e.krnVision = nil
		e.loadedVision = ""
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
	opts := []model.Option{
		model.WithModelFiles(mp.ModelFiles),
		model.WithProjFile(mp.ProjFile),
		model.WithAutoTune(true),
	}
	if e.concurrency > 1 {
		// Multiple sequence slots + non-incremental cache so several
		// classifications can run in parallel against this one loaded model.
		opts = append(opts,
			model.WithNSeqMax(e.concurrency),
			model.WithIncrementalCache(false),
			model.WithContextWindow(8*1024),
		)
	}
	krn, err := kronk.New(opts...)
	if err != nil {
		return fmt.Errorf("ml: new vision model: %w", err)
	}
	e.krnVision = krn
	e.loadedVision = e.visionModel
	e.log.Info("ml: vision model loaded", "source", e.visionModel)
	return nil
}

// Embed returns the embedding vector for text, loading the model on first use.
func (e *Engine) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := e.EnsureEmbed(ctx); err != nil {
		return nil, err
	}
	ictx, cancel := withDefaultTimeout(ctx, 90*time.Second)
	defer cancel()
	resp, err := e.krnEmbed.Embeddings(ictx, model.D{"input": text, "truncate": true})
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
func (e *Engine) Classify(ctx context.Context, png []byte, hint string) (Classification, error) {
	if err := e.EnsureVision(ctx); err != nil {
		return Classification{}, err
	}

	// Downscaling the image is the biggest speed lever (see classifyImageSize).
	if small, derr := downscalePNG(png, classifyImageSize); derr == nil {
		png = small
	}

	prompt := classifyPrompt
	if h := strings.TrimSpace(hint); h != "" {
		prompt += fmt.Sprintf("\n\nThe embroidery file is named %q, which often hints at the subject — use it as a clue but classify what you actually see.", h)
	}

	d := model.D{
		"messages":        model.ImageMessage(prompt, png, "png"),
		"json_schema":     classificationSchema(),
		"temperature":     0.2,
		"top_p":           0.9,
		"enable_thinking": false,
		// Smaller VLMs (e.g. Qwen2-VL-2B) tend to loop the tags array — the
		// json_schema grammar can't cap array length. A mild repeat penalty breaks
		// the loop without starving the output (1.3 + frequency penalty produced
		// empty captions); mergeTags also dedups/caps downstream.
		"repeat_penalty": 1.15,
		// Bounded: the structured object is small, and a tight cap limits any
		// runaway the grammar still permits.
		"max_tokens": 512,
	}

	ictx, cancel := withDefaultTimeout(ctx, 4*time.Minute)
	defer cancel()
	content, err := e.streamText(ictx, e.krnVision, d)
	if err != nil {
		return Classification{}, fmt.Errorf("ml: classify: %w", err)
	}

	var out Classification
	if err := unmarshalLoose(content, &out); err != nil {
		return Classification{}, fmt.Errorf("ml: classify: decode %.160q: %w", content, err)
	}
	return out, nil
}

// streamText drains a chat-streaming response into a single string, tolerating
// nil deltas on non-content events.
func (e *Engine) streamText(ctx context.Context, krn *kronk.Kronk, d model.D) (string, error) {
	ch, err := krn.ChatStreaming(ctx, d)
	if err != nil {
		return "", fmt.Errorf("stream: %w", err)
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
			return "", fmt.Errorf("model error: %s", content)
		}
		sb.WriteString(content)
	}
	return strings.TrimSpace(sb.String()), nil
}

// Complete generates a text completion constrained to the given JSON schema,
// reusing the vision model as a text LLM (Qwen2.5-VL handles text-only chat). Used
// for proposing the virtual-folder taxonomy. Loads the vision model on first use.
func (e *Engine) Complete(ctx context.Context, prompt string, schema map[string]any) (string, error) {
	if err := e.EnsureVision(ctx); err != nil {
		return "", err
	}

	d := model.D{
		"messages":       []model.D{{"role": "user", "content": prompt}},
		"temperature":    0.3,
		"top_p":          0.9,
		"repeat_penalty": 1.15,
		"max_tokens":     1536,
	}
	if len(schema) > 0 {
		d["json_schema"] = model.D(schema)
	}

	ictx, cancel := withDefaultTimeout(ctx, 5*time.Minute)
	defer cancel()
	content, err := e.streamText(ictx, e.krnVision, d)
	if err != nil {
		return "", fmt.Errorf("ml: complete: %w", err)
	}
	return repairJSON(content), nil
}

// unmarshalLoose parses JSON, retrying with a bracket-balancing repair to survive
// the trailing-whitespace / unclosed-object runaway the json_schema grammar can
// produce when the model emits whitespace until max_tokens instead of closing.
func unmarshalLoose(s string, dst any) error {
	if err := json.Unmarshal([]byte(s), dst); err == nil {
		return nil
	}
	return json.Unmarshal([]byte(repairJSON(s)), dst)
}

// repairJSON trims trailing whitespace and appends any missing closing quote and
// brackets so a truncated-but-otherwise-valid JSON document parses.
func repairJSON(s string) string {
	s = strings.TrimRight(s, " \t\r\n")

	var stack []byte
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	var b strings.Builder
	b.WriteString(s)
	if inString {
		b.WriteByte('"')
	}
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteByte(stack[i])
	}
	return b.String()
}

// Close unloads any loaded models (alias for Unload, for app shutdown).
func (e *Engine) Close(ctx context.Context) error {
	return e.Unload(ctx)
}

// downscalePNG re-encodes a PNG scaled so its longest edge is at most maxEdge.
// Returns the original bytes unchanged when it is already small enough.
func downscalePNG(data []byte, maxEdge int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return data, nil
	}
	scale := float64(maxEdge) / float64(max(w, h))
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(w)*scale), int(float64(h)*scale)))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
