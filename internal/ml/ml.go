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
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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
	// triggers a direct download with automatic mmproj sibling discovery). Both are
	// verified to LOAD in the current Kronk/llama.cpp build. SmolVLM2 was dropped:
	// it downloads fine but llama.cpp fails to initialize its mtmd (multimodal)
	// context ("init-mtmd-meta-context: failed to initialize mtmd context"). Fast is
	// the default; users switch via the UI / WithVisionModel.
	VisionFast    = "https://huggingface.co/ggml-org/Qwen2-VL-2B-Instruct-GGUF/resolve/main/Qwen2-VL-2B-Instruct-Q4_K_M.gguf"
	VisionQuality = "https://huggingface.co/ggml-org/Qwen2.5-VL-3B-Instruct-GGUF/resolve/main/Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"

	DefaultVisionModel = VisionFast

	EmbedDim = 768

	// defaultClassifyPx is the max edge (px) the thumbnail is downscaled to before
	// classification. Vision-token count scales with image area and drives the
	// prefill cost, so this is a primary speed lever (512px is ~4.5x slower than
	// 256). Override per run via $STITCHVAULT_CLASSIFY_PX. The stored thumbnail
	// stays full-size for the UI.
	defaultClassifyPx = 192

	// defaultCaptionLang is the language the vision model writes the caption + tags
	// in (a CaptionLanguagePresets code). Portuguese by default.
	defaultCaptionLang = "pt"
)

// CaptionLang is a selectable description language (shown in the UI picker). The
// instruction (written IN the target language, which small VLMs follow far more
// reliably than an English "write in X") is internal.
type CaptionLang struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	instruction string
}

// CaptionLanguagePresets returns the selectable caption/tags languages.
func CaptionLanguagePresets() []CaptionLang {
	return []CaptionLang{
		{Code: "pt", Label: "Português", instruction: `IMPORTANTE: escreva a "caption" e cada item de "tags" em PORTUGUÊS do Brasil. Nunca use inglês.`},
		{Code: "en", Label: "English", instruction: `Write the "caption" and every "tags" entry in English.`},
		{Code: "es", Label: "Español", instruction: `IMPORTANTE: escribe la "caption" y cada elemento de "tags" en ESPAÑOL. Nunca uses inglés.`},
	}
}

// captionLangInstruction maps a language code to the in-language prompt directive,
// falling back to the default (Portuguese) for an unknown code.
func captionLangInstruction(code string) string {
	for _, l := range CaptionLanguagePresets() {
		if l.Code == code {
			return l.instruction
		}
	}
	return CaptionLanguagePresets()[0].instruction
}

// Classification is the structured result of classifying a design image. The lean
// schema (classificationSchema) makes the model generate only caption, tags and
// rotate — short output is the main classify speed lever (classification is
// decode-bound). Elements/Style/Theme/Mood remain for backward compatibility but
// are no longer populated (always empty); embedText/mergeTags degrade gracefully.
type Classification struct {
	Caption  string   `json:"caption"`
	Elements []string `json:"elements"`
	Style    string   `json:"style"`
	Theme    string   `json:"theme"`
	Mood     string   `json:"mood"`
	Tags     []string `json:"tags"`
	// Rotate is the clockwise rotation in degrees ("0", "90", "180" or "270") the
	// model judges the image needs to read upright. "0" for abstract/geometric
	// designs with no inherent orientation. The caller rotates the thumbnail (and
	// re-classifies the corrected image) when this is not "0".
	Rotate string `json:"rotate"`
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
		{ID: "fast", Label: "Rápido", Description: "Qwen2-VL 2B — rápido (~5-8s/imagem)", URL: VisionFast},
		{ID: "quality", Label: "Qualidade", Description: "Qwen2.5-VL 3B — mais detalhes, porém mais lento", URL: VisionQuality},
	}
}

// KnownVisionModel reports whether url is one of the built-in vision presets.
func KnownVisionModel(url string) bool {
	for _, p := range VisionPresets() {
		if p.URL == url {
			return true
		}
	}
	return false
}

// ResolveVisionModel returns url when it is a known preset, otherwise the default.
// It guards against a persisted choice that has since been removed (e.g. a model
// that turned out not to load), so the app always starts on a working model.
func ResolveVisionModel(url string) string {
	if KnownVisionModel(url) {
		return url
	}
	return DefaultVisionModel
}

// Engine lazily loads and holds the Kronk models. Methods load their model on
// first use; loads are guarded so concurrent callers wait rather than double-load.
type Engine struct {
	log         *slog.Logger
	libVersion  string
	embedModel  string
	visionModel string // desired vision model source (URL)
	concurrency int    // NSeqMax / worker-pool size for parallel inference
	classifyPx  int    // downscale edge (px) for the classification image
	captionLang string // caption/tags language code (e.g. "pt")

	mu           sync.Mutex
	sysReady     bool
	krnEmbed     *kronk.Kronk
	krnVision    *kronk.Kronk
	loadedVision string // URL currently loaded in krnVision (for hot-swap)

	embedMu sync.Mutex // serializes Embeddings (embed model is single-sequence)
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
func WithEmbedModel(s string) Option {
	return func(e *Engine) {
		if s != "" {
			e.embedModel = s
		}
	}
}

// WithVisionModel overrides the vision model source.
func WithVisionModel(s string) Option {
	return func(e *Engine) {
		if s != "" {
			e.visionModel = s
		}
	}
}

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

// WithClassifyPx overrides the classification image edge (px).
func WithClassifyPx(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.classifyPx = n
		}
	}
}

