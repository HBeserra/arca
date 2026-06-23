package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"stitchvault/internal/ingest"
	"stitchvault/internal/ml"
	"stitchvault/internal/render"
)

// Engine wires a Reader, a Renderer and a Store into the catalog use cases:
// importing a directory of embroidery files, serving the resulting designs, and
// (when a Classifier is wired) classifying them with the local vision model.
type Engine struct {
	log      *slog.Logger
	reader   ingest.Reader
	renderer render.Renderer
	store    Store

	thumbDir  string
	thumbSize int

	cls Classifier // optional; nil disables classification/semantic search
}

// Option configures the Engine.
type Option func(*Engine)

// WithThumbSize sets the thumbnail edge in pixels.
func WithThumbSize(px int) Option {
	return func(e *Engine) {
		if px > 0 {
			e.thumbSize = px
		}
	}
}

// WithClassifier wires the ML capability (vision + embeddings) for Phase 2.
func WithClassifier(c Classifier) Option {
	return func(e *Engine) { e.cls = c }
}

// New creates the catalog engine. thumbDir is where rendered PNGs are written and
// from where the asset middleware serves them.
func New(log *slog.Logger, reader ingest.Reader, renderer render.Renderer, store Store, thumbDir string, opts ...Option) *Engine {
	e := &Engine{
		log:       log,
		reader:    reader,
		renderer:  renderer,
		store:     store,
		thumbDir:  thumbDir,
		thumbSize: render.DefaultSize,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ThumbDir is the directory holding rendered thumbnails (served at /thumb/).
func (e *Engine) ThumbDir() string { return e.thumbDir }

// HasClassifier reports whether classification/semantic search are available.
func (e *Engine) HasClassifier() bool { return e.cls != nil }

// Scan expands the given roots (files and/or directories) into a de-duplicated
// list of absolute embroidery file paths.
func (e *Engine) Scan(roots []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}

	add := func(p string) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}

	for _, root := range roots {
		fi, err := os.Stat(root)
		if err != nil {
			e.log.Warn("scan: skipping unreadable root", "path", root, "err", err)
			continue
		}
		if fi.IsDir() {
			found, err := ingest.ScanDir(root)
			if err != nil {
				return nil, fmt.Errorf("scan %s: %w", root, err)
			}
			for _, f := range found {
				add(f)
			}
		} else if ingest.IsSupported(root) {
			add(root)
		}
	}
	return files, nil
}

// Prepare runs the expensive, parallelizable part of import for one file: parse →
// render a thumbnail → extract deterministic metadata. It does NOT touch the
// store, so many Prepare calls can run concurrently; persistence is serialized
// separately via Save (DuckDB writes are best kept single-writer). The thumbnail
// is named by the design's deterministic id.
func (e *Engine) Prepare(ctx context.Context, path string) (Design, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	des, err := e.reader.Read(ctx, abs)
	if err != nil {
		return Design{}, err
	}

	id := DesignID(abs)
	thumbPath := filepath.Join(e.thumbDir, id.String()+".png")
	if err := e.renderer.Thumbnail(des, thumbPath, e.thumbSize); err != nil {
		return Design{}, fmt.Errorf("render %s: %w", filepath.Base(abs), err)
	}

	format := des.Format
	if format == "" {
		format = strings.ToLower(filepath.Ext(abs))
	}

	var fileSize int64
	if fi, err := os.Stat(abs); err == nil {
		fileSize = fi.Size()
	}

	w, h := des.SizeMM()
	return Design{
		ID:            id,
		Path:          abs,
		FileName:      filepath.Base(abs),
		Format:        format,
		WidthMM:       w,
		HeightMM:      h,
		StitchCount:   des.StitchCount(),
		ColorChanges:  des.ColorChanges(),
		ColorCount:    des.ColorCount(),
		Palette:       des.Palette(),
		ThumbnailPath: thumbPath,
		FileSize:      fileSize,
		CreatedAt:     time.Now(),
	}, nil
}

// Save persists a prepared design. Call it from a single goroutine to keep DuckDB
// writes serialized.
func (e *Engine) Save(ctx context.Context, d Design) error {
	return e.store.InsertDesign(ctx, d)
}

