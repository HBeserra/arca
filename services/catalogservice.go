package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"stitchvault/internal/business/catalog"
)

// ─── Events ─────────────────────────────────────────────────────────────────

// ImportProgressEvent is emitted as each file finishes importing (or fails).
type ImportProgressEvent struct {
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	FileName string `json:"fileName"`
	DesignID string `json:"designID"`
}

// ImportCompleteEvent is emitted once the whole batch has been processed.
type ImportCompleteEvent struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
	Failed   int `json:"failed"`
}

// ImportErrorEvent is emitted when one file fails (the batch continues).
type ImportErrorEvent struct {
	FileName string `json:"fileName"`
	Error    string `json:"error"`
}

// ClassifyProgressEvent is emitted as each design is classified.
type ClassifyProgressEvent struct {
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	FileName string `json:"fileName"`
	DesignID string `json:"designID"`
}

// ClassifyCompleteEvent is emitted when the classification batch finishes.
type ClassifyCompleteEvent struct {
	Total      int `json:"total"`
	Classified int `json:"classified"`
	Failed     int `json:"failed"`
}

// ClassifyErrorEvent is emitted when one design fails to classify.
type ClassifyErrorEvent struct {
	FileName string `json:"fileName"`
	Error    string `json:"error"`
}

// ClassifyStatusInfo reports classification coverage of the catalog.
type ClassifyStatusInfo struct {
	Available  bool `json:"available"`
	Total      int  `json:"total"`
	Classified int  `json:"classified"`
	Loaded     bool `json:"loaded"` // models currently in memory (eject available)
}

// VisionModelOption is one selectable model in the UI picker.
type VisionModelOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// VisionModelInfo is the picker state: the options and which is active.
type VisionModelInfo struct {
	CurrentID string              `json:"currentID"`
	Models    []VisionModelOption `json:"models"`
}

// ─── JSON views ─────────────────────────────────────────────────────────────

// DesignInfo is the JSON-serialisable view of a catalogued design.
type DesignInfo struct {
	ID           string      `json:"id"`
	FileName     string      `json:"fileName"`
	Path         string      `json:"path"`
	Format       string      `json:"format"`
	WidthMM      float64     `json:"widthMM"`
	HeightMM     float64     `json:"heightMM"`
	StitchCount  int         `json:"stitchCount"`
	ColorChanges int         `json:"colorChanges"`
	ColorCount   int         `json:"colorCount"`
	Palette      []ColorInfo `json:"palette"`
	ThumbnailURL string      `json:"thumbnailURL"`
	FileSize     int64       `json:"fileSize"`
	CreatedAt    string      `json:"createdAt"`

	// Phase 2 (empty until classification runs).
	Caption         string   `json:"caption"`
	Style           string   `json:"style"`
	Tags            []string `json:"tags"`
	VirtualFolderID string   `json:"virtualFolderID"`
}

// ColorInfo is one palette swatch.
type ColorInfo struct {
	R           uint8  `json:"r"`
	G           uint8  `json:"g"`
	B           uint8  `json:"b"`
	Hex         string `json:"hex"`
	Description string `json:"description"`
}

// FacetInfo drives the filter sidebar.
type FacetInfo struct {
	Total       int              `json:"total"`
	Formats     []FacetCountInfo `json:"formats"`
	MaxSizeMM   float64          `json:"maxSizeMM"`
	MaxStitches int              `json:"maxStitches"`
	MaxColors   int              `json:"maxColors"`
}

// FacetCountInfo is one facet value and its count.
type FacetCountInfo struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// ListFilter is the JSON-serialisable filter the gallery sends.
type ListFilter struct {
	Formats         []string `json:"formats"`
	MinSizeMM       float64  `json:"minSizeMM"`
	MaxSizeMM       float64  `json:"maxSizeMM"`
	MinStitches     int      `json:"minStitches"`
	MaxStitches     int      `json:"maxStitches"`
	MinColors       int      `json:"minColors"`
	MaxColors       int      `json:"maxColors"`
	Search          string   `json:"search"`
	VirtualFolderID string   `json:"virtualFolderID"`
	DuplicatesOnly  bool     `json:"duplicatesOnly"`
	Limit           int      `json:"limit"`
	Offset          int      `json:"offset"`
}

