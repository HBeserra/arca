// Package pyreader is the MVP ingest.Reader: it parses embroidery files by
// shelling out to a host Python interpreter running the embedded reader.py
// (pyembroidery). It is deliberately isolated behind ingest.Reader so it can later
// be swapped for a native Go parser (or embedded CPython) without touching callers.
package pyreader

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"stitchvault/internal/embroidery"
	"stitchvault/internal/ingest"
)

// readerScript is run once-written to disk and invoked per file. Keep the command
// mapping inside it in sync with internal/embroidery/embroidery.go.
//
//go:embed reader.py
var readerScript []byte

// Reader implements ingest.Reader using a Python subprocess.
type Reader struct {
	mu         sync.RWMutex
	python     string // guarded: EnsurePyembroidery may switch it to a bootstrapped venv
	scriptPath string
}

var _ ingest.Reader = (*Reader)(nil)

func (r *Reader) pythonPath() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.python
}

func (r *Reader) setPython(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.python = p
}

// Option configures the Reader.
type Option func(*Reader)

// WithPython overrides the Python interpreter to use.
func WithPython(path string) Option {
	return func(r *Reader) {
		if path != "" {
			r.python = path
		}
	}
}

// New writes the embedded reader.py to disk once and resolves the Python
// interpreter. It does NOT verify pyembroidery, so the app always starts even
// when the dependency is missing; call Check to validate upfront, or let Read
// surface a per-file error. The interpreter is resolved (highest priority first)
// from: WithPython, $STITCHVAULT_PYTHON, ~/.stitchvault/venv/bin/python, then
// "python3".
func New(opts ...Option) (*Reader, error) {
	r := &Reader{python: defaultPython()}
	for _, o := range opts {
		o(r)
	}

	scriptPath, err := writeScript()
	if err != nil {
		return nil, fmt.Errorf("pyreader: write script: %w", err)
	}
	r.scriptPath = scriptPath

	return r, nil
}

// Check reports whether the resolved interpreter can import pyembroidery, with an
// actionable error if not. Callers (e.g. app startup) can use it to warn early;
// it is not required before Read.
func (r *Reader) Check(ctx context.Context) error {
	py := r.pythonPath()
	cmd := exec.CommandContext(ctx, py, "-c", "import pyembroidery")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pyreader: %q cannot import pyembroidery: %v: %s",
			py, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// EnsurePyembroidery makes the reader able to parse: if the current interpreter
// cannot import pyembroidery, it creates a dedicated virtualenv at
// ~/.stitchvault/venv and pip-installs pyembroidery (pure-Python, no build), then
// switches the reader to it. Network-dependent and may take a few seconds — call
// it once at startup (e.g. in a goroutine). Idempotent and self-healing.
func (r *Reader) EnsurePyembroidery(ctx context.Context) error {
	if r.Check(ctx) == nil {
		return nil
	}
	py, err := bootstrapVenv(ctx)
	if err != nil {
		return fmt.Errorf("pyreader: could not bootstrap pyembroidery: %w", err)
	}
	r.setPython(py)
	return r.Check(ctx)
}

// bootstrapVenv creates ~/.stitchvault/venv (if absent) and installs pyembroidery,
// returning the venv's python path.
func bootstrapVenv(ctx context.Context) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	venvDir := filepath.Join(home, ".stitchvault", "venv")
	py := venvPython(venvDir)

	if _, err := os.Stat(py); err != nil {
		base, err := exec.LookPath("python3")
		if err != nil {
			return "", fmt.Errorf("python3 not found on PATH: %w", err)
		}
		if out, err := exec.CommandContext(ctx, base, "-m", "venv", venvDir).CombinedOutput(); err != nil {
			return "", fmt.Errorf("create venv: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}

	if out, err := exec.CommandContext(ctx, py, "-m", "pip", "install", "--quiet", "pyembroidery").CombinedOutput(); err != nil {
		return "", fmt.Errorf("pip install pyembroidery: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return py, nil
}

// venvPython returns the interpreter path inside a venv for the current OS.
func venvPython(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}

// Read parses one file by invoking the sidecar. The context cancels the
// subprocess (exec.CommandContext kills it), which is how batch imports abort.
func (r *Reader) Read(ctx context.Context, path string) (*embroidery.Design, error) {
	cmd := exec.CommandContext(ctx, r.pythonPath(), r.scriptPath, path)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pyreader %s: %v: %s",
			filepath.Base(path), err, strings.TrimSpace(stderr.String()))
	}

	var wire wireDesign
	if err := json.Unmarshal(stdout.Bytes(), &wire); err != nil {
		return nil, fmt.Errorf("pyreader %s: decode output: %w", filepath.Base(path), err)
	}

	return wire.toDesign(), nil
}

// --- JSON wire format (matches reader.py output) ----------------------------

type wireDesign struct {
	Format   string            `json:"format"`
	Stitches [][3]float64      `json:"stitches"` // [x_mm, y_mm, cmd_int]
	Threads  []wireThread      `json:"threads"`
	Extras   map[string]string `json:"extras"`
}

type wireThread struct {
	R           uint8  `json:"r"`
	G           uint8  `json:"g"`
	B           uint8  `json:"b"`
	Description string `json:"description"`
	Brand       string `json:"brand"`
	Catalog     string `json:"catalog"`
}

func (w *wireDesign) toDesign() *embroidery.Design {
	d := &embroidery.Design{
		Format:   w.Format,
		Extras:   w.Extras,
		Stitches: make([]embroidery.Point, 0, len(w.Stitches)),
		Threads:  make([]embroidery.Thread, 0, len(w.Threads)),
	}

	for _, s := range w.Stitches {
		d.Stitches = append(d.Stitches, embroidery.Point{
			X:   s[0],
			Y:   s[1],
			Cmd: embroidery.Command(uint8(s[2])),
		})
	}

	for _, t := range w.Threads {
		d.Threads = append(d.Threads, embroidery.Thread{
			R: t.R, G: t.G, B: t.B,
			Description: t.Description,
			Brand:       t.Brand,
			Catalog:     t.Catalog,
		})
	}

	return d
}

// --- interpreter & script bootstrap -----------------------------------------

func defaultPython() string {
	if p := os.Getenv("STITCHVAULT_PYTHON"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		venv := venvPython(filepath.Join(home, ".stitchvault", "venv"))
		if _, err := os.Stat(venv); err == nil {
			return venv
		}
	}
	return "python3"
}

func scriptDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".stitchvault", "bin")
}

// writeScript writes the embedded reader.py once to a stable path and returns it.
// It is rewritten on every New so an upgraded binary refreshes the script.
func writeScript() (string, error) {
	dir := scriptDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "reader.py")
	if err := os.WriteFile(p, readerScript, 0o644); err != nil {
		return "", err
	}
	return p, nil
}