// WithCaptionLanguage sets the language code the caption/tags are written in.
func WithCaptionLanguage(code string) Option {
	return func(e *Engine) {
		if code != "" {
			e.captionLang = code
		}
	}
}

// CaptionLanguage reports the caption/tags language code.
func (e *Engine) CaptionLanguage() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.captionLang
}

// SetCaptionLanguage changes the caption/tags language. It only affects the
// prompt (no model reload), so it takes effect on the next classification.
func (e *Engine) SetCaptionLanguage(code string) {
	if code == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.captionLang = code
}

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

// classifyPxFromEnv picks the classification image edge, overridable via
// $STITCHVAULT_CLASSIFY_PX (clamped to a sane 64..1024 range).
func classifyPxFromEnv() int {
	if v := os.Getenv("STITCHVAULT_CLASSIFY_PX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 64 && n <= 1024 {
			return n
		}
	}
	return defaultClassifyPx
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
		classifyPx:  classifyPxFromEnv(),
		captionLang: defaultCaptionLang,
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

// loadKronk downloads a model and constructs a Kronk instance from it, building
// the load options via opts. It self-heals once: if construction fails (typically
// "validate-config: sha256 mismatch" from an interrupted earlier download that
// left an empty file in the cache), it purges zero-byte cache files and retries —
// Download then re-fetches only the missing pieces, keeping multi-GB models.
func (e *Engine) loadKronk(ctx context.Context, source string, opts func(models.Path) ([]model.Option, error)) (*kronk.Kronk, error) {
	mdls, err := models.New()
	if err != nil {
		return nil, fmt.Errorf("ml: models new: %w", err)
	}

	attempt := func() (*kronk.Kronk, error) {
		mp, err := mdls.Download(ctx, kronk.FmtLogger, source)
		if err != nil {
			return nil, fmt.Errorf("ml: download %q: %w", source, err)
		}
		mopts, err := opts(mp)
		if err != nil {
			return nil, err
		}
		krn, err := kronk.New(mopts...)
		if err != nil {
			return nil, fmt.Errorf("ml: new model %q: %w", source, err)
		}
		return krn, nil
	}

	krn, err := attempt()
	if err == nil {
		return krn, nil
	}
	if n := purgeEmptyCacheFiles(mdls.Path(), e.log); n > 0 {
		e.log.Warn("ml: load failed; cleared empty cache files and retrying", "removed", n, "source", source, "err", err)
		return attempt()
	}
	return nil, err
}