// ImportFile prepares and saves one file in sequence. Convenient for single-file
// use and tests; batch imports use Prepare (parallel) + Save (serial) directly.
func (e *Engine) ImportFile(ctx context.Context, path string) (Design, error) {
	d, err := e.Prepare(ctx, path)
	if err != nil {
		return Design{}, err
	}
	if err := e.Save(ctx, d); err != nil {
		return Design{}, err
	}
	return d, nil
}

// List, Count, Get and Facets delegate to the store; they exist so callers depend
// on the engine, not the persistence layer.

func (e *Engine) List(ctx context.Context, f Filter) ([]Design, error) {
	return e.store.ListDesigns(ctx, f)
}

func (e *Engine) Count(ctx context.Context, f Filter) (int, error) {
	return e.store.CountDesigns(ctx, f)
}

func (e *Engine) Get(ctx context.Context, id uuid.UUID) (Design, error) {
	return e.store.GetDesign(ctx, id)
}

// Delete removes a design from the catalog and best-effort deletes its thumbnail.
func (e *Engine) Delete(ctx context.Context, id uuid.UUID) error {
	d, err := e.store.GetDesign(ctx, id)
	if err != nil {
		return err
	}
	if err := e.store.DeleteDesign(ctx, id); err != nil {
		return err
	}
	if d.ThumbnailPath != "" {
		_ = os.Remove(d.ThumbnailPath)
	}
	return nil
}

// DeleteMany removes many designs (rows + thumbnail files) in one batch. The
// thumbnail path is deterministic (<thumbDir>/<id>.png), so no per-id lookup.
func (e *Engine) DeleteMany(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	if err := e.store.DeleteDesigns(ctx, ids); err != nil {
		return err
	}
	for _, id := range ids {
		_ = os.Remove(filepath.Join(e.thumbDir, id.String()+".png"))
	}
	return nil
}

func (e *Engine) Facets(ctx context.Context) (Facets, error) {
	return e.store.Facets(ctx)
}

// Concurrency reports how many designs can be classified in parallel.
func (e *Engine) Concurrency() int {
	if e.cls == nil {
		return 1
	}
	if n := e.cls.Concurrency(); n > 0 {
		return n
	}
	return 1
}

// ClassifyResult is the outcome of classifying a design: semantic attributes plus
// the caption embedding, ready to persist.
type ClassifyResult struct {
	Caption   string
	Tags      []string
	Style     string
	Embedding []float32
}

// ClassifyData runs vision classification + caption embedding for a design WITHOUT
// touching the store, so many calls run in parallel (bounded by Concurrency).
func (e *Engine) ClassifyData(ctx context.Context, d Design) (ClassifyResult, error) {
	if e.cls == nil {
		return ClassifyResult{}, fmt.Errorf("catalog: classification unavailable (no ML engine)")
	}
	png, err := os.ReadFile(d.ThumbnailPath)
	if err != nil {
		return ClassifyResult{}, fmt.Errorf("catalog: read thumbnail %s: %w", d.FileName, err)
	}

	hint := classifyHint(d)
	c, err := e.cls.Classify(ctx, png, hint)
	if err != nil {
		return ClassifyResult{}, err
	}

	// The vision model recognizes faces/animals/letters and reports when the design
	// is rendered sideways or upside down. Rotate the stored thumbnail so the
	// preview reads upright, then re-classify the corrected image once for better
	// attributes (the first pass described a rotated subject). Done at most once, so
	// a wrong correction can't loop.
	if deg := rotateDegrees(c.Rotate); deg != 0 {
		if rotated, rerr := render.RotatePNG(png, deg); rerr != nil {
			e.log.Warn("catalog: rotate thumbnail failed", "file", d.FileName, "deg", deg, "err", rerr)
		} else if werr := os.WriteFile(d.ThumbnailPath, rotated, 0o644); werr != nil {
			e.log.Warn("catalog: persist rotated thumbnail failed", "file", d.FileName, "err", werr)
		} else {
			png = rotated
			if c2, err2 := e.cls.Classify(ctx, png, hint); err2 == nil {
				c = c2
			}
		}
	}

	tags := mergeTags(c.Tags, c.Elements)
	emb, err := e.cls.Embed(ctx, embedText(c))
	if err != nil {
		return ClassifyResult{}, fmt.Errorf("catalog: embed caption: %w", err)
	}
	return ClassifyResult{Caption: c.Caption, Tags: tags, Style: c.Style, Embedding: emb}, nil
}