// FolderInfo is the JSON view of an LLM-generated virtual folder.
type FolderInfo struct {
	ID       string `json:"id"`
	ParentID string `json:"parentID"`
	Name     string `json:"name"`
	Count    int    `json:"count"`
}

// FoldersCompleteEvent is emitted when folder generation finishes.
type FoldersCompleteEvent struct {
	Count int `json:"count"`
}

// FoldersErrorEvent is emitted when folder generation fails.
type FoldersErrorEvent struct {
	Error string `json:"error"`
}

// ─── Service ────────────────────────────────────────────────────────────────

// CatalogService is the Wails-facing service for importing and browsing designs.
type CatalogService struct {
	eng     *catalog.Engine
	appIcon []byte

	mu             sync.Mutex
	classifyCancel context.CancelFunc // non-nil while a batch classify runs
	foldersBusy    bool               // true while folder generation runs
}

// NewCatalogService returns a new CatalogService.
func NewCatalogService(eng *catalog.Engine, appIcon []byte) *CatalogService {
	return &CatalogService{eng: eng, appIcon: appIcon}
}

// PickFolder opens a native folder dialog and returns the selected path.
func (s *CatalogService) PickFolder() (string, error) {
	return application.Get().Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetTitle("Select a folder of embroidery files").
		PromptForSingleSelection()
}

// PickFiles opens a native multi-file dialog and returns the selected paths.
func (s *CatalogService) PickFiles() ([]string, error) {
	return application.Get().Dialog.OpenFile().
		SetTitle("Select embroidery files").
		PromptForMultipleSelection()
}

type prepResult struct {
	path   string
	design catalog.Design
	err    error
}

// ImportPaths scans the given files/folders and imports every embroidery file.
// Parsing + rendering run on a worker pool; persistence and event emission happen
// on a single collector goroutine (keeping DuckDB writes serialized and progress
// monotonic). One bad file emits import:error and does not abort the batch.
func (s *CatalogService) ImportPaths(paths []string) error {
	files, err := s.eng.Scan(paths)
	if err != nil {
		return fmt.Errorf("catalog: scan: %w", err)
	}

	total := len(files)
	application.Get().Event.Emit("import:progress", ImportProgressEvent{Done: 0, Total: total})
	if total == 0 {
		application.Get().Event.Emit("import:complete", ImportCompleteEvent{})
		return nil
	}

	go s.runImport(files)
	return nil
}

func (s *CatalogService) runImport(files []string) {
	ctx := context.Background()

	workers := max(runtime.NumCPU(), 1)

	jobs := make(chan string)
	prepared := make(chan prepResult)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				d, err := s.eng.Prepare(ctx, path)
				prepared <- prepResult{path: path, design: d, err: err}
			}
		}()
	}

	go func() {
		for _, f := range files {
			jobs <- f
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(prepared)
	}()

	total := len(files)
	var done, imported, failed int
	for r := range prepared {
		done++
		name := filepath.Base(r.path)

		if r.err == nil {
			r.err = s.eng.Save(ctx, r.design)
		}
		if r.err != nil {
			failed++
			application.Get().Event.Emit("import:error", ImportErrorEvent{FileName: name, Error: r.err.Error()})
			application.Get().Event.Emit("import:progress", ImportProgressEvent{Done: done, Total: total, FileName: name})
			continue
		}

		imported++
		application.Get().Event.Emit("import:progress", ImportProgressEvent{
			Done: done, Total: total, FileName: r.design.FileName, DesignID: r.design.ID.String(),
		})
	}

	application.Get().Event.Emit("import:complete", ImportCompleteEvent{
		Total: total, Imported: imported, Failed: failed,
	})
	_ = beeep.Notify("StitchVault — import complete",
		fmt.Sprintf("Imported %d of %d designs.", imported, total), s.appIcon)
}

