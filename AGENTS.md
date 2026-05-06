# AGENTS.md

This file provides guidance to AI coding assistants working in this repository. `CLAUDE.md` is a symlink to this file.

## Essential Commands

```bash
make build    # Build gfy binary (injects version via ldflags)
make test     # Run tests — MUST scope to ./cmd/... ./pkg/... ./internal/...
make tidy     # mod tidy + fmt + vet
make lint     # golangci-lint
make diff     # Build and run `gfy diff .`
```

**CRITICAL**: Never run `go test ./...` — the `reference/` directory contains Python/C files that break the Go toolchain. Always scope tests to `./cmd/... ./pkg/... ./internal/...`.

### Single test / dev iteration

```bash
go test ./pkg/extract -run TestExtractGo                # one test in one package
go test ./pkg/extract -run TestExtractGo/subtest -v     # subtest, verbose
make build && ./gfy build ./testdata/some-fixture       # iterate on extractors
make build && ./gfy compare ./a ./b                     # iterate on compare metrics
```

## Architecture

Pipeline: `detect → extract → build → cluster → analyze → report → export`

### Entry Point

`cmd/gfy/main.go` — single-file cobra CLI with subcommands (`build`, `compare`, `diff`, `trace`, `query`, `path`, `view`, `serve`, `watch`). The `init()` sets GOGC=50 and GOMEMLIMIT=2GiB to manage tree-sitter parser memory pressure.

### Package Layout

- **`pkg/`** — Public API: `analyze`, `build`, `cache`, `cluster`, `detect`, `extract`, `graph`, `report`, `search`, `types`, `validate`
- **`internal/`** — Application-specific: `compare`, `export`, `semantic`, `serve`, `source`, `trace`, `watch`

### Extractor System (3-Tier)

Located in `pkg/extract/`:

1. **Custom extractors** — Hand-written AST walkers for Go, Python, JS/TS, Rust, Zig, PowerShell, Elixir, Julia, Objective-C, Verilog
2. **Generic config-driven** — 10 languages (Java, C, C++, Ruby, C#, Kotlin, Scala, PHP, Lua, Swift) share `ExtractGeneric()` via `LanguageConfig`
3. **Regex-based** — Dart and Blade templates

**Cross-file call resolution**: Extractors emit `RawCall` records; top-level `Extract()` merges results and resolves to `calls` edges with `Confidence: INFERRED`. Member calls (`obj.method()`) are intentionally excluded.

**Confidence levels** (used throughout `pkg/types`): `EXTRACTED` (direct from AST), `INFERRED` (cross-file resolution, ~0.8), `AMBIGUOUS` (multiple candidates).

**gotreesitter API**: node methods take `*Language` — `node.Type(lang)`, `node.ChildByFieldName("name", lang)`. Don't name files `*_js.go` (Go reads `_js` as a GOOS build constraint); use `javascript.go`.

### Document Processing (`pkg/detect/documents.go`)

PDF, DOCX, and XLSX files are converted to markdown and included in the knowledge graph as `document` nodes with `content` attributes. Used for research papers, API docs, and specifications alongside code.

### Compare System (`internal/compare/`)

Pairwise and N-way graph comparison with 11 metrics:
- **Graph-level (4)**: Node Jaccard, Edge Jaccard, degree JSD, Community NMI
- **Tree-level (7)**: AHU Subtree Match, Tree Edit Distance (Zhang-Shasha), Max Common Subtree, Subtree Frequency, Collins-Duffy Tree Kernel, Anti-Unification, Role Distribution

Cross-project mode: `--normalize` enables Weisfeiler-Lehman structural alignment; `--sensitivity` controls semantic matching aggressiveness.

### MCP Server (`internal/serve/`)

The `gfy serve` command implements a Model Context Protocol (MCP) stdio server exposing knowledge graph query tools to external AI assistants. Registered tools include:
- `search_nodes` — fuzzy label search with regex support
- `list_nodes` / `list_leaves` — enumerate nodes with tag filtering
- `get_callees` / `get_callers` — navigate call graph
- `trace_tag` — behavioral chain tracing (throws, logs, fs, net, exec, etc.)
- `suggest_questions` / `surprising_connections` / `god_nodes` — analysis queries

### Source Resolution (`internal/source/`)

Accepts local directories, git URLs (cloned to `~/.gfy/git/`), and archives (extracted to `~/.gfy/archive/`). Branch comparison uses `ResolveForBranch()` to check out specific refs into temporary directories.

## Build Patterns

- Version injection: `LDFLAGS := -ldflags "-X main.version=$(VERSION)"`
- Output directory: `.gfy-out/` (contains `graph.json`, `graph.html`, `GRAPH_REPORT.md`)
- HTML UI is embedded as a constant string in `internal/export/html.go` (no template files)
- SHA256-based per-file extraction cache in `.gfy-out/cache/` subdirectories
- Semantic extraction requires Ollama server (auto-detected; disable with `--no-semantic`)

## Key Constraints

- **NEVER** run `go test ./...` — breaks on `reference/` Python/C files
- **Pure Go, zero CGO** — tree-sitter goes through `gotreesitter` (206 embedded grammars). Do not introduce CGO or system-library dependencies; the single-binary, no-runtime-deps property is core to the project.
- The `reference/` directory contains the original Python graphify implementation — read-only reference, do not modify
- `.gfyignore` works like `.gitignore` for controlling which files gfy scans
