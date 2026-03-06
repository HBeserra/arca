package main

import (
	"context"
	"embed"
	_ "embed"
	"log"
	"time"

	"changeme/internal/engine"
	"changeme/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	embedModelURL  = "https://huggingface.co/ggml-org/embeddinggemma-300m-qat-q8_0-GGUF/resolve/main/embeddinggemma-300m-qat-Q8_0.gguf"
	rerankModelURL = "https://huggingface.co/gpustack/bge-reranker-v2-m3-GGUF/resolve/main/bge-reranker-v2-m3-Q8_0.gguf"
	chatModelURL   = "https://huggingface.co/unsloth/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf"
)

func init() {
	application.RegisterEvent[services.IndexProgressEvent]("index:progress")
	application.RegisterEvent[services.IndexCompleteEvent]("index:complete")
	application.RegisterEvent[services.IndexErrorEvent]("index:error")
	application.RegisterEvent[services.ChatTokenEvent]("chat:token")
	application.RegisterEvent[services.ChatCitationEvent]("chat:citation")
	application.RegisterEvent[services.ChatDoneEvent]("chat:done")
}

func main() {
	eng := engine.New(embedModelURL, rerankModelURL, chatModelURL)

	// Initialise Kronk libs and catalog in the background (downloads on first run).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		if err := eng.Init(ctx); err != nil {
			log.Printf("engine init: %v", err)
		}
	}()

	idxSvc := services.NewIndexService(eng)
	qrySvc := services.NewQueryService(eng, idxSvc)

	// Load persisted sessions.
	if err := idxSvc.LoadAll(); err != nil {
		log.Printf("session load: %v", err)
	}

	app := application.New(application.Options{
		Name:        "Arca",
		Description: "RAG-powered knowledge base and chat",
		Services: []application.Service{
			application.NewService(idxSvc),
			application.NewService(qrySvc),
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
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