// ListDesigns returns designs matching the filter. When a search term is present
// and the vision model is wired, results are ranked by semantic similarity;
// otherwise the search term is a filename substring filter.
func (s *CatalogService) ListDesigns(filter ListFilter) ([]DesignInfo, error) {
	designs, err := s.eng.Search(context.Background(), filter.Search, toFilter(filter))
	if err != nil {
		return nil, fmt.Errorf("catalog: list: %w", err)
	}
	out := make([]DesignInfo, 0, len(designs))
	for _, d := range designs {
		out = append(out, designToInfo(d))
	}
	return out, nil
}

// CountDesigns returns how many designs match the filter.
func (s *CatalogService) CountDesigns(filter ListFilter) (int, error) {
	return s.eng.Count(context.Background(), toFilter(filter))
}

// GetDesign returns a single design by id.
func (s *CatalogService) GetDesign(id string) (*DesignInfo, error) {
	did, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("catalog: invalid design id: %w", err)
	}
	d, err := s.eng.Get(context.Background(), did)
	if err != nil {
		return nil, fmt.Errorf("catalog: get %s: %w", id, err)
	}
	info := designToInfo(d)
	return &info, nil
}

// Facets returns the catalog-wide facets for the filter sidebar.
func (s *CatalogService) Facets() (*FacetInfo, error) {
	f, err := s.eng.Facets(context.Background())
	if err != nil {
		return nil, fmt.Errorf("catalog: facets: %w", err)
	}
	info := FacetInfo{
		Total:       f.Total,
		MaxSizeMM:   f.MaxSizeMM,
		MaxStitches: f.MaxStitches,
		MaxColors:   f.MaxColors,
	}
	for _, fc := range f.Formats {
		info.Formats = append(info.Formats, FacetCountInfo{Value: fc.Value, Count: fc.Count})
	}
	return &info, nil
}

// ClassifyStatus reports whether the vision model is available and how many
// designs have been classified.
func (s *CatalogService) ClassifyStatus() (*ClassifyStatusInfo, error) {
	info := &ClassifyStatusInfo{Available: s.eng.HasClassifier(), Loaded: s.eng.ModelsLoaded()}
	all, err := s.eng.List(context.Background(), catalog.Filter{Limit: 1_000_000})
	if err != nil {
		return nil, fmt.Errorf("catalog: classify status: %w", err)
	}
	info.Total = len(all)
	for _, d := range all {
		if d.Caption != "" {
			info.Classified++
		}
	}
	return info, nil
}

// ClassifyDesign classifies a single design on demand and returns it updated.
func (s *CatalogService) ClassifyDesign(id string) (*DesignInfo, error) {
	did, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("catalog: invalid design id: %w", err)
	}
	d, err := s.eng.Classify(context.Background(), did)
	if err != nil {
		return nil, fmt.Errorf("catalog: classify: %w", err)
	}
	info := designToInfo(d)
	return &info, nil
}

