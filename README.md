# gfy

A pure Go tool that builds knowledge graphs from codebases using tree-sitter AST extraction. **Single binary, zero dependencies.**

**Go port of [graphify](https://github.com/safishamsi/graphify)** by [Safi Shamsi](https://github.com/safishamsi) — an AI coding assistant skill that reads your files, builds a knowledge graph, and gives you back structure you didn't know was there.

## Install

```bash
go install github.com/qiangli/gfy/cmd/gfy@latest
```

That's it. No Python, no Node.js, no system libraries, no runtime downloads. The binary includes all 206 language grammars and runs anywhere Go compiles to.

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

# Output lands in gfy-out/
ls gfy-out/
#  GRAPH_REPORT.md   graph.json   graph.html
```

Open `graph.html` in a browser for an interactive visualization with search, community filtering, and click-to-inspect.

## Commands

```bash
gfy build [path]                           # build knowledge graph
gfy build --format json,html,obsidian .    # choose export formats
gfy compare [path1] [path2]                # compare two codebases
gfy compare --normalize --sensitivity 0.7 [path1] [path2]  # cross-project similarity
gfy diff                                   # compare local vs remote tracking branch
gfy diff --base main                       # compare local vs origin/main
gfy query [path] "search terms"            # search the graph (fuzzy)
gfy path  [path] [source] [target]         # find shortest path
gfy trace [path] --tag throws              # trace behavioral call chains
gfy serve [path]                           # start MCP server
gfy watch [path]                           # watch and rebuild with live UI
gfy view  [path]                           # open graph.html in browser
```

### build

Scans a directory, extracts AST structure from code files, builds a knowledge graph, detects communities, and exports results.

```bash
gfy build .
gfy build --format json,html,obsidian,cypher,graphml ./my-project
gfy build --view .                         # build and open in browser
```

**Export formats:**

| Format | File | Description |
|--------|------|-------------|
| `json` | `graph.json` | NetworkX-compatible node-link JSON |
| `html` | `graph.html` | Interactive vis.js visualization |
| `obsidian` | `obsidian/` | Obsidian vault with wikilinks |
| `cypher` | `graph.cypher` | Neo4j Cypher import statements |
| `graphml` | `graph.graphml` | GraphML XML for Gephi/Cytoscape |

Default: `json,html`

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

# Skip expensive computations
gfy compare --skip-trees --skip-communities ./v1 ./v2
```

**Comparison metrics (11 total):**

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

**Cross-project flags:**

- `--normalize` — Align nodes by structural fingerprint (Weisfeiler-Lehman) instead of ID
- `--sensitivity` (0-1) — How aggressively to look through renames/refactors for intrinsic similarity. At higher values: uses semantic AHU hashing (NodeType + behavioral tags), cross-project weights (zeroes useless Jaccard), and more permissive alignment thresholds

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
```

Tags: `throws`, `catches`, `logs`, `fs`, `net`, `exec`, `async`, `unsafe`, `test`

### serve

Starts an [MCP](https://modelcontextprotocol.io/) stdio server exposing the graph for AI assistants.

```bash
gfy serve gfy-out/graph.json
```

**MCP tools:** `query_graph`, `get_node`, `get_neighbors`, `get_community`, `god_nodes`, `graph_stats`, `shortest_path`

### query

Search the graph by keyword with fuzzy matching, diacritics normalization, and degree-weighted ranking.

```bash
gfy query gfy-out/graph.json "authentication"
gfy query gfy-out/graph.json "servr"   # typo-tolerant
```

### path

Find the shortest path between two nodes.

```bash
gfy path gfy-out/graph.json "Server" "Database"
```

### watch

Monitor a directory for code changes and automatically rebuild the graph. Opens a live-reloading web UI with real-time updates via SSE.

```bash
gfy watch .
# Live graph: http://localhost:52431
```

## Supported Languages (22)

Go, Python, JavaScript, TypeScript, Java, Rust, C, C++, Ruby, C#, Kotlin, Scala, PHP, Swift, Lua, Zig, PowerShell, Elixir, Julia, Objective-C, Dart, Verilog.

Plus Vue, Svelte (via JS extractor) and Blade templates (regex).

## How It Works

```
detect → extract → build → cluster → analyze → report → export
```

1. **Detect** — Walk the filesystem, classify files (code, document, paper, image, video), respect `.gfyignore`
2. **Extract** — Parse code with [tree-sitter](https://tree-sitter.github.io/) (via [gotreesitter](https://github.com/odvcencio/gotreesitter)) to extract classes, functions, imports, call graphs
3. **Build** — Assemble extracted nodes and edges into a graph with ID normalization and deduplication
4. **Cluster** — Detect communities using the Louvain algorithm
5. **Analyze** — Identify god nodes (most connected), surprising connections (cross-file/cross-community), suggested questions
6. **Report** — Generate `GRAPH_REPORT.md` with findings
7. **Export** — Write graph in requested formats

Every relationship is tagged with confidence: `EXTRACTED` (from AST), `INFERRED` (cross-file resolution), or `AMBIGUOUS`.

## Output

```
gfy-out/
├── GRAPH_REPORT.md    # god nodes, surprising connections, community structure
├── graph.json         # queryable graph (NetworkX-compatible)
├── graph.html         # interactive visualization (open in browser)
├── graph.cypher       # Neo4j import (optional)
├── graph.graphml      # Gephi/Cytoscape (optional)
└── obsidian/          # Obsidian vault (optional)
```

## Credits

This project is a Go port of **[graphify](https://github.com/safishamsi/graphify)** by [Safi Shamsi](https://github.com/safishamsi). The original Python implementation provides the architectural design, pipeline structure, extraction patterns, and HTML visualization template that this project faithfully reproduces in Go.

- **Original project:** [github.com/safishamsi/graphify](https://github.com/safishamsi/graphify)
- **PyPI package:** [graphifyy](https://pypi.org/project/graphifyy/)
- **Tree-sitter runtime:** [gotreesitter](https://github.com/odvcencio/gotreesitter) by Oscar Dov Vcencio — pure Go, zero CGO, 206 embedded grammars
- **MCP SDK:** [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk)

## License

See [graphify](https://github.com/safishamsi/graphify) for the original project's license terms.
