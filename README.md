# gfy

A pure Go tool that builds knowledge graphs from codebases using tree-sitter AST parsing. **Single binary, zero dependencies.**

Extract structure from source code, build a knowledge graph, detect communities, compare codebases, trace behavioral call chains, search by meaning, and visualize it all — with one command. Ships with an MCP server and one-shot install for Claude Code and Cursor.

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
gfy build --format json,html,obsidian,cypher,graphml,mermaid ./my-project
gfy build --view .                         # build and open in browser
gfy build -o ./output .                    # custom output directory
gfy build --no-embeddings .                # skip embedding generation
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-f, --format` | Export formats: `json`, `html`, `obsidian`, `cypher`, `graphml`, `mermaid` (default `json,html`) |
| `-o, --out` | Output directory (default `<path>/.gfy-out/`) |
| `--view` | Open graph.html in browser after building |
| `--no-cache` | Ignore and clear cached extraction results |
| `--no-semantic` | Skip semantic extraction even if Ollama is available |
| `--no-embeddings` | Skip node embedding generation (semantic search falls back to fuzzy) |
| `--model` | LLM model for semantic extraction (auto-selects if empty) |
| `--embed-model` | Ollama embedding model (auto-selects `nomic-embed-text`, `mxbai-embed-large`, `all-minilm`, ... if empty) |
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

**MCP tools (16):**

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
| `semantic_search` | Embedding-based search by meaning (registered when `embeddings.bin` is present) |
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
gfy query .gfy-out/graph.json "servr"          # typo-tolerant
gfy query --semantic . "rate limiting"         # embedding-based, meaning-aware
```

`--semantic` ranks by cosine similarity against the `embeddings.bin` sidecar produced at build time. Requires Ollama to embed the query string. Falls back to fuzzy ranking automatically if the sidecar or Ollama is unavailable.

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

### install

Register gfy as a skill/plugin so Claude Code and Cursor auto-discover it on startup. Writes `SKILL.md` / `.cursor/rules/gfy.mdc` and prints the MCP server snippet for the user to paste into their own MCP config (we never edit `~/.claude.json` or `~/.cursor/mcp.json` in place — those are user-owned).

```bash
gfy install                                  # claude-code, user scope
gfy install --target cursor
gfy install --target all --scope project     # write into ./.claude/ and ./.cursor/
gfy install --dry-run                        # preview the writes
gfy install --uninstall --target all
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--target` | IDE: `claude-code` (default), `cursor`, `all` |
| `--scope` | Install scope: `user` (`~/.claude/`) or `project` (`./.claude/`) |
| `--project-dir` | Project directory for `--scope=project` (default: cwd) |
| `--dry-run` | Print actions without writing files |
| `--uninstall` | Remove previously installed skill/rules files |

## Source Resolution

gfy accepts multiple source types — not just local directories:

| Source | Example | Caching |
|--------|---------|---------|
| Local directory | `gfy build ./my-project` | Per-file extraction cache |
| Git URL | `gfy build https://github.com/user/repo` | Cloned to `~/.gfy/git/` |
| Archive | `gfy build ./project.zip` | Extracted to `~/.gfy/archive/` |

Supports `.zip`, `.tar`, `.tar.gz`, and `.tgz` archives. Git clones support SSH (agent-based) and HTTPS (credential helper) authentication.

## Semantic Extraction & Search

When [Ollama](https://ollama.ai) is running, gfy adds three semantic layers on top of the AST graph. All are best-effort — if Ollama isn't reachable or no suitable model is installed, the build still produces a useful AST-only graph and prints a hint for what to pull.

### 1. LLM concept extraction

Extract concepts, design rationale, and cross-references from non-code files (markdown, RST, PDFs, DOCX, XLSX, papers) via an LLM. Output nodes are merged into the knowledge graph alongside AST-extracted structure and cached per-file.

```bash
gfy build .                                # auto-detects Ollama
gfy build --model qwen3:8b .               # use specific chat model
gfy build --no-semantic .                  # skip LLM extraction
```

### 2. Document ingestion

PDF, DOCX, and XLSX files are converted to markdown via pure-Go converters and ingested as `document` nodes with a `content` attribute. The raw text becomes part of the graph so embedding search (below) covers design docs, ADRs, and spec sheets — not just code.

### 3. Embedding search

When an Ollama embedding model is available (`nomic-embed-text`, `mxbai-embed-large`, `all-minilm`, ...), `gfy build` embeds every node and writes a compact binary sidecar `embeddings.bin`. Search becomes meaning-aware:

```bash
ollama pull nomic-embed-text                       # ~270 MB, one-time
gfy build .                                        # auto-detects + writes embeddings.bin
gfy query --semantic . "rate limiting"             # cosine ranking
```

Same capability via MCP: the `semantic_search` tool is registered automatically when `embeddings.bin` is present. Falls back to fuzzy `search` otherwise — the tool surface stays consistent.

### Quick setup

```bash
ollama pull qwen3:8b                # chat model for concept extraction
ollama pull nomic-embed-text        # embedding model for semantic search
gfy build .
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
| `mermaid` | `callflow.md` | Mermaid flowchart of the call graph (renders inline in GitHub/Obsidian) |

Default: `json,html`. Embeddings (`embeddings.bin`) are written automatically alongside any format when an embedding model is available; pass `--no-embeddings` to skip.

## Output

```
.gfy-out/
├── GRAPH_REPORT.md    # god nodes, surprising connections, community structure
├── graph.json         # queryable graph (NetworkX-compatible)
├── graph.html         # interactive visualization (open in browser)
├── embeddings.bin     # binary sidecar (model + per-node vectors) for semantic search
├── callflow.md        # Mermaid flowchart of the call graph (optional, --format mermaid)
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