// ClassifyAll classifies, in the background, every design that has no caption yet.
// Vision inference is GPU-bound and run one-at-a-time; progress is emitted per
// design via classify:* events.
func (s *CatalogService) ClassifyAll() error {
	if !s.eng.HasClassifier() {
		return fmt.Errorf("catalog: classification unavailable (no vision model)")
	}

	all, err := s.eng.List(context.Background(), catalog.Filter{Limit: 1_000_000})
	if err != nil {
		return fmt.Errorf("catalog: classify all: %w", err)
	}

	var todo []catalog.Design
	for _, d := range all {
		if d.Caption == "" {
			todo = append(todo, d)
		}
	}

	total := len(todo)
	application.Get().Event.Emit("classify:progress", ClassifyProgressEvent{Done: 0, Total: total})
	if total == 0 {
		application.Get().Event.Emit("classify:complete", ClassifyCompleteEvent{})
		return nil
	}

	s.mu.Lock()
	if s.classifyCancel != nil {
		s.mu.Unlock()
		return fmt.Errorf("catalog: classification already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.classifyCancel = cancel
	s.mu.Unlock()

	go s.runClassify(ctx, todo)
	return nil
}

// StopClassify cancels a running batch classification (no-op if none is running).
func (s *CatalogService) StopClassify() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.classifyCancel != nil {
		s.classifyCancel()
	}
	return nil
}

type classifyResult struct {
	design catalog.Design
	data   catalog.ClassifyResult
	err    error
}

func (s *CatalogService) runClassify(ctx context.Context, todo []catalog.Design) {
	defer func() {
		s.mu.Lock()
		s.classifyCancel = nil
		s.mu.Unlock()
	}()

	total := len(todo)

	workers := s.eng.Concurrency()
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan catalog.Design)
	results := make(chan classifyResult)

	// Workers run the GPU-bound vision + embed in parallel (the model is loaded
	// once with NSeqMax slots); the single collector below serializes DB writes.
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range jobs {
				data, err := s.eng.ClassifyData(ctx, d)
				results <- classifyResult{design: d, data: data, err: err}
			}
		}()
	}
	// Feeder stops enqueuing as soon as the batch is cancelled (StopClassify).
	go func() {
		defer close(jobs)
		for _, d := range todo {
			select {
			case jobs <- d:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var done, classified, failed int
	for r := range results {
		// StopClassify cancels ctx; in-flight work then aborts with a cancellation
		// error. That's user-initiated, not a failure — skip it silently (no event,
		// no counts) so the user doesn't see spurious "context canceled" errors.
		if classifyCanceled(ctx, r.err) {
			continue
		}
		done++
		if r.err == nil {
			r.err = s.eng.SaveClassification(ctx, r.design.ID, r.data)
		}
		if classifyCanceled(ctx, r.err) {
			continue
		}
		if r.err != nil {
			failed++
			application.Get().Event.Emit("classify:error", ClassifyErrorEvent{FileName: r.design.FileName, Error: r.err.Error()})
		} else {
			classified++
		}
		application.Get().Event.Emit("classify:progress", ClassifyProgressEvent{
			Done: done, Total: total, FileName: r.design.FileName, DesignID: r.design.ID.String(),
		})
	}

	application.Get().Event.Emit("classify:complete", ClassifyCompleteEvent{
		Total: total, Classified: classified, Failed: failed,
	})
	_ = beeep.Notify("StitchVault — classificação concluída",
		fmt.Sprintf("Classificados %d de %d designs.", classified, total), s.appIcon)
}

// classifyCanceled reports whether err is a user-cancellation artifact (StopClassify
// cancelled the batch ctx) rather than a real classification failure. The batch ctx
// has no deadline, so ctx.Err() is non-nil only when the user stopped the run.
func classifyCanceled(ctx context.Context, err error) bool {
	return err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled))
}

// ListFolders returns the current LLM-generated virtual folders with counts.
func (s *CatalogService) ListFolders() ([]FolderInfo, error) {
	folders, err := s.eng.ListFolders(context.Background())
	if err != nil {
		return nil, fmt.Errorf("catalog: list folders: %w", err)
	}
	out := make([]FolderInfo, 0, len(folders))
	for _, f := range folders {
		parent := ""
		if f.ParentID != nil {
			parent = f.ParentID.String()
		}
		out = append(out, FolderInfo{ID: f.ID.String(), ParentID: parent, Name: f.Name, Count: f.Count})
	}
	return out, nil
}

// GenerateFolders asks the LLM (in the background) to propose a folder taxonomy
// and assigns every classified design to its nearest folder. Emits folders:* events.
func (s *CatalogService) GenerateFolders(targetCount int) error {
	if !s.eng.HasClassifier() {
		return fmt.Errorf("catalog: folder generation unavailable (no vision model)")
	}
	s.mu.Lock()
	s.foldersBusy = true
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			s.foldersBusy = false
			s.mu.Unlock()
		}()
		folders, err := s.eng.GenerateFolders(context.Background(), targetCount)
		if err != nil {
			application.Get().Event.Emit("folders:error", FoldersErrorEvent{Error: err.Error()})
			return
		}
		application.Get().Event.Emit("folders:complete", FoldersCompleteEvent{Count: len(folders)})
		_ = beeep.Notify("StitchVault — pastas virtuais",
			fmt.Sprintf("%d pastas geradas pela IA.", len(folders)), s.appIcon)
	}()
	return nil
}

