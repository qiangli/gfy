# gfy

A pure Go tool that builds knowledge graphs from codebases using tree-sitter AST parsing. **Single binary, zero dependencies.**

Extract structure from source code, build a knowledge graph, detect communities, compare codebases, trace behavioral call chains, and visualize it all — with one command.

## Install

```bash
go install github.com/qiangli/gfy/cmd/gfy@latest
```

No Python, no Node.js, no system libraries, no runtime downloads. The binary includes all 206 language grammars and runs anywhere Go compiles to.

Or build from source:

```bash
git clone https://github.com/qiangli/gfy.git
cd gfy
make build
```

## Quick Start

```bash
# Build a knowledge graph from any codebase
gfy build .

# Output lands in .gfy-out/
ls .gfy-out/
#  GRAPH_REPORT.md   graph.json   graph.html
```

Open `graph.html` in a browser for an interactive visualization with search, community filtering, and click-to-inspect.

## Commands

### build

Scan a directory, extract AST structure from code files, build a knowledge graph, detect communities, and export results.

```bash
gfy build .
gfy build --format json,html,obsidian,cypher,graphml ./my-project
gfy build --view .                         # build and open in browser
gfy build -o ./output .                    # custom output directory
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-f, --format` | Export formats: `json`, `html`, `obsidian`, `cypher`, `graphml` (default `json,html`) |
| `-o, --out` | Output directory (default `<path>/.gfy-out/`) |
| `--view` | Open graph.html in browser after building |
| `--no-cache` | Ignore and clear cached extraction results |
| `--no-semantic` | Skip semantic extraction even if Ollama is available |
| `--model` | LLM model for semantic extraction (auto-selects if empty) |
| `--ollama-url` | Ollama server URL (default `http://localhost:11434`) |

### compare

Compare two or more knowledge graphs and generate a similarity report. Works across branches, versions, or entirely different codebases.

```bash
# Compare directories or pre-built graph.json files
gfy compare ./project-v1 ./project-v2

# Cross-project comparison (different codebases implementing similar functionality)
gfy compare --normalize ./repo-a ./repo-b

# Tune how aggressively to look through renames/refactors
gfy compare --normalize --sensitivity 0.8 ./repo-a ./repo-b

# Compare branches within one repo
gfy compare --branch main --branch feature-x .

# N-way comparison
gfy compare ./v1 ./v2 ./v3

# Fast mode: skip expensive computations
gfy compare --skip-trees --skip-communities ./v1 ./v2
```

**11 comparison metrics:**

| Metric | Category | What it measures |
|--------|----------|-----------------|
| Node Jaccard | Graph | Node set overlap |
| Edge Jaccard | Graph | Edge set overlap |
| Degree Similarity | Graph | Connectivity pattern similarity (1 - JSD) |
| Community NMI | Graph | Community structure alignment |
| AHU Subtree Match | Tree | Fraction of isomorphic subtrees |
| Tree Edit Distance | Tree | Minimum structural edits (highest weight) |
| Max Common Subtree | Tree | Largest shared structural fragment |
| Subtree Frequency | Tree | Local pattern distribution (Deckard-style) |
| Tree Kernel | Tree | Partial structural similarity (Collins-Duffy) |
| Anti-Unification | Tree | Shared top-down structural template |
| Role Distribution | Tree | Behavioral profile similarity (NodeType + tags) |

**Flags:**

| Flag | Description |
|------|-------------|
| `--normalize` | Align nodes by structural fingerprint (Weisfeiler-Lehman) instead of ID |
| `--sensitivity` | 0-1, how aggressively to look through renames/refactors |
| `--branch` | Branches to compare (requires exactly one source) |
| `--skip-trees` | Skip tree comparison algorithms (faster) |
| `--skip-communities` | Skip community comparison (faster) |
| `--estimate` | N-way: compute N-1 full comparisons, estimate rest via triangle inequality |
| `--rename-threshold` | Min similarity for rename detection (default 0.6) |
| `-f, --format` | Output format: `markdown`, `json` (default `markdown`) |

**Report includes:** composite similarity score, node/edge diffs, rename candidates, impact analysis (transitive dependents), drift analysis (import changes), and community evolution (splits, merges, stable).

### diff

Compare your local working tree's knowledge graph against the remote tracking branch. Auto-detects the upstream branch via go-git (no system `git` required).

```bash
gfy diff                              # auto-detect tracking branch
gfy diff --base main                  # compare against origin/main
gfy diff ./myrepo                     # run on a different repo
```

### trace

Find all call chains leading to functions with a specific behavioral tag.

```bash
gfy trace . --tag throws              # what triggers panics/exceptions?
gfy trace . --tag net --depth 5       # what reaches network calls?
gfy trace . --tag fs                  # what touches the filesystem?
```

**Tags:** `throws`, `catches`, `logs`, `fs`, `net`, `exec`, `async`, `unsafe`, `test`

### watch

Monitor a directory for code changes and automatically rebuild the graph. Serves a live-reloading web UI with real-time updates via SSE.

```bash
gfy watch .
# Live graph: http://localhost:<port>
```

The UI provides interactive vis.js visualization with search, community filtering, click-to-inspect node details, and live status indicators.

### serve

