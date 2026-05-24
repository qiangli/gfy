package install

import "fmt"

// claudeSkillTemplate returns the SKILL.md body that Claude Code loads from
// ~/.claude/skills/gfy/SKILL.md. The frontmatter `description` is the trigger
// hint shown to the model; keep it focused on what gfy uniquely answers
// (graph-shaped questions, cross-file behaviour, structural diffs).
func claudeSkillTemplate(binaryPath string) string {
	return fmt.Sprintf(`---
name: gfy
description: |
  Build and query a knowledge graph of a codebase (functions, calls, imports,
  documents). Use when the user asks: how is X used, what calls Y, what reaches
  the network/filesystem, how do two branches differ structurally, or what is
  in this large unfamiliar repo. Backed by tree-sitter AST extraction across
  22 languages plus PDF/DOCX/XLSX ingestion.
---

# gfy — knowledge graph for codebases

gfy is a pure-Go CLI that extracts AST structure with tree-sitter, builds a
knowledge graph, and exposes it via interactive HTML, an MCP server, and CLI
commands. Single binary, zero dependencies.

## When to use

- "What functions call X?" / "Where is X used?"
- "What in this codebase touches the network / writes files / panics?"
- "How did this branch change the structure vs main?"
- "I'm new to this repo — what are the central modules?"
- "Find code related to 'rate limiting' / 'authentication'" (semantic search)

## Core commands

%s build .                              # build the graph; output → .gfy-out/
%s query . "authentication"             # fuzzy keyword search
%s query --semantic . "rate limiting"   # embedding-based semantic search
%s trace . --tag net                    # what reaches network calls
%s diff                                 # local working tree vs upstream
%s compare ./v1 ./v2                    # cross-project structural diff
%s serve .                              # MCP stdio server (15+ tools)

## Prerequisites for full power

- Ollama running locally (http://localhost:11434) for semantic extraction
  and embedding search. Suggest: 'ollama pull qwen3:8b' (chat) and
  'ollama pull nomic-embed-text' (embeddings).
- Without Ollama, gfy still produces a useful AST-only graph.

## Output layout

  .gfy-out/
    graph.json        — NetworkX-compatible node-link JSON
    graph.html        — interactive vis.js visualisation
    embeddings.bin    — binary sidecar (when embeddings enabled)
    GRAPH_REPORT.md   — god-nodes, surprising connections, communities
    cache/            — SHA256 per-file extraction cache

## Tips for using the MCP server

When the user has %s serve running, prefer these MCP tools over re-grepping:
- semantic_search — natural-language node lookup
- get_callers / get_callees — call graph navigation
- trace_calls — behavioural call chains (throws, fs, net, exec, ...)
- god_nodes, surprising_connections — anomaly insights
`,
		binaryPath, binaryPath, binaryPath, binaryPath, binaryPath, binaryPath, binaryPath, binaryPath)
}

// cursorRulesTemplate returns the .mdc body Cursor reads from
// ~/.cursor/rules/gfy.mdc. Cursor frontmatter accepts `description` and an
// optional `globs` field; we leave globs empty so the rule is always active.
func cursorRulesTemplate(binaryPath string) string {
	return fmt.Sprintf(`---
description: Use gfy to answer graph-shaped questions about the codebase (call graphs, structural diffs, behavioural tags, semantic search).
alwaysApply: false
---

# gfy

When the user asks structural or cross-file questions about the repo
(what calls X, what reaches the network, how does branch A differ from
branch B, what is this large codebase about), reach for gfy before
grepping or reading many files.

Common one-liners:

- %s build .                              — build .gfy-out/graph.json
- %s query . "authentication"             — fuzzy keyword search
- %s query --semantic . "rate limiting"   — embedding-based semantic search
- %s trace . --tag net                    — call chains reaching net calls
- %s diff                                 — local vs upstream structural diff
- %s serve .                              — MCP stdio server for richer tools

Output lives in .gfy-out/. graph.html is an interactive visualisation.
`,
		binaryPath, binaryPath, binaryPath, binaryPath, binaryPath, binaryPath)
}

// claudeMCPSnippet returns the JSON to add to ~/.claude.json (or the platform
// equivalent for Claude Desktop). Caller pastes this into the mcpServers map.
func claudeMCPSnippet(binaryPath string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "gfy": {
      "command": "%s",
      "args": ["serve", "."]
    }
  }
}`, binaryPath)
}

// cursorMCPSnippet returns the JSON for ~/.cursor/mcp.json.
func cursorMCPSnippet(binaryPath string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "gfy": {
      "command": "%s",
      "args": ["serve", "."]
    }
  }
}`, binaryPath)
}