// EjectModels unloads the ML models to free memory. Refuses while classification
// or folder generation is running (would unload mid-inference).
func (s *CatalogService) EjectModels() error {
	s.mu.Lock()
	busy := s.classifyCancel != nil || s.foldersBusy
	s.mu.Unlock()
	if busy {
		return fmt.Errorf("pare a classificação antes de ejetar o modelo")
	}
	return s.eng.EjectModels(context.Background())
}

// VisionModels returns the model picker options and the active one.
func (s *CatalogService) VisionModels() (*VisionModelInfo, error) {
	current := s.eng.CurrentVisionModel()
	info := &VisionModelInfo{}
	for _, p := range s.eng.VisionPresets() {
		info.Models = append(info.Models, VisionModelOption{ID: p.ID, Label: p.Label, Description: p.Description, URL: p.URL})
		if p.URL == current {
			info.CurrentID = p.ID
		}
	}
	return info, nil
}

// SetVisionModel selects a model by preset id (loads on next classification).
func (s *CatalogService) SetVisionModel(id string) error {
	for _, p := range s.eng.VisionPresets() {
		if p.ID == id {
			return s.eng.SetVisionModel(context.Background(), p.URL)
		}
	}
	return fmt.Errorf("modelo desconhecido: %q", id)
}

// ─── helpers ────────────────────────────────────────────────────────────────

func toFilter(f ListFilter) catalog.Filter {
	return catalog.Filter{
		Formats:         f.Formats,
		MinSizeMM:       f.MinSizeMM,
		MaxSizeMM:       f.MaxSizeMM,
		MinStitches:     f.MinStitches,
		MaxStitches:     f.MaxStitches,
		MinColors:       f.MinColors,
		MaxColors:       f.MaxColors,
		Search:          f.Search,
		VirtualFolderID: f.VirtualFolderID,
		DuplicatesOnly:  f.DuplicatesOnly,
		Limit:           f.Limit,
		Offset:          f.Offset,
	}
}

func designToInfo(d catalog.Design) DesignInfo {
	palette := make([]ColorInfo, 0, len(d.Palette))
	for _, t := range d.Palette {
		palette = append(palette, ColorInfo{
			R: t.R, G: t.G, B: t.B,
			Hex:         fmt.Sprintf("#%02X%02X%02X", t.R, t.G, t.B),
			Description: t.Description,
		})
	}

	vfid := ""
	if d.VirtualFolderID != nil {
		vfid = d.VirtualFolderID.String()
	}

	tags := d.Tags
	if tags == nil {
		tags = []string{}
	}

	// Cache-bust the thumbnail by file mtime so a re-rendered (zoom cap) or
	// orientation-corrected image refreshes in the browser — the URL path is
	// otherwise stable per design id, and the browser would serve a stale PNG.
	thumbURL := "/thumb/" + d.ID.String() + ".png"
	if fi, err := os.Stat(d.ThumbnailPath); err == nil {
		thumbURL += "?v=" + strconv.FormatInt(fi.ModTime().Unix(), 10)
	}

	return DesignInfo{
		ID:              d.ID.String(),
		FileName:        d.FileName,
		Path:            d.Path,
		Format:          d.Format,
		WidthMM:         d.WidthMM,
		HeightMM:        d.HeightMM,
		StitchCount:     d.StitchCount,
		ColorChanges:    d.ColorChanges,
		ColorCount:      d.ColorCount,
		Palette:         palette,
		ThumbnailURL:    thumbURL,
		FileSize:        d.FileSize,
		CreatedAt:       d.CreatedAt.Format(time.RFC3339),
		Caption:         d.Caption,
		Style:           d.Style,
		Tags:            tags,
		VirtualFolderID: vfid,
	}
}
