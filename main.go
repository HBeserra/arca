package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"changeme/internal/business/enginebus"
	"changeme/internal/business/enginebus/monitor"
	"changeme/internal/business/enginebus/stores/indexdb"
	"changeme/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

const (
	embedModelURL  = "ggml-org/embeddinggemma-300m-qat-q8_0-GGUF/embeddinggemma-300m-qat-Q8_0.gguf"
	rerankModelURL = "gpustack/bge-reranker-v2-m3-GGUF/bge-reranker-v2-m3-Q8_0.gguf"
	chatModelURL   = "unsloth/Qwen3-0.6B-GGUF/Qwen3-0.6B-Q8_0.gguf"
)

func init() {
	application.RegisterEvent[services.IndexProgressEvent]("index:progress")
	application.RegisterEvent[services.IndexCompleteEvent]("index:complete")
	application.RegisterEvent[services.IndexErrorEvent]("index:error")
	application.RegisterEvent[services.ChatTokenEvent]("chat:token")
	application.RegisterEvent[services.ChatCitationEvent]("chat:citation")
	application.RegisterEvent[services.ChatDoneEvent]("chat:done")
	application.RegisterEvent[services.ChatErrorEvent]("chat:error")
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db, err := sql.Open("duckdb", dbPath())
	if err != nil {
		log.Fatalf("open duckdb: %v", err)
	}

	store, err := indexdb.New(logger, db,
		indexdb.WithDimensions(768), // embeddinggemma-300m → 768-dim vectors (300M = params, not dims)
	)
	if err != nil {
		log.Fatalf("indexdb init: %v", err)
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	_ = appCancel // cancelled when app exits

	mon := monitor.New(appCtx, logger, 2*time.Second)

	eng, err := enginebus.New(logger, store,
		enginebus.WithEmbedModel(embedModelURL),
		enginebus.WithRerankModel(rerankModelURL),
		enginebus.WithChatModel(chatModelURL),
		enginebus.WithStatusProvider(mon),
	)
	if err != nil {
		log.Fatalf("engine init: %v", err)
	}

	// Download libs and models in the background.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		if err := eng.Load(ctx); err != nil {
			log.Printf("engine load: %v", err)
		}
	}()

	idxSvc := services.NewIndexService(eng, appIcon)
	qrySvc := services.NewQueryService(eng)
	monSvc := services.NewMonitorService(eng)

	app := application.New(application.Options{
		Name:        "Arca",
		Description: "RAG-powered knowledge base and chat",
		Services: []application.Service{
			application.NewService(idxSvc),
			application.NewService(qrySvc),
			application.NewService(monSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Arca",
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

// dbPath returns ~/.arca/arca.db, creating the directory if needed.
func dbPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	dir := filepath.Join(home, ".arca")
	_ = os.MkdirAll(dir, 0o700)

	// environment base name for the database file, useful for testing
	buildInfo := os.Getenv("ARCA_DB_BUILD_INFO")

	vals := os.Environ()
	for _, v := range vals {
		fmt.Println(v)
	}

	filename := fmt.Sprintf("arca-%s.db", buildInfo)

	return filepath.Join(dir, filename)
}