// SaveClassification persists a ClassifyResult. Call from a single goroutine to
// keep DuckDB writes serialized.
func (e *Engine) SaveClassification(ctx context.Context, id uuid.UUID, r ClassifyResult) error {
	return e.store.UpdateClassification(ctx, id, r.Caption, r.Tags, r.Style, r.Embedding)
}

// Classify classifies one design and persists it, returning the updated design.
func (e *Engine) Classify(ctx context.Context, id uuid.UUID) (Design, error) {
	d, err := e.store.GetDesign(ctx, id)
	if err != nil {
		return Design{}, err
	}
	r, err := e.ClassifyData(ctx, d)
	if err != nil {
		return Design{}, err
	}
	if err := e.SaveClassification(ctx, id, r); err != nil {
		return Design{}, err
	}
	d.Caption, d.Tags, d.Style = r.Caption, r.Tags, r.Style
	return d, nil
}

// Search performs semantic search when a Classifier is available (embeds the
// query and ranks classified designs by cosine similarity); otherwise it falls
// back to the filename substring filter.
func (e *Engine) Search(ctx context.Context, query string, f Filter) ([]Design, error) {
	query = strings.TrimSpace(query)
	if e.cls == nil || query == "" {
		f.Search = query
		return e.store.ListDesigns(ctx, f)
	}
	// Semantic ranking replaces the filename filter.
	f.Search = ""
	vec, err := e.cls.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("catalog: embed query: %w", err)
	}
	return e.store.SearchSimilar(ctx, vec, f)
}

// embedText builds the text embedded for similarity search from a classification.
func embedText(c ml.Classification) string {
	parts := []string{c.Caption}
	if c.Theme != "" {
		parts = append(parts, c.Theme)
	}
	if c.Style != "" {
		parts = append(parts, c.Style)
	}
	// mergeTags drops run-on artifacts (e.g. a small VLM echoing the prompt into
	// elements) so they don't pollute the similarity-search embedding.
	parts = append(parts, mergeTags(c.Elements, c.Tags)...)
	return strings.Join(parts, ". ")
}

// mergeTags lowercases, de-duplicates and concatenates tag lists.
// maxTags bounds the tag list (a small VLM can loop the tags array).
const maxTags = 15

func mergeTags(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, s := range list {
			s = strings.ToLower(strings.TrimSpace(s))
			// Drop empties, dupes, run-on "tags" (repetition artifacts), and
			// medium tags ("embroidery", "design", …) that every design shares.
			if s == "" || seen[s] || isMetaTag(s) || len(s) > 40 || len(strings.Fields(s)) > 4 {
				continue
			}
			seen[s] = true
			out = append(out, s)
			if len(out) >= maxTags {
				return out
			}
		}
	}
	return out
}

// metaTagWords describe the embroidery medium rather than the subject. Since every
// catalogued image is an embroidery design, these add no search value.
var metaTagWords = map[string]bool{
	"design": true, "designs": true, "pattern": true, "patterns": true,
	"motif": true, "motifs": true, "stitch": true, "stitches": true, "stitching": true,
	"desenho": true, "padrão": true, "padrao": true, "diseño": true, "diseno": true,
	"image": true, "imagem": true, "picture": true,
}

// isMetaTag reports whether a (lowercased) tag is about the embroidery medium
// itself — any tag mentioning embroidery/bordado, or a bare design/pattern word.
func isMetaTag(s string) bool {
	return metaTagWords[s] || strings.Contains(s, "embroider") || strings.Contains(s, "bordad")
}

// ─── virtual folders (LLM-driven) ───────────────────────────────────────────

type folderSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GenerateFolders asks the LLM to propose a folder taxonomy from the catalog's
// classifications, then assigns every classified design to its nearest folder by
// caption-embedding cosine similarity. Replaces any existing folders. Returns the
// resulting folders with counts.
func (e *Engine) GenerateFolders(ctx context.Context, targetCount int) ([]VirtualFolder, error) {
	if e.cls == nil {
		return nil, fmt.Errorf("catalog: folder generation unavailable (no ML engine)")
	}
	if targetCount <= 0 {
		targetCount = 8
	}

	vectors, err := e.store.ListForClustering(ctx)
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("catalog: no classified designs to organize — classify first")
	}

	all, err := e.store.ListDesigns(ctx, Filter{Limit: 100000})
	if err != nil {
		return nil, err
	}

	lang := ""
	if e.cls != nil {
		lang = e.cls.CaptionLanguage()
	}
	out, err := e.cls.Complete(ctx, buildTaxonomyPrompt(all, targetCount, lang), folderSchema())
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Folders []folderSpec `json:"folders"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("catalog: parse taxonomy %q: %w", out, err)
	}

	type folder struct {
		id   uuid.UUID
		name string
		vec  []float32
	}
	var folders []folder
	for _, f := range parsed.Folders {
		name := shortFolderName(f.Name)
		if name == "" {
			continue
		}
		vec, err := e.cls.Embed(ctx, name+". "+f.Description)
		if err != nil {
			return nil, fmt.Errorf("catalog: embed folder %q: %w", name, err)
		}
		folders = append(folders, folder{id: uuid.New(), name: name, vec: vec})
	}
	if len(folders) == 0 {
		return nil, fmt.Errorf("catalog: the LLM proposed no usable folders")
	}

	if err := e.store.ClearVirtualFolders(ctx); err != nil {
		return nil, err
	}
	for _, f := range folders {
		if err := e.store.CreateVirtualFolder(ctx, f.id, f.name); err != nil {
			return nil, err
		}
	}

	for _, dv := range vectors {
		best := -2.0
		bestID := folders[0].id
		for _, f := range folders {
			if sim := cosine(dv.Embedding, f.vec); sim > best {
				best, bestID = sim, f.id
			}
		}
		if err := e.store.AssignFolder(ctx, dv.ID, bestID); err != nil {
			return nil, err
		}
	}

	return e.store.ListVirtualFolders(ctx)
}

// ListFolders returns the current virtual folders with counts.
func (e *Engine) ListFolders(ctx context.Context) ([]VirtualFolder, error) {
	return e.store.ListVirtualFolders(ctx)
}

func folderSchema() map[string]any {
	str := map[string]any{"type": "string"}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"folders": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": str, "description": str},
					"required":   []string{"name", "description"},
				},
			},
		},
		"required": []string{"folders"},
	}
}

// folderLangDirective returns an in-language instruction so folder names match the
// configured description language — small LLMs follow a native-language directive
// far better than an English "write in X".
func folderLangDirective(lang string) string {
	switch lang {
	case "en":
		return "Write all folder names and descriptions in English."
	case "es":
		return `IMPORTANTE: escribe TODOS los nombres y descripciones de las carpetas en ESPAÑOL.`
	default: // pt
		return `IMPORTANTE: escreva TODOS os nomes e descrições das pastas em PORTUGUÊS do Brasil.`
	}
}

func buildTaxonomyPrompt(designs []Design, targetCount int, lang string) string {
	tagFreq := map[string]int{}
	var captions []string
	for _, d := range designs {
		if d.Caption == "" {
			continue
		}
		for _, t := range d.Tags {
			tagFreq[t]++
		}
		if len(captions) < 40 {
			captions = append(captions, d.Caption)
		}
	}

	type tc struct {
		tag string
		n   int
	}
	tags := make([]tc, 0, len(tagFreq))
	for t, n := range tagFreq {
		tags = append(tags, tc{t, n})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].n > tags[j].n })
	top := make([]string, 0, 40)
	for i := 0; i < len(tags) && i < 40; i++ {
		top = append(top, tags[i].tag)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are organizing a library of %d machine-embroidery designs into about %d virtual folders by subject/theme.\n\n", len(designs), targetCount)
	if len(top) > 0 {
		b.WriteString("Most common tags across the library:\n")
		b.WriteString(strings.Join(top, ", "))
		b.WriteString("\n\n")
	}
	if len(captions) > 0 {
		b.WriteString("Example design captions:\n")
		for _, c := range captions {
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Propose about %d concise, mutually distinct folder categories that organize the whole library.\n", targetCount)
	b.WriteString(`Each folder name MUST be no more than 3 words naming the theme (1-2 is better) — do NOT include the words "Machine", "Embroidery", "Design", "Designs", "Pattern" or "Motif". Give each folder a one-sentence description.`)
	if d := folderLangDirective(lang); d != "" {
		b.WriteString("\n\n")
		b.WriteString(d)
	}
	return b.String()
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return -1
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return -1
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// ─── model management ───────────────────────────────────────────────────────

// VisionPresets returns the selectable vision models.
func (e *Engine) VisionPresets() []ml.Preset { return ml.VisionPresets() }

// CurrentVisionModel returns the active vision model URL.
func (e *Engine) CurrentVisionModel() string {
	if e.cls == nil {
		return ""
	}
	return e.cls.VisionModel()
}

// SetVisionModel switches the vision model and persists the choice. The new model
// loads on the next classification.
func (e *Engine) SetVisionModel(ctx context.Context, url string) error {
	if e.cls == nil {
		return fmt.Errorf("catalog: model selection unavailable (no ML engine)")
	}
	if err := e.store.SetSetting(ctx, "vision_model", url); err != nil {
		return err
	}
	e.cls.SetVisionModel(url)
	return nil
}

// CaptionLanguagePresets returns the selectable description languages.
func (e *Engine) CaptionLanguagePresets() []ml.CaptionLang { return ml.CaptionLanguagePresets() }

// CaptionLanguage returns the active caption/tags language code.
func (e *Engine) CaptionLanguage() string {
	if e.cls == nil {
		return ""
	}
	return e.cls.CaptionLanguage()
}

// SetCaptionLanguage switches the description language and persists the choice.
// It takes effect on the next classification (no model reload).
func (e *Engine) SetCaptionLanguage(ctx context.Context, code string) error {
	if e.cls == nil {
		return fmt.Errorf("catalog: language selection unavailable (no ML engine)")
	}
	if err := e.store.SetSetting(ctx, "caption_language", code); err != nil {
		return err
	}
	e.cls.SetCaptionLanguage(code)
	return nil
}

// ModelsLoaded reports whether ML models are currently in memory.
func (e *Engine) ModelsLoaded() bool {
	return e.cls != nil && e.cls.Loaded()
}

// EjectModels unloads the ML models to free memory; they reload on next use.
func (e *Engine) EjectModels(ctx context.Context) error {
	if e.cls == nil {
		return nil
	}
	return e.cls.Unload(ctx)
}

// cleanName turns a file name into a short subject hint: drops the extension and
// turns separators into spaces (e.g. "Lily_PrintStitch.pes" -> "Lily PrintStitch").
func cleanName(fileName string) string {
	name := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	name = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(name)
	return strings.Join(strings.Fields(name), " ")
}

// maxFolderNameWords is the hard cap on a virtual-folder name. Small LLMs routinely
// ignore the prompt's word limit, so the proposed name is clamped here — this is the
// guarantee that the sidebar can render every folder name without overflowing.
const maxFolderNameWords = 3

// shortFolderName collapses whitespace and keeps at most maxFolderNameWords words,
// with a rune-safe character cap as a final guard against a single very long word.
func shortFolderName(name string) string {
	fields := strings.Fields(name)
	if len(fields) > maxFolderNameWords {
		fields = fields[:maxFolderNameWords]
	}
	name = strings.Join(fields, " ")
	if r := []rune(name); len(r) > 28 {
		name = strings.TrimSpace(string(r[:28]))
	}
	return name
}

// classifyHint builds the textual context handed to the vision model: the cleaned
// file name (a strong subject clue) and the design's physical size, so the model
// knows a few-millimetre design is a tiny, simple motif rather than a busy scene.
func classifyHint(d Design) string {
	var parts []string
	if n := cleanName(d.FileName); n != "" {
		parts = append(parts, fmt.Sprintf("File name: %q.", n))
	}
	if d.WidthMM > 0 && d.HeightMM > 0 {
		size := fmt.Sprintf("Physical size: %.1f × %.1f mm.", d.WidthMM, d.HeightMM)
		if d.WidthMM < 20 && d.HeightMM < 20 {
			size += " This is a very small design — likely a single simple motif or just a few stitches."
		}
		parts = append(parts, size)
	}
	return strings.Join(parts, " ")
}

// rotateDegrees maps the model's reported orientation to a clockwise rotation,
// treating anything it can't parse (including "0") as no rotation.
func rotateDegrees(s string) int {
	switch strings.TrimSpace(s) {
	case "90":
		return 90
	case "180":
		return 180
	case "270":
		return 270
	default:
		return 0
	}
}
