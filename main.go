package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"stitchvault/internal/business/catalog"
	"stitchvault/internal/business/catalog/stores/catalogdb"
	"stitchvault/internal/ingest/goreader"
	"stitchvault/internal/ml"
	"stitchvault/internal/render"
	"stitchvault/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func init() {
	application.RegisterEvent[services.ImportProgressEvent]("import:progress")
	application.RegisterEvent[services.ImportCompleteEvent]("import:complete")
	application.RegisterEvent[services.ImportErrorEvent]("import:error")
	application.RegisterEvent[services.ClassifyProgressEvent]("classify:progress")
	application.RegisterEvent[services.ClassifyCompleteEvent]("classify:complete")
	application.RegisterEvent[services.ClassifyErrorEvent]("classify:error")
	application.RegisterEvent[services.FoldersCompleteEvent]("folders:complete")
	application.RegisterEvent[services.FoldersErrorEvent]("folders:error")
	application.RegisterEvent[services.ExportProgressEvent]("export:progress")
	application.RegisterEvent[services.ExportCompleteEvent]("export:complete")
	application.RegisterEvent[services.ExportErrorEvent]("export:error")
	application.RegisterEvent[services.RestoreProgressEvent]("restore:progress")
	application.RegisterEvent[services.RestoreCompleteEvent]("restore:complete")
	application.RegisterEvent[services.RestoreErrorEvent]("restore:error")
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Catalog and RAG share the one DuckDB file; the slice only touches catalogdb.
	store, db, err := openCatalogStore(logger, dbPath())
	if err != nil {
		log.Fatalf("catalogdb init: %v", err)
	}
	defer db.Close()

	// Native Go embroidery reader — no Python, no external process, no bootstrap.
	// Parses the common machine formats directly (see internal/ingest/goreader).
	reader := goreader.New()

	thumbs := thumbsDir()
	var mlOpts []ml.Option
	{
		ctx := context.Background()
		stored, _ := store.GetSetting(ctx, "vision_model")
		resolved := ml.ResolveVisionModel(stored)
		if stored != "" && stored != resolved {
			// A previously-selected model is no longer available (e.g. removed
			// because it would not load) — migrate the setting to the default.
			logger.Warn("vision model setting unavailable; using default", "stored", stored, "using", resolved)
			_ = store.SetSetting(ctx, "vision_model", resolved)
		}
		mlOpts = append(mlOpts, ml.WithVisionModel(resolved))

		if lang, _ := store.GetSetting(ctx, "caption_language"); lang != "" {
			mlOpts = append(mlOpts, ml.WithCaptionLanguage(lang))
		}

		if mode, _ := store.GetSetting(ctx, "ai_worker_mode"); mode != "" {
			switch mode {
			case catalog.WorkerModeEco:
				mlOpts = append(mlOpts, ml.WithConcurrency(1))
			case catalog.WorkerModeTurbo:
				mlOpts = append(mlOpts, ml.WithConcurrency(3))
			case catalog.WorkerModeAuto:
				mlOpts = append(mlOpts, ml.WithConcurrency(ml.AutoWorkers()))
			}
		}
	}
	mlEng := ml.New(logger, mlOpts...)
	eng := catalog.New(logger, reader, render.New(), store, thumbs, catalog.WithClassifier(mlEng))
	catSvc := services.NewCatalogService(eng, appIcon)

	// Consolidate any catalogued originals still living outside ~/.stitchvault
	// (e.g. on a USB drive) into it, so the catalogue is self-contained. Runs once
	// in the background; idempotent and non-destructive (sources are only copied,
	// never deleted; unreachable sources are skipped).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if copied, skipped, err := eng.ConsolidateOriginals(ctx); err != nil {
			logger.Warn("consolidate originals", "err", err)
		} else if copied > 0 || skipped > 0 {
			logger.Info("consolidated originals into ~/.stitchvault/originals", "copied", copied, "skipped", skipped)
		}
	}()

	app := application.New(application.Options{
		Name:        "StitchVault",
		Description: "Catálogo de bordados com busca inteligente",
		Services: []application.Service{
			application.NewService(catSvc),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: thumbMiddleware(thumbs),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "StitchVault",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropNormal,
			TitleBar:                application.MacTitleBarDefault,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func openCatalogStore(logger *slog.Logger, path string) (*catalogdb.Store, *sql.DB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, nil, err
	}

	store, err := catalogdb.New(logger, db)
	if err == nil {
		return store, db, nil
	}
	db.Close()

	if !isDuckDBWALReplayError(err) {
		return nil, nil, err
	}

	walPath := path + ".wal"
	if _, statErr := os.Stat(walPath); statErr != nil {
		return nil, nil, err
	}
	recoveryPath := fmt.Sprintf("%s.recovery-%s", walPath, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if renameErr := os.Rename(walPath, recoveryPath); renameErr != nil {
		return nil, nil, fmt.Errorf("%w (preserve WAL %q: %v)", err, recoveryPath, renameErr)
	}

	logger.Warn("DuckDB WAL replay failed; preserved WAL and retrying database", "wal", recoveryPath, "err", err)
	db, openErr := sql.Open("duckdb", path)
	if openErr != nil {
		return nil, nil, openErr
	}
	store, retryErr := catalogdb.New(logger, db)
	if retryErr != nil {
		db.Close()
		return nil, nil, fmt.Errorf("after WAL recovery: %w", retryErr)
	}
	return store, db, nil
}

func isDuckDBWALReplayError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "Failure while replaying WAL") ||
		strings.Contains(message, "GetDefaultDatabase with no default database set")
}

// thumbMiddleware serves rendered thumbnails from dir at /thumb/<id>.png and falls
// through to the normal asset server for every other path. http.Dir rejects ".."
// traversal, constraining reads to the thumbnails directory.
func thumbMiddleware(dir string) application.Middleware {
	fileServer := http.FileServer(http.Dir(dir))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/thumb/") {
				req := r.Clone(r.Context())
				req.URL.Path = strings.TrimPrefix(r.URL.Path, "/thumb")
				fileServer.ServeHTTP(w, req)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// dbPath returns ~/.stitchvault/stitchvault.db, creating the directory if needed.
func dbPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	dir := filepath.Join(home, ".stitchvault")
	_ = os.MkdirAll(dir, 0o700)

	// environment base name for the database file, useful for testing
	buildInfo := os.Getenv("STITCHVAULT_DB_BUILD_INFO")

	filename := fmt.Sprintf("stitchvault-%s.db", buildInfo)

	return filepath.Join(dir, filename)
}

// thumbsDir returns ~/.stitchvault/thumbnails, creating it if needed.
func thumbsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".stitchvault", "thumbnails")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
