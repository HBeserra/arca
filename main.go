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

	_ "github.com/marcboeker/go-duckdb/v2"

	"stitchvault/internal/business/catalog"
	"stitchvault/internal/business/catalog/stores/catalogdb"
	"stitchvault/internal/ingest/pyreader"
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
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Catalog and RAG share the one DuckDB file; the slice only touches catalogdb.
	db, err := sql.Open("duckdb", dbPath())
	if err != nil {
		log.Fatalf("open duckdb: %v", err)
	}

	store, err := catalogdb.New(logger, db)
	if err != nil {
		log.Fatalf("catalogdb init: %v", err)
	}

	// pyreader never fails for a missing dependency — it warns and surfaces a
	// clear per-file error at import time, so the app always starts.
	reader, err := pyreader.New()
	if err != nil {
		log.Fatalf("pyreader init: %v", err)
	}
	if err := reader.Check(context.Background()); err != nil {
		logger.Warn("pyembroidery not available — imports will fail until it is installed", "err", err)
	}

	thumbs := thumbsDir()
	mlEng := ml.New(logger)
	eng := catalog.New(logger, reader, render.New(), store, thumbs, catalog.WithClassifier(mlEng))
	catSvc := services.NewCatalogService(eng, appIcon)

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
