# Arca

A local-first, RAG-powered knowledge base and chat desktop application. Index your documents, ask questions, and get context-grounded answers — all running on your machine with no cloud dependency.

Built with **Yzma**, **Wails v3**, **React**, and **DuckDB VSS** for vector search. 
Models run via [Kronk](https://github.com/ardanlabs/kronk) (llama.cpp under the hood) and are downloaded automatically on first launch.

---

## Features

- **Sessions** — organize your knowledge into isolated workspaces
- **Document indexing** — add files, folders, or drives; supports text, markdown, code, JSON, YAML, CSV, HTML, and more
- **Semantic search** — HNSW vector index (cosine similarity) powered by DuckDB VSS
- **RAG-augmented chat** — streaming responses grounded in your documents with citations
- **Optional reranking** — improve retrieval quality with a cross-encoder reranker
- **Fully local** — models and data never leave your machine

---

## Models

| Role | Model | Size |
|---|---|---|
| Embedding | `embeddinggemma-300m-qat-Q8_0` | ~600 MB |
| Chat | `Qwen3-0.6B-Q8_0` | ~400 MB |
| Reranker (optional) | `bge-reranker-v2-m3-Q8_0` | ~400 MB |

All models are downloaded automatically to your local model cache on first run.

---

## Prerequisites

- **Go 1.24+**
- **Node.js 18+ with npm**
- **Wails v3 CLI**
- **C compiler** (gcc or clang — required for llama.cpp)
- ~2 GB free RAM for model inference

Install the Wails CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

---

## Getting Started

```sh
# Clone the repo
git clone <repo-url>
cd arca

# Install frontend dependencies
cd frontend && npm install && cd ..

# Run in development mode (hot reload)
task dev

# Or build a production binary
task build
```

### Available tasks

| Command | Description |
|---|---|
| `task dev` | Dev mode with hot reload |
| `task build` | Production binary |
| `task run` | Run the built binary |

You can also drive Wails directly:

```sh
wails3 dev -config ./build/config.yml
wails3 build -config ./build/config.yml
```

---

## Environment Variables

| Variable | Effect |
|---|---|
| `ARCA_DB_BUILD_INFO` | Suffix appended to the DB filename — useful for isolated test databases (e.g. `ARCA_DB_BUILD_INFO=test` → `~/.arca/arca-test.db`) |

The database lives at `~/.arca/arca.db` by default.

---

## Architecture

```
┌────────────────────────────────────────────────────────────────┐
│  Desktop Window (Wails v3 webview)                             │
│  ┌─────────────┐  ┌─────────────────────┐  ┌───────────────┐   │
│  │ Knowledge   │  │       Chat          │  │    Config     │   │
│  │ Base Panel  │  │       Panel         │  │    Panel      │   │
│  │             │  │                     │  │               │   │
│  │ Sessions    │  │ Messages + Citations│  │ Model / RAG   │   │
│  │ Documents   │  │ Streaming tokens    │  │ settings      │   │
│  │ Progress    │  │                     │  │               │   │
│  └─────────────┘  └─────────────────────┘  └───────────────┘   │
└───────────────────────────▲────────────────────────────────────┘
                            │ Wails RPC + Events
┌───────────────────────────▼────────────────────────────────────┐
│  Go Backend                                                    │
│  IndexService    QueryService                                  │
│       └──────────────┴───────── enginebus.Engine               │
│                                       │                        │
│                          ┌────────────┼────────────┐           │
│                     Embed model  Chat model   Reranker         │
│                     (Kronk / llama.cpp)                        │
│                                       │                        │
│                                  DuckDB + VSS                  │
│                             sessions / documents / chunks      │
└────────────────────────────────────────────────────────────────┘
```

### Project layout

```
.
├── main.go                   # Wails app setup, event registration, model URLs
├── services/
│   ├── indexservice.go       # Session & document management API
│   └── queryservice.go       # RAG chat API (streaming)
├── internal/business/
│   └── enginebus/
│       ├── engine.go         # Model loading, embedding, search, chat
│       └── stores/indexdb/   # DuckDB persistence + HNSW vector index
└── frontend/
    └── src/
        ├── App.tsx
        └── components/
            ├── ChatPanel.tsx
            ├── KnowledgeBasePanel.tsx
            └── ConfigPanel.tsx
```

---

## Tech Stack

| Layer | Technology |
|---|---|
| Desktop framework | [Wails v3](https://v3.wailsapp.com/) |
| Frontend | React 18, TypeScript, Vite 5 |
| Styling | Tailwind CSS v4, shadcn/ui, Radix UI |
| Backend | Go 1.24 |
| Vector DB | [DuckDB](https://duckdb.org/) with [VSS extension](https://duckdb.org/docs/extensions/vss) (HNSW) |
| LLM inference | [Kronk SDK](https://github.com/ardanlabs/kronk) (llama.cpp) |
| Embedding dims | 768 (embeddinggemma-300m) |

---

## Supported File Types

`.txt` `.md` `.mdx` `.go` `.py` `.js` `.ts` `.jsx` `.tsx` `.json` `.yaml` `.yml` `.toml` `.csv` `.xml` `.html` `.sh` `.sql` `.rs` `.java` `.c` `.cpp` `.h`