// purgeEmptyCacheFiles deletes zero-byte files under root (the Kronk model cache).
// An interrupted download can leave an empty .gguf or sha record that wedges the
// model with a sha-mismatch on every load; an empty cache file is never valid.
// Returns how many were removed.
func purgeEmptyCacheFiles(root string, log *slog.Logger) int {
	removed := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e != nil || info.Size() != 0 {
			return nil
		}
		if os.Remove(path) == nil {
			removed++
			log.Warn("ml: removed empty cache file", "path", path)
		}
		return nil
	})
	return removed
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
	krn, err := e.loadKronk(ctx, e.embedModel, func(mp models.Path) ([]model.Option, error) {
		// Always single-sequence: embeddinggemma's pooling graph asserts
		// (ggml_can_mul_mat) and aborts the process under NSeqMax>1. Embedding is
		// cheap, so the concurrency knob (vision worker pool) doesn't apply here;
		// Embed() serializes concurrent callers via embedMu.
		return []model.Option{
			model.WithModelFiles(mp.ModelFiles),
			model.WithAutoTune(true),
		}, nil
	})
	if err != nil {
		return err
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
	krn, err := e.loadKronk(ctx, e.visionModel, func(mp models.Path) ([]model.Option, error) {
		if mp.ProjFile == "" {
			return nil, fmt.Errorf("ml: vision model %q has no mmproj (not a multimodal model?)", e.visionModel)
		}
		opts := []model.Option{
			model.WithModelFiles(mp.ModelFiles),
			model.WithProjFile(mp.ProjFile),
			model.WithAutoTune(true),
			// Vision tuning (Kronk manual §3.11): process the image's token batch in
			// a single prefill pass. A low n_ubatch forces multiple passes per image
			// and "significantly slows inference"; match n_batch to it. Safe on Apple
			// Silicon's unified memory.
			model.WithNBatch(2048),
			model.WithNUBatch(2048),
		}
		// Classification needs little context (~image + short prompt + short JSON),
		// so a small window keeps the KV cache and graph reserve light. Parallel
		// slots (opt-in) need their own KV partitions and a bigger shared context.
		ctxWindow := 4096
		if e.concurrency > 1 {
			ctxWindow = 8 * 1024
			opts = append(opts,
				model.WithNSeqMax(e.concurrency),
				model.WithIncrementalCache(false),
			)
		}
		opts = append(opts, model.WithContextWindow(ctxWindow))
		return opts, nil
	})
	if err != nil {
		return err
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
	// The embed model has a single sequence slot; serialize concurrent callers
	// (the classify worker pool) so they don't contend on it.
	e.embedMu.Lock()
	resp, err := e.krnEmbed.Embeddings(ictx, model.D{"input": text, "truncate": true})
	e.embedMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("ml: embed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("ml: embed: empty response")
	}
	return resp.Data[0].Embedding, nil
}

const classifyPrompt = `Look at this machine-embroidery design and describe ONLY what you actually
see. Do not repeat or echo these instructions, and do NOT mention orientation, rotation, degrees, or
"machine embroidery" in the caption. Write the "caption" as one brief sentence of about 8-12 words
naming the subject, and give 3 to 8 short lowercase search "tags". For "rotate", output the clockwise
degrees to make the design read upright — "0", "90", "180" or "270" — using "0" unless a clearly
recognizable subject (a face, animal, or letter) appears sideways or upside down.`

// Classify sends a rendered PNG to the vision model and returns structured
// attributes, constrained to the schema via Kronk's json_schema grammar.
func (e *Engine) Classify(ctx context.Context, png []byte, hint string) (Classification, error) {
	if err := e.EnsureVision(ctx); err != nil {
		return Classification{}, err
	}

	// Downscaling the image is a primary speed lever (see defaultClassifyPx).
	if small, derr := downscalePNG(png, e.classifyPx); derr == nil {
		png = small
	}

	prompt := classifyPrompt
	if h := strings.TrimSpace(hint); h != "" {
		prompt += "\n\nContext about this file (use as a clue, but classify what you actually see): " + h
	}
	prompt += "\n\n" + captionLangInstruction(e.CaptionLanguage())

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
		// Bounded: the structured object is small (~120-200 tokens for all 7 fields),
		// and a tight cap limits any runaway the grammar still permits. repairJSON
		// handles a rare truncation; caption (first field) always survives.
		"max_tokens": 320,
	}

	ictx, cancel := withDefaultTimeout(ctx, 4*time.Minute)
	defer cancel()
	content, err := e.streamText(ictx, e.krnVision, d)
	if err != nil {
		return Classification{}, fmt.Errorf("ml: classify: %w", err)
	}

	var out Classification
	if err := unmarshalLoose(content, &out); err != nil {
		// Last resort: a small VLM occasionally breaks the JSON structure (single
		// quotes, an unterminated string) beyond what repairJSON can fix. Salvage at
		// least the caption so the design still gets a searchable description instead
		// of failing the whole classification.
		if cap := salvageCaption(content); cap != "" {
			e.log.Warn("ml: classify: malformed JSON, salvaged caption only", "caption", cap)
			return Classification{Caption: cap}, nil
		}
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
			// A cancelled/expired context surfaces as a model "error" event with a
			// "context canceled" message — return the real context error so callers
			// can distinguish a user stop from a genuine model failure.
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("model error: %s", content)
		}
		sb.WriteString(content)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
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

// captionSalvageRe loosely extracts the caption value from model output, tolerating
// an unterminated string (it stops at a quote, brace, or newline) and capping length.
var captionSalvageRe = regexp.MustCompile(`"caption"\s*:\s*"([^"}\n]{1,200})`)

// salvageCaption pulls a usable caption out of malformed JSON as a last resort.
func salvageCaption(s string) string {
	m := captionSalvageRe.FindStringSubmatch(s)
	if len(m) != 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// repairJSON makes a slightly-malformed model JSON document parseable: it escapes
// raw control characters inside string literals (small VLMs emit raw newlines/tabs,
// which are illegal in JSON strings → "invalid character '\n' in string literal")
// and appends any missing closing quote and brackets for a truncated document.
func repairJSON(s string) string {
	s = strings.TrimRight(s, " \t\r\n")

	var b strings.Builder
	b.Grow(len(s) + 8)
	var stack []byte
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
				b.WriteByte(ch)
			case ch == '\\':
				escaped = true
				b.WriteByte(ch)
			case ch == '"':
				inString = false
				b.WriteByte(ch)
			case ch == '\n':
				b.WriteString(`\n`)
			case ch == '\r':
				b.WriteString(`\r`)
			case ch == '\t':
				b.WriteString(`\t`)
			case ch < 0x20:
				// drop other unprintable control characters
			default:
				b.WriteByte(ch)
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
		b.WriteByte(ch)
	}
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
			"caption": str,
			"tags":    strArr,
			"rotate":  model.D{"type": "string", "enum": []string{"0", "90", "180", "270"}},
		},
		"required": []string{"caption", "tags", "rotate"},
	}
}
