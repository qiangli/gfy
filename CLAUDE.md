# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

gfy is a Go port of [graphify](https://github.com/safishamsi/graphify) (Python, PyPI package `graphifyy`). The original Python implementation is available locally under `reference/graphify/` (excluded from git via `.gitignore`) as an architectural reference.

The tool extracts structure from source code using tree-sitter AST parsing, builds a knowledge graph, detects communities, and generates reports. Pure Go with zero CGO — uses [gotreesitter](https://github.com/odvcencio/gotreesitter) for tree-sitter parsing.

## Build & Test Commands

```bash
make build          # go build ./cmd/gfy (injects version via ldflags)
make test           # go test ./cmd/... ./internal/...
make lint           # golangci-lint run
make tidy           # go mod tidy, fmt, vet
make install        # go install ./cmd/gfy
make clean          # remove binary and .gfy-out/
make diff           # build and run `gfy diff .`
make help           # list all targets
go test -run TestExtractGo ./internal/extract/       # single extract test
go test -run TestLanguageExtractors ./internal/extract/  # all language tests
go test -run TestFullPipeline ./internal/              # full e2e pipeline test
```

**Output:** Results go to `.gfy-out/` (report, graph.json, graph.html). Default export formats: `json,html`.

**Important:** Use `./cmd/... ./internal/...` instead of `./...` for tests — the `reference/` directory contains Python/C files that confuse the Go toolchain.

## Architecture

Pipeline: `detect → extract → build → cluster → analyze → report → export`

Each stage lives in its own `internal/<stage>/` package. `cmd/gfy/` is the CLI entry point (cobra).

### Extractor Architecture (the most complex subsystem)

The `internal/extract/` package uses three tiers of extractors, all registered in a dispatch map (`extract.go`) keyed by file extension:

1. **Custom extractors** — Hand-written AST walkers for languages with unique patterns: Go, Python, JS/TS, Rust, Zig, PowerShell, Elixir, Julia, Objective-C, Verilog. Each is an `Extract<Lang>(path) *types.ExtractionResult` function.
2. **Generic config-driven extractors** — 10 languages (Java, C, C++, Ruby, C#, Kotlin, Scala, PHP, Lua, Swift) share one `ExtractGeneric()` walker parameterized by a `LanguageConfig` struct (`langconfig.go`). The config declares AST node type names and optional hooks for language-specific behavior.
3. **Regex-based** — Dart and Blade templates use regex extraction when AST grammars don't fit well.

**Cross-file call resolution:** Individual extractors emit `RawCall` records for unresolved calls. The top-level `Extract()` function merges all per-file results, builds a global label index, and resolves raw calls into `calls` edges with `Confidence: INFERRED` (score 0.8). Member calls (e.g., `obj.method()`) are excluded from cross-file resolution.

### Compare Architecture

The `internal/compare/` package provides pairwise and N-way graph comparison with 11 metrics:

**Graph-level (4 metrics):** Node Jaccard, Edge Jaccard, Jensen-Shannon divergence on degree distributions, Community NMI via Louvain clustering.

**Tree-level (7 metrics):** All operate on the containment tree (file → class → function hierarchy) extracted from "contains"/"method" edges:

1. **AHU Subtree Match** (`tree_ahu.go`) — Canonical hashing (Aho-Hopcroft-Ullman). Supports semantic mode where hashes incorporate NodeType + behavioral Tags (rename-invariant).
2. **Tree Edit Distance** (`tree_ted.go`) — Zhang-Shasha with optional semantic match cost (NodeType + Tags awareness).
3. **Max Common Subtree** (`tree_mcs.go`) — Greedy matching via AHU hash groups.
4. **Subtree Frequency Vectors** (`tree_freq.go`) — Deckard-style depth-limited hash frequency cosine similarity.
5. **Collins-Duffy Tree Kernel** (`tree_kernel.go`) — Subset tree kernel with λ decay, hash-grouped optimization.
6. **Anti-Unification** (`tree_antiunify.go`) — Most specific generalization (shared structural template).
7. **Role Distribution** (`tree_role.go`) — Cosine similarity of `(NodeType, tagSet, arityBucket)` frequency vectors.

**Cross-project comparison:** `--normalize` enables Weisfeiler-Lehman structural alignment (`normalize.go`). `--sensitivity` (0-1) controls semantic AHU hashing, cross-project weight presets (`CrossProjectWeights()` zeros Jaccard, enables role distribution), and match threshold permissiveness.

**Composite scoring:** `tree_composite.go` — `DefaultWeights()` (tree 80%, graph 20%), `CrossProjectWeights()` (auto-selected with `--normalize`), `ComputeComposite()` weighted arithmetic mean.

**N-way comparison:** `compare.go` `CompareN()` — full O(n²) pairwise or `--estimate` mode using triangle inequality (`estimate.go`).

### Other Key Packages

- **`types/`** — Core types: `Node`, `Edge`, `RawCall`, `ExtractionResult`, `Confidence` (EXTRACTED/INFERRED/AMBIGUOUS)
- **`graph/`** — Custom adjacency-list graph (`map[string]map[string]map[string]any`) matching NetworkX dict-of-dicts semantics. Supports BFS, shortest path, subgraph extraction.
- **`build/`** — Transforms extraction results into graph with ID normalization and deduplication
- **`cluster/`** — Louvain community detection and cohesion scoring
- **`analyze/`** — God nodes (most-connected entities), surprising connections (cross-community edges), suggested questions
- **`report/`** — Generates `GRAPH_REPORT.md` with god nodes, surprising connections, community cohesion scores
- **`detect/`** — File classification (code, document, paper). Detects sensitive files (credentials, keys) and skips them. Respects `.gfyignore`.
- **`semantic/`** — LLM-powered extraction from non-code files (docs, papers) via Ollama. Per-file SHA256-based caching. Merges concepts/rationale into the AST graph.
- **`cache/`** — SHA256-based file hashing with streaming. JSON-encoded cache entries in `.cache/` subdirectories. Per-stage caching (extraction, semantic).
- **`trace/`** — Backward BFS call graph tracing from tagged nodes. Tags: throws, logs, fs, net, exec, async, unsafe, test, catches, otel.
- **`search/`** — Fuzzy matching (Levenshtein ≤2) with scoring: exact (+10), prefix (+5), contains (+2), degree-weighted tiebreaker.
- **`export/`** — JSON (NetworkX format), GraphML (Gephi/Cytoscape), Cypher (Neo4j), Obsidian (markdown with wikilinks)
- **`validate/`** — ExtractionResult schema validation (node/edge IDs, required fields, confidence levels)
- **`serve/`** — MCP stdio server (15 tools via modelcontextprotocol/go-sdk)
- **`watch/`** — File watching with fsnotify, auto-rebuild. Web UI uses vis-network (vis.js) for force-directed graph visualization, SSE for live reload. HTML is embedded as a constant string (no template files).
- **`source/`** — Source resolution: local directories, archives (.zip/.tar/.tgz), git URLs. Caches clones under `~/.gfy/`.

### CLI Subcommands

- **`build`** (default) — Run the full pipeline. Flags: `--no-cache`, `--no-semantic`, `--model`, `--ollama-url`, `--view`
- **`compare`** — Compare 2+ codebases or branches. Flags: `--normalize`, `--sensitivity`, `--branch`, `--skip-trees`, `--skip-communities`, `--estimate`, `--rename-threshold`
- **`diff`** — Compare local working tree against remote tracking branch. Auto-detects upstream via go-git. Flags: `--base`
- **`trace`** — Find behavioral call chains by tag (e.g., `gfy trace --tag throws`)
- **`view`** — Open `graph.html` in the default browser
- **`query`** — Fuzzy search the graph by keyword
- **`path`** — Shortest path between two nodes
- **`serve`** — Start MCP stdio server
- **`watch`** — File watch mode with live-reloading web UI at `http://localhost:<port>`

### Memory Management

Custom GC tuning in `cmd/gfy/main.go` init: `GOGC=50` (triggers GC sooner), `GOMEMLIMIT=2GiB` (caps heap) to address tree-sitter parser memory accumulation. Overridable via environment variables.

## Key Dependencies

- `github.com/odvcencio/gotreesitter` — Pure Go tree-sitter runtime (206 embedded grammars, zero CGO)
- `github.com/spf13/cobra` — CLI framework
- `github.com/modelcontextprotocol/go-sdk` — MCP server
- `github.com/fsnotify/fsnotify` — File watching
- `github.com/ledongthuc/pdf` — PDF text extraction (native Go)
- `github.com/xuri/excelize/v2` — XLSX reading (native Go)
- `github.com/nguyenthenguyen/docx` — DOCX reading (native Go)
- `github.com/go-git/go-git/v5` — Git operations (clone, pull, branch detection) for `diff` and `source` packages

## gotreesitter API Notes

The API requires passing `*Language` to node methods:
```go
lang := grammars.GoLanguage()
parser := ts.NewParser(lang)
tree, _ := parser.Parse(source)
root := tree.RootNode()
nodeType := root.Type(lang)                       // NOT root.Type()
child := root.ChildByFieldName("name", lang)      // NOT root.ChildByFieldName("name")
```

**File naming:** Do not name files `*_js.go` — Go treats `_js` as a GOOS build constraint. Use `javascript.go` instead.

## Adding a New Language Extractor

1. Create `internal/extract/extract_<lang>.go` with an `Extract<Lang>(path string) *types.ExtractionResult` function
2. Use the same pattern as `extract_go.go`: load grammar via `grammars.<Lang>Language()`, walk AST, emit nodes/edges/rawCalls
3. Register in the `dispatch` map in `internal/extract/extract.go`
4. Add test fixture in `testdata/` and test in `extract_test.go`

## Graph JSON Format

Output JSON uses NetworkX `node_link_data` format for cross-compatibility with the Python version:
```json
{"directed": false, "multigraph": false, "graph": {}, "nodes": [...], "links": [...]}
```

## Supported Languages (22)

Go, Python, JavaScript, TypeScript, Java, Rust, C, C++, Ruby, C#, Kotlin, Scala, PHP, Swift, Lua, Zig, PowerShell, Elixir, Julia, Objective-C, Dart, Verilog. Plus Vue/Svelte (via JS extractor) and Blade templates (regex).

## Design Decisions

- **Custom graph (not gonum)** — matches NetworkX dict-of-dicts semantics with string-keyed nodes
- **Louvain (not Leiden)** — Python version falls back to Louvain; simpler to port
- **Native Go for documents** — PDF, DOCX, XLSX extraction without Python/containers
- **Generic config-driven extractor** — 10 languages share one walker via LanguageConfig struct
- **Custom extractors** — Languages with unique AST patterns (Rust, Elixir, Julia, etc.) get dedicated extractors
- **Semantic AHU for cross-project** — Standard AHU hashes all leaves to 0; semantic mode incorporates NodeType + Tags for rename-invariant comparison
- **Sensitivity parameter** — Single knob controls weight presets, AHU mode, and match thresholds rather than requiring multiple flags