Start an [MCP](https://modelcontextprotocol.io/) stdio server exposing the graph for AI assistants.

```bash
gfy serve .gfy-out/graph.json
```

**MCP tools (15):**

| Tool | Description |
|------|-------------|
| `graph_stats` | Overview: node/edge counts, communities, confidence, relations, behavioral tags |
| `get_node` | Look up a node with all attributes (tags, comments, source location, hierarchy) |
| `get_neighbors` | Direct neighbors with edge metadata and optional relation filter |
| `get_children` | Containment children (functions in a file, methods in a class) |
| `shortest_path` | Find shortest path between two nodes |
| `list_roots` | Top-level file/module nodes (top of containment hierarchy) |
| `list_leaves` | Leaf functions/methods with optional tag filter |
| `list_nodes` | Filter nodes by source file, behavioral tag, or file type |
| `search` | Fuzzy keyword search with ranked results and scores |
| `get_subgraph` | Extract focused subgraph around nodes with BFS expansion |
| `god_nodes` | Most connected entities (excluding file-level hubs) |
| `surprising_connections` | Cross-file/cross-community anomalous edges |
| `suggest_questions` | Investigation questions from graph anomalies |
| `community_info` | List communities with cohesion scores, or inspect one |
| `trace_calls` | Call chains leading to behavioral tags (throws, fs, net, etc.) |

### query

Search the graph by keyword with fuzzy matching, diacritics normalization, and degree-weighted ranking.

```bash
gfy query .gfy-out/graph.json "authentication"
gfy query .gfy-out/graph.json "servr"   # typo-tolerant
```

### path

Find the shortest path between two nodes.

```bash
gfy path .gfy-out/graph.json "Server" "Database"
```

### view

Open `graph.html` in the default browser. Auto-builds if the graph doesn't exist.

```bash
gfy view .
```

## Source Resolution

gfy accepts multiple source types — not just local directories:

| Source | Example | Caching |
|--------|---------|---------|
| Local directory | `gfy build ./my-project` | Per-file extraction cache |
| Git URL | `gfy build https://github.com/user/repo` | Cloned to `~/.gfy/git/` |
| Archive | `gfy build ./project.zip` | Extracted to `~/.gfy/archive/` |

Supports `.zip`, `.tar`, `.tar.gz`, and `.tgz` archives. Git clones support SSH (agent-based) and HTTPS (credential helper) authentication.

## Semantic Extraction

When [Ollama](https://ollama.ai) is running, gfy can extract concepts and relationships from non-code files (docs, papers, markdown) using LLM analysis. Results are cached per-file and merged into the knowledge graph alongside AST-extracted structure.

```bash
gfy build .                                # auto-detects Ollama
gfy build --model llama3.2 .               # use specific model
gfy build --no-semantic .                   # skip LLM extraction
```

## Supported Languages (22)

Go, Python, JavaScript, TypeScript, Java, Rust, C, C++, Ruby, C#, Kotlin, Scala, PHP, Swift, Lua, Zig, PowerShell, Elixir, Julia, Objective-C, Dart, Verilog.

Plus Vue, Svelte (via JS extractor) and Blade templates (regex).

## How It Works

```
detect → extract → build → cluster → analyze → report → export
```

1. **Detect** — Walk the filesystem, classify files (code, document, paper, image, video), respect `.gitignore` and `.gfyignore`, skip sensitive files
2. **Extract** — Parse code with tree-sitter (via [gotreesitter](https://github.com/odvcencio/gotreesitter)) to extract classes, functions, imports, call graphs, and behavioral tags
3. **Build** — Assemble extracted nodes and edges into a graph with ID normalization and deduplication
4. **Cluster** — Detect communities using the Louvain algorithm
5. **Analyze** — Identify god nodes, surprising connections, and generate suggested investigation questions
6. **Report** — Generate `GRAPH_REPORT.md` with findings
7. **Export** — Write graph in requested formats

Every relationship is tagged with confidence: `EXTRACTED` (from AST), `INFERRED` (cross-file resolution), or `AMBIGUOUS`.

## Export Formats

| Format | File | Description |
|--------|------|-------------|
| `json` | `graph.json` | NetworkX-compatible node-link JSON |
| `html` | `graph.html` | Interactive vis.js visualization |
| `obsidian` | `obsidian/` | Obsidian vault with wikilinks |
| `cypher` | `graph.cypher` | Neo4j Cypher import statements |
| `graphml` | `graph.graphml` | GraphML XML for Gephi/Cytoscape |

Default: `json,html`

## Output

```
.gfy-out/
├── GRAPH_REPORT.md    # god nodes, surprising connections, community structure
├── graph.json         # queryable graph (NetworkX-compatible)
├── graph.html         # interactive visualization (open in browser)
├── graph.cypher       # Neo4j import (optional)
├── graph.graphml      # Gephi/Cytoscape (optional)
├── obsidian/          # Obsidian vault (optional)
└── cache/
    ├── extract/       # AST extraction cache (per-file, SHA256-keyed)
    └── semantic/      # LLM extraction cache (per-file, SHA256-keyed)
```

## Credits

gfy is based on **[graphify](https://github.com/safishamsi/graphify)** by [Safi Shamsi](https://github.com/safishamsi), which provides the foundational pipeline design, extraction patterns, and HTML visualization template.

- **Original project:** [github.com/safishamsi/graphify](https://github.com/safishamsi/graphify)
- **PyPI package:** [graphifyy](https://pypi.org/project/graphifyy/)
- **Tree-sitter runtime:** [gotreesitter](https://github.com/odvcencio/gotreesitter) by Oscar Dov Vcencio — pure Go, zero CGO, 206 embedded grammars
- **MCP SDK:** [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk)

## License

MIT
