package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/gfy/internal/analyze"
	"github.com/qiangli/gfy/internal/build"
	"github.com/qiangli/gfy/internal/cache"
	clust "github.com/qiangli/gfy/internal/cluster"
	"github.com/qiangli/gfy/internal/compare"
	"github.com/qiangli/gfy/internal/detect"
	"github.com/qiangli/gfy/internal/export"
	"github.com/qiangli/gfy/internal/extract"
	"github.com/qiangli/gfy/internal/graph"
	"github.com/qiangli/gfy/internal/report"
	"github.com/qiangli/gfy/internal/search"
	"github.com/qiangli/gfy/internal/semantic"
	"github.com/qiangli/gfy/internal/serve"
	"github.com/qiangli/gfy/internal/source"
	"github.com/qiangli/gfy/internal/trace"
	"github.com/qiangli/gfy/internal/types"
	"github.com/qiangli/gfy/internal/watch"
)

var (
	version        = "dev"
	formats        string
	noSemantic     bool
	noCache        bool
	model          string
	ollamaURL      string
	outFlag        string
	viewAfterBuild bool
)

func init() {
	// Tree-sitter parsers allocate large lookup tables per file that
	// accumulate faster than the default GC can reclaim. Two problems:
	// 1. GOGC=100 (default) lets the heap double before GC triggers.
	// 2. Go's runtime keeps freed pages mapped, so RSS stays high and
	//    the OS OOM-killer fires even though memory is logically free.
	// Fix: trigger GC sooner (GOGC=50) and cap heap (GOMEMLIMIT=2GiB).
	// Both are overridable via env vars.
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(2 << 30) // 2 GiB
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "gfy",
		Short: "Build knowledge graphs from codebases",
		Long: "gfy extracts structure from source code using tree-sitter AST parsing,\n" +
			"builds a knowledge graph, detects communities, and generates interactive reports.\n\n" +
			"Sources can be a local directory, an archive (.zip, .tar, .tar.gz, .tgz),\n" +
			"or a git URL (https://, git://, ssh://). Archives and git repos are cached\n" +
			"under ~/.gfy/ for fast re-runs.\n\n" +
			"A pure Go port of graphify (https://github.com/safishamsi/graphify). See README for credits.",
		Version: version,
	}

	// --- build command (default action) ---
	buildCmd := &cobra.Command{
		Use:   "build <source>",
		Short: "Build a knowledge graph from a codebase",
		Long: "Scan the source, extract AST structure from code files, build a knowledge\n" +
			"graph, detect communities, and export results.\n\n" +
			"Source can be a local directory, an archive (.zip, .tar, .tar.gz, .tgz),\n" +
			"or a git URL. Archives and clones are cached under ~/.gfy/.\n\n" +
			"Examples:\n" +
			"  gfy build ./myproject\n" +
			"  gfy build project.zip\n" +
			"  gfy build https://github.com/user/repo",
		Args: cobra.ExactArgs(1),
		RunE: runBuild,
	}
	buildCmd.Flags().StringVarP(&formats, "format", "f", "json,html",
		"export formats: json,html,obsidian,cypher,graphml (comma-separated)")
	buildCmd.Flags().BoolVar(&noCache, "no-cache", false,
		"ignore and clear cached extraction results")
	buildCmd.Flags().BoolVar(&noSemantic, "no-semantic", false,
		"skip semantic extraction even if Ollama is available")
	buildCmd.Flags().StringVar(&model, "model", "",
		"LLM model for semantic extraction (auto-selects if empty)")
	buildCmd.Flags().StringVar(&ollamaURL, "ollama-url", "http://localhost:11434",
		"Ollama server URL")
	buildCmd.Flags().BoolVar(&viewAfterBuild, "view", false,
		"open the graph visualization in a browser after building")
	rootCmd.AddCommand(buildCmd)

	// --- serve command ---
	serveCmd := &cobra.Command{
		Use:   "serve <source>",
		Short: "Start an MCP stdio server for querying a graph",
		Long: "Load or build graph.json from the source and expose it via the\n" +
			"Model Context Protocol (MCP) over stdin/stdout.\n\n" +
			"Source can be a local directory, archive, or git URL.\n\n" +
			"Tools: query_graph, get_node, get_neighbors, get_community,\n" +
			"god_nodes, graph_stats, shortest_path.",
		Args: cobra.ExactArgs(1),
		RunE: runServe,
	}
	rootCmd.AddCommand(serveCmd)

	// --- query command ---
	queryCmd := &cobra.Command{
		Use:   "query <source> <question>",
		Short: "Search the knowledge graph by keyword",
		Long: "Find nodes matching search terms and display their BFS neighborhood.\n" +
			"Auto-builds the graph if it doesn't exist yet.\n\n" +
			"Source can be a local directory, archive, or git URL.",
		Args: cobra.ExactArgs(2),
		RunE: runQuery,
	}
	rootCmd.AddCommand(queryCmd)

	// --- path command ---
	pathCmd := &cobra.Command{
		Use:   "path <source> <from> <to>",
		Short: "Find the shortest path between two nodes",
		Long: "Find and display the shortest path between two nodes in the graph.\n" +
			"Auto-builds the graph if it doesn't exist yet.\n\n" +
			"Source can be a local directory, archive, or git URL.",
		Args: cobra.ExactArgs(3),
		RunE: runPath,
	}
	rootCmd.AddCommand(pathCmd)

	// --- trace command ---
	var traceTag string
	var traceDepth int
	var traceMax int
	traceCmd := &cobra.Command{
		Use:   "trace <source>",
		Short: "Trace call paths leading to nodes with a specific tag",
		Long: "Find all call chains that reach functions tagged with throws, logs,\n" +
			"fs, net, exec, async, unsafe, or test. Shows the execution paths\n" +
			"that lead to the tagged behavior.\n\n" +
			"Source can be a local directory, archive, or git URL.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrace(args[0], traceTag, traceDepth, traceMax)
		},
	}
	traceCmd.Flags().StringVar(&traceTag, "tag", "throws", "behavioral tag to trace (throws, logs, fs, net, exec, async, unsafe, test, catches)")
	traceCmd.Flags().IntVar(&traceDepth, "depth", 10, "maximum call chain depth")
	traceCmd.Flags().IntVar(&traceMax, "max", 20, "maximum number of chains to show")
	rootCmd.AddCommand(traceCmd)

	// --- view command ---
	viewCmd := &cobra.Command{
		Use:   "view <source>",
		Short: "Open the interactive graph visualization in a browser",
		Long: "Open graph.html in the default browser. Auto-builds the graph\n" +
			"if it doesn't exist yet.\n\n" +
			"Source can be a local directory, archive, or git URL.",
		Args: cobra.ExactArgs(1),
		RunE: runView,
	}
	rootCmd.AddCommand(viewCmd)

	// --- watch command ---
	watchCmd := &cobra.Command{
		Use:   "watch <source>",
		Short: "Watch for code changes and rebuild the graph",
		Long: "Monitor code files for changes using fsnotify and automatically\n" +
			"rebuild the graph (AST-only, no LLM). Debounce: 3 seconds.\n\n" +
			"Source can be a local directory, archive, or git URL.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := source.Resolve(args[0], outFlag)
			if err != nil {
				return err
			}
			return watch.Watch(info.SourceDir, 3*time.Second)
		},
	}
	rootCmd.AddCommand(watchCmd)

	// --- compare command ---
	var compareBranches []string
	var compareFormats string
	var compareSkipCommunities bool
	var compareRenameThreshold float64
	var compareNormalize bool
	var compareSkipTrees bool
	var compareEstimate bool
	var compareSensitivity float64
	var compareSensitivitySet bool
	compareCmd := &cobra.Command{
		Use:   "compare <source1> <source2> [sourceN...]",
		Short: "Compare knowledge graphs from different codebases or branches",
		Long: "Compare two or more knowledge graphs and generate a diff report.\n\n" +
			"Sources can be local directories, archives, git URLs, or pre-built\n" +
			"graph.json files. Use --branch to compare branches within one repo.\n\n" +
			"Use --normalize for cross-project comparison where node IDs differ but\n" +
			"structural roles are equivalent (e.g., MY_CONST=3.14 vs your_constant=3.14).\n\n" +
			"Examples:\n" +
			"  gfy compare ./project-v1 ./project-v2\n" +
			"  gfy compare --normalize ./repo-a ./repo-b\n" +
			"  gfy compare graph-old.json graph-new.json\n" +
			"  gfy compare --branch main --branch feature-x .\n" +
			"  gfy compare --branch main --branch dev --branch staging .\n" +
			"  gfy compare https://github.com/user/repo1 https://github.com/user/repo2",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			compareSensitivitySet = cmd.Flags().Changed("sensitivity")
			return runCompare(args, compareBranches, compareFormats, compareSkipCommunities, compareRenameThreshold, compareNormalize, compareSkipTrees, compareEstimate, compareSensitivity, compareSensitivitySet)
		},
	}
	compareCmd.Flags().StringSliceVar(&compareBranches, "branch", nil,
		"branches to compare (requires exactly one source argument)")
	compareCmd.Flags().StringVarP(&compareFormats, "format", "f", "markdown",
		"output format: markdown, json (comma-separated)")
	compareCmd.Flags().BoolVar(&compareSkipCommunities, "skip-communities", false,
		"skip community comparison (faster)")
	compareCmd.Flags().Float64Var(&compareRenameThreshold, "rename-threshold", 0.6,
		"minimum similarity for rename detection (0-1)")
	compareCmd.Flags().BoolVar(&compareNormalize, "normalize", false,
		"align nodes by structural fingerprint before comparing (for cross-project comparison)")
	compareCmd.Flags().BoolVar(&compareSkipTrees, "skip-trees", false,
		"skip tree comparison algorithms (faster)")
	compareCmd.Flags().BoolVar(&compareEstimate, "estimate", false,
		"N-way: compute only N-1 full comparisons, estimate the rest via triangle inequality")
	compareCmd.Flags().Float64Var(&compareSensitivity, "sensitivity", 0,
		"how aggressively to look through renames/refactors for intrinsic similarity (0-1, default: 0.5 with --normalize)")
	rootCmd.AddCommand(compareCmd)

	// --- diff command ---
	var diffBase string
	diffCmd := &cobra.Command{
		Use:   "diff [source]",
		Short: "Compare local working tree against remote tracking branch",
		Long: "Auto-detect the remote tracking branch, build knowledge graphs for\n" +
			"both the local working tree and the remote branch, then compare them.\n\n" +
			"If no source is given, uses the current directory.\n\n" +
			"Examples:\n" +
			"  gfy diff                    # auto-detect tracking branch\n" +
			"  gfy diff --base main        # force comparison against origin/main\n" +
			"  gfy diff ./myrepo           # run on a different repo path",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args, diffBase)
		},
	}
	diffCmd.Flags().StringVar(&diffBase, "base", "",
		"override tracking branch (e.g., main, develop)")
	rootCmd.AddCommand(diffCmd)

	rootCmd.PersistentFlags().StringVarP(&outFlag, "out", "o", "", "output directory (default: <path>/.gfy-out/)")

	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// --- build ---

func runBuild(cmd *cobra.Command, args []string) error {
	info, err := source.Resolve(args[0], outFlag)
	if err != nil {
		return err
	}
	absPath := info.SourceDir
	outDir := info.OutDir
	targetPath := args[0]

	if noCache {
		if err := cache.Clear(absPath); err != nil {
			fmt.Printf("  Warning: failed to clear cache: %v\n", err)
		} else {
			fmt.Println("Cache cleared.")
		}
	}

	// Step 1: Detect files.
	fmt.Println("Detecting files...")
	result := detect.Detect(absPath, false)
	fmt.Printf("  Found %d files (%d words)\n", result.TotalFiles, result.TotalWords)
	if result.Warning != "" {
		fmt.Printf("  Warning: %s\n", result.Warning)
	}
	if len(result.SkippedSensitive) > 0 {
		fmt.Printf("  Skipped %d sensitive file(s)\n", len(result.SkippedSensitive))
	}

	codeFiles := result.Files[types.Code]
	if len(codeFiles) == 0 {
		fmt.Println("No code files found.")
		return nil
	}

	// Step 2: Extract AST.
	fmt.Printf("Extracting from %d code file(s)...\n", len(codeFiles))
	extraction := extract.Extract(codeFiles, absPath)
	fmt.Printf("  Extracted %d nodes, %d edges\n", len(extraction.Nodes), len(extraction.Edges))

	// Step 3–7: Build, cluster, analyze, report, export with AST results.
	if err := buildAndExport(extraction, targetPath, outDir); err != nil {
		return err
	}

	// Step 8: Semantic extraction (background, non-blocking).
	// The AST graph is already exported. Semantic extraction runs concurrently
	// and re-exports an enriched graph when complete. Ctrl+C skips the wait.
	if !noSemantic {
		selectedModel := resolveModel(ollamaURL, model)
		nonCodeFiles := collectNonCodeFiles(result)
		if selectedModel != "" && len(nonCodeFiles) == 0 {
			fmt.Println("\n  No non-code files — skipping semantic extraction.")
		}
		if selectedModel != "" && len(nonCodeFiles) > 0 {
			fmt.Printf("\nSemantic extraction with %s (%d non-code files)...\n", selectedModel, len(nonCodeFiles))
			fmt.Println("AST graph is ready. Semantic enrichment runs in the background.")
			fmt.Println("Press Ctrl+C to exit without waiting.")

			// Send compact node directory as the first "document" so the LLM
			// learns what code entities exist. Tools provide on-demand detail.
			client := &semantic.Client{BaseURL: ollamaURL, Model: selectedModel}
			dirResult, dirText := semantic.ExtractNodeDirectory(client, extraction.Nodes)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				semResult, err := semantic.Extract(nonCodeFiles, absPath, semantic.Options{
					BaseURL:     ollamaURL,
					Model:       selectedModel,
					CodeSummary: dirText,
					ASTNodes:    extraction,
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "\n[semantic] Warning: extraction failed: %v\n", err)
					return
				}
				// Merge node directory extraction results into semantic results.
				semResult.Nodes = append(dirResult.Nodes, semResult.Nodes...)
				semResult.Edges = append(dirResult.Edges, semResult.Edges...)
				semResult.InputTokens += dirResult.InputTokens
				semResult.OutputTokens += dirResult.OutputTokens

				if len(semResult.Nodes) == 0 && len(semResult.Edges) == 0 {
					fmt.Fprintf(os.Stderr, "\n[semantic] No additional relationships found.\n")
					return
				}

				fmt.Fprintf(os.Stderr, "\n[semantic] Enriching graph: +%d nodes, +%d edges\n",
					len(semResult.Nodes), len(semResult.Edges))

				merged := semantic.Merge(extraction, semResult)
				merged = semantic.LinkSemanticToAST(merged)
				if err := buildAndExport(merged, targetPath, outDir); err != nil {
					fmt.Fprintf(os.Stderr, "[semantic] Warning: re-export failed: %v\n", err)
					return
				}
				fmt.Fprintf(os.Stderr, "[semantic] Graph updated with semantic relationships.\n")
			}()

			// Wait for completion or Ctrl+C.
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			select {
			case <-done:
				// Semantic extraction completed.
			case <-sigCh:
				fmt.Fprintf(os.Stderr, "\n[semantic] Interrupted. Cached progress will be used on next run.\n")
			}
			signal.Stop(sigCh)
		}
	}

	fmt.Printf("Output: %s\n", outDir)
	fmt.Println("Done.")

	if viewAfterBuild {
		htmlPath := filepath.Join(outDir, "graph.html")
		if _, err := os.Stat(htmlPath); err == nil {
			fmt.Printf("Opening %s\n", htmlPath)
			openBrowser(htmlPath)
		}
	}

	return nil
}

// resolveModel determines which LLM model to use for semantic extraction.
// Returns "" if no suitable model is available.
func resolveModel(ollamaURL, explicitModel string) string {
	if explicitModel != "" {
		return explicitModel
	}
	models, err := semantic.ListModels(ollamaURL)
	if err != nil {
		fmt.Println("  Ollama not detected — skipping semantic extraction.")
		fmt.Println("  Install Ollama and re-run to enrich the graph.")
		return ""
	}
	if len(models) == 0 {
		fmt.Println("  No models found in Ollama.")
		fmt.Println("  Run: ollama pull qwen3:8b")
		return ""
	}
	selected := semantic.SelectModel(models)
	if selected == "" {
		fmt.Println("  No suitable model found for semantic extraction.")
		fmt.Println("  Run: ollama pull qwen3:8b")
	}
	return selected
}

// buildAndExport runs the build→cluster→analyze→report→export pipeline.
func buildAndExport(extraction *types.ExtractionResult, targetPath, outDir string) error {
	fmt.Println("Building graph...")
	g := build.BuildFromResult(extraction, false)
	fmt.Printf("  Graph: %s\n", g)

	fmt.Println("Detecting communities...")
	communities := clust.Cluster(g)
	cohesionScores := clust.ScoreAll(g, communities)
	fmt.Printf("  Found %d communities\n", len(communities))

	communityLabels := buildCommunityLabels(g, communities)

	fmt.Println("Analyzing...")
	godNodes := analyze.GodNodes(g, 10)
	surprises := analyze.SurprisingConnections(g, communities, 5)
	questions := analyze.SuggestQuestions(g, communities, 7)

	fmt.Println("Generating report...")
	reportMD := report.Generate(g, communities, cohesionScores, godNodes, surprises, questions, targetPath)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	reportPath := filepath.Join(outDir, "GRAPH_REPORT.md")
	if err := os.WriteFile(reportPath, []byte(reportMD), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	fmt.Printf("  Wrote %s\n", reportPath)

	fmt.Println("Exporting...")
	fmtSet := parseFormats(formats)

	if fmtSet["json"] {
		p := filepath.Join(outDir, "graph.json")
		if _, err := export.ToJSON(g, p, true); err != nil {
			return fmt.Errorf("export JSON: %w", err)
		}
		fmt.Printf("  Wrote %s\n", p)
	}
	if fmtSet["html"] {
		p := filepath.Join(outDir, "graph.html")
		if err := export.ToHTML(g, communities, communityLabels, p); err != nil {
			return fmt.Errorf("export HTML: %w", err)
		}
		fmt.Printf("  Wrote %s\n", p)
	}
	if fmtSet["obsidian"] {
		p := filepath.Join(outDir, "obsidian")
		if err := export.ToObsidian(g, communities, communityLabels, cohesionScores, p); err != nil {
			return fmt.Errorf("export Obsidian: %w", err)
		}
		fmt.Printf("  Wrote %s/\n", p)
	}
	if fmtSet["cypher"] {
		p := filepath.Join(outDir, "graph.cypher")
		if err := export.ToCypher(g, p); err != nil {
			return fmt.Errorf("export Cypher: %w", err)
		}
		fmt.Printf("  Wrote %s\n", p)
	}
	if fmtSet["graphml"] {
		p := filepath.Join(outDir, "graph.graphml")
		if err := export.ToGraphML(g, communities, p); err != nil {
			return fmt.Errorf("export GraphML: %w", err)
		}
		fmt.Printf("  Wrote %s\n", p)
	}
	return nil
}

// --- serve ---

func runServe(cmd *cobra.Command, args []string) error {
	g, err := loadOrBuildGraph(args[0])
	if err != nil {
		return err
	}
	communities := clust.Cluster(g)
	fmt.Fprintf(os.Stderr, "gfy MCP server: %d nodes, %d edges, %d communities\n",
		g.NodeCount(), g.EdgeCount(), len(communities))
	return serve.Serve(g, communities)
}

// --- query ---

func runQuery(cmd *cobra.Command, args []string) error {
	g, err := loadOrBuildGraph(args[0])
	if err != nil {
		return err
	}

	results := search.ScoreNodes(g, args[1])
	if len(results) == 0 {
		fmt.Println("No matching nodes found.")
		return nil
	}
	if len(results) > 10 {
		results = results[:10]
	}

	startNodes := make([]string, len(results))
	for i, r := range results {
		startNodes[i] = r.ID
	}
	visited, edges := g.BFS(startNodes, 2)
	fmt.Printf("Query: %s\nSubgraph: %d nodes, %d edges\n\n", args[1], len(visited), len(edges))
	for _, id := range visited {
		attrs := g.NodeAttrs(id)
		fmt.Printf("  - %s (%s)\n", nodeAttr(attrs, "label"), nodeAttr(attrs, "file_type"))
	}
	return nil
}

// --- trace ---

func runTrace(pathArg, tag string, maxDepth, maxResults int) error {
	g, err := loadOrBuildGraph(pathArg)
	if err != nil {
		return err
	}

	chains := trace.TraceTag(g, tag, maxDepth, maxResults)
	if len(chains) == 0 {
		fmt.Printf("No call paths found leading to nodes tagged %q.\n", tag)
		return nil
	}

	fmt.Printf("Call paths reaching %q (%d chains):\n\n", tag, len(chains))
	for i, chain := range chains {
		fmt.Printf("Chain %d:\n", i+1)
		for j, node := range chain.Path {
			indent := strings.Repeat("  ", j)
			if j == len(chain.Path)-1 {
				fmt.Printf("%s→ %s [%s]\n", indent, node.Label, tag)
			} else {
				fmt.Printf("%s→ %s\n", indent, node.Label)
			}
		}
		fmt.Println()
	}
	return nil
}

// --- path ---

func runPath(cmd *cobra.Command, args []string) error {
	g, err := loadOrBuildGraph(args[0])
	if err != nil {
		return err
	}
	srcID := findNode(g, args[1])
	tgtID := findNode(g, args[2])
	if srcID == "" {
		return fmt.Errorf("source node not found: %s", args[1])
	}
	if tgtID == "" {
		return fmt.Errorf("target node not found: %s", args[2])
	}
	path := g.ShortestPath(srcID, tgtID, 0)
	if path == nil {
		fmt.Println("No path found.")
		return nil
	}
	for i, nid := range path {
		attrs := g.NodeAttrs(nid)
		fmt.Printf("%d. %s\n", i+1, nodeAttr(attrs, "label"))
	}
	return nil
}

// --- view ---

func runView(cmd *cobra.Command, args []string) error {
	info, err := source.Resolve(args[0], outFlag)
	if err != nil {
		return err
	}

	htmlPath := filepath.Join(info.OutDir, "graph.html")
	if _, err := os.Stat(htmlPath); err != nil {
		// No HTML yet — build it.
		fmt.Fprintf(os.Stderr, "No graph.html found, building from %s...\n", info.SourceDir)
		formats = "json,html"
		if err := runBuild(cmd, args); err != nil {
			return err
		}
	}

	fmt.Printf("Opening %s\n", htmlPath)
	return openBrowser(htmlPath)
}

// --- compare ---

func runCompare(args []string, branches []string, formatStr string, skipCommunities bool, renameThreshold float64, normalize bool, skipTrees bool, estimate bool, sensitivity float64, sensitivitySet bool) error {
	opts := compare.Options{
		RenameThreshold:    renameThreshold,
		SkipCommunities:    skipCommunities,
		Normalize:          normalize,
		SkipTreeComparison: skipTrees,
		EstimateMode:       estimate,
		Sensitivity:        sensitivity,
		SensitivitySet:     sensitivitySet,
		OnProgress: func(step string) {
			fmt.Printf("  %s\n", step)
		},
	}
	fmtSet := parseFormats(formatStr)

	var graphs []*graph.Graph
	var labels []string

	if len(branches) > 0 {
		// Branch mode: single source, multiple branches.
		if len(args) != 1 {
			return fmt.Errorf("--branch requires exactly one source argument")
		}
		if len(branches) < 2 {
			return fmt.Errorf("--branch requires at least two branches to compare")
		}
		for _, branch := range branches {
			fmt.Printf("Building graph for branch %q...\n", branch)
			g, err := buildGraphForBranch(args[0], branch)
			if err != nil {
				return fmt.Errorf("branch %s: %w", branch, err)
			}
			graphs = append(graphs, g)
			labels = append(labels, branch)
			fmt.Printf("  Branch %s: %d nodes, %d edges\n", branch, g.NodeCount(), g.EdgeCount())
		}
	} else {
		// Multi-source mode.
		if len(args) < 2 {
			return fmt.Errorf("compare requires at least two sources (or use --branch)")
		}
		for _, arg := range args {
			fmt.Printf("Loading graph from %s...\n", arg)
			g, err := loadOrBuildGraph(arg)
			if err != nil {
				return fmt.Errorf("source %s: %w", arg, err)
			}
			graphs = append(graphs, g)
			labels = append(labels, filepath.Base(arg))
			fmt.Printf("  %s: %d nodes, %d edges\n", filepath.Base(arg), g.NodeCount(), g.EdgeCount())
		}
	}

	// Determine output directory — use the first source's .gfy-out/ by default.
	outDir := outFlag
	if outDir == "" {
		base := args[0]
		if len(branches) > 0 {
			base = args[0]
		}
		absBase, err := filepath.Abs(base)
		if err == nil {
			outDir = filepath.Join(absBase, ".gfy-out")
		} else {
			outDir = ".gfy-out"
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	if len(graphs) == 2 {
		// Pairwise comparison.
		fmt.Println("\nComparing graphs...")
		result := compare.Compare(graphs[0], graphs[1], labels[0], labels[1], opts)

		if result.Alignment != nil {
			fmt.Printf("  Aligned %d node pairs (avg score: %.2f), %d unmatched in A, %d unmatched in B\n",
				result.Alignment.MatchedCount, result.Alignment.AvgScore,
				result.Alignment.UnmatchedACount, result.Alignment.UnmatchedBCount)
		}
		fmt.Printf("\nComposite similarity: %.2f%%\n", result.Summary.CompositeScore*100)
		fmt.Printf("  Graph:  Jaccard=%.2f  JSD=%.4f  NMI=%.2f  GED=%d\n",
			result.Similarity.NodeJaccard, result.Similarity.DegreeJSD,
			result.Similarity.CommunityNMI, result.Summary.ApproxGED)
		if result.Similarity.TreeScores != nil {
			ts := result.Similarity.TreeScores
			fmt.Printf("  Tree:   AHU=%.2f  TED=%.2f  MCS=%.2f  Freq=%.2f  Kernel=%.2f  AU=%.2f  Role=%.2f\n",
				ts.AHUSubtreeMatch, ts.TreeEditDistSim, ts.MaxCommonSubtree,
				ts.SubtreeFreqCos, ts.TreeKernelNorm, ts.AntiUnifCoverage, ts.RoleDistribution)
			if ts.SemanticAHU {
				fmt.Println("  (semantic AHU: NodeType+Tags hashing enabled)")
			}
		}
		fmt.Printf("  Nodes: +%d / -%d / ~%d\n",
			result.Summary.NodesAdded, result.Summary.NodesRemoved, result.Summary.NodesModified)
		fmt.Printf("  Edges: +%d / -%d / ~%d\n",
			result.Summary.EdgesAdded, result.Summary.EdgesRemoved, result.Summary.EdgesModified)
		if len(result.Renames) > 0 {
			fmt.Printf("  Rename candidates: %d\n", len(result.Renames))
		}

		// Print interpretation to console.
		fmt.Println()
		fmt.Print(compare.InterpretResults(result))

		ts := time.Now().Format("20060102-150405")
		if fmtSet["markdown"] {
			reportMD := compare.GenerateReport(result)
			p := filepath.Join(outDir, fmt.Sprintf("COMPARE_REPORT-%s.md", ts))
			if err := os.WriteFile(p, []byte(reportMD), 0o644); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			fmt.Printf("  Wrote %s\n", p)
		}
		if fmtSet["json"] {
			data, err := result.ToJSON()
			if err != nil {
				return fmt.Errorf("marshal JSON: %w", err)
			}
			p := filepath.Join(outDir, fmt.Sprintf("compare-%s.json", ts))
			if err := os.WriteFile(p, data, 0o644); err != nil {
				return fmt.Errorf("write JSON: %w", err)
			}
			fmt.Printf("  Wrote %s\n", p)
		}
	} else {
		// N-way comparison.
		fmt.Printf("\nComparing %d graphs...\n", len(graphs))
		result := compare.CompareN(graphs, labels, opts)

		if estimate {
			fmt.Println("\nSimilarity matrix (Composite Score, ~ = estimated):")
		} else {
			fmt.Println("\nSimilarity matrix (Composite Score):")
		}
		printHeatmap(result.Labels, result.Heatmap)
		fmt.Printf("\nCore entities (in all %d): %d nodes, %d edges\n",
			len(graphs), result.Core.NodeCount, result.Core.EdgeCount)
		for _, u := range result.Unique {
			fmt.Printf("Unique to %s: %d nodes\n", u.Label, u.NodeCount)
		}

		if len(result.Estimates) > 0 {
			fmt.Println("\nEstimated similarity (via triangle inequality):")
			for _, e := range result.Estimates {
				fmt.Printf("  %s\n", compare.FormatEstimate(&e))
			}
		}

		ts := time.Now().Format("20060102-150405")
		if fmtSet["markdown"] {
			reportMD := compare.GenerateNWayReport(result)
			p := filepath.Join(outDir, fmt.Sprintf("COMPARE_REPORT-%s.md", ts))
			if err := os.WriteFile(p, []byte(reportMD), 0o644); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			fmt.Printf("  Wrote %s\n", p)
		}
		if fmtSet["json"] {
			data, err := result.ToJSON()
			if err != nil {
				return fmt.Errorf("marshal JSON: %w", err)
			}
			p := filepath.Join(outDir, fmt.Sprintf("compare-%s.json", ts))
			if err := os.WriteFile(p, data, 0o644); err != nil {
				return fmt.Errorf("write JSON: %w", err)
			}
			fmt.Printf("  Wrote %s\n", p)
		}
	}

	fmt.Println("Done.")
	return nil
}

func buildGraphForBranch(repoPath, branch string) (*graph.Graph, error) {
	info, err := source.ResolveForBranch(repoPath, branch, "")
	if err != nil {
		return nil, err
	}

	// Check for cached graph.json.
	jsonPath := filepath.Join(info.OutDir, "graph.json")
	if g, err := graph.LoadJSON(jsonPath); err == nil {
		return g, nil
	}

	// Build from scratch.
	result := detect.Detect(info.SourceDir, false)
	codeFiles := result.Files[types.Code]
	if len(codeFiles) == 0 {
		return nil, fmt.Errorf("no code files found in %s (branch %s)", info.SourceDir, branch)
	}
	extraction := extract.Extract(codeFiles, info.SourceDir)
	g := build.BuildFromResult(extraction, false)

	// Cache for next time.
	os.MkdirAll(filepath.Dir(jsonPath), 0o755)
	export.ToJSON(g, jsonPath, true)
	return g, nil
}

// --- diff ---

func runDiff(args []string, baseOverride string) error {
	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return err
	}

	// Detect tracking branch.
	var tracking *source.TrackingInfo
	if baseOverride != "" {
		// User override: detect remote URL from git config, use specified branch.
		info, err := source.DetectTracking(absRepo)
		if err != nil {
			// If detection fails entirely, assume origin.
			tracking = &source.TrackingInfo{
				LocalBranch:  "HEAD",
				Remote:       "origin",
				RemoteBranch: baseOverride,
			}
			// Try to get the remote URL from the repo config.
			remoteURL, urlErr := source.RemoteURL(absRepo, "origin")
			if urlErr != nil {
				return fmt.Errorf("cannot determine remote URL: %w (git detection: %w)", urlErr, err)
			}
			tracking.RemoteURL = remoteURL
		} else {
			tracking = info
			tracking.RemoteBranch = baseOverride
		}
		fmt.Printf("Comparing local against: %s/%s\n", tracking.Remote, tracking.RemoteBranch)
	} else {
		tracking, err = source.DetectTracking(absRepo)
		if err != nil {
			return err
		}
		fmt.Printf("Branch: %s → tracks %s/%s\n", tracking.LocalBranch, tracking.Remote, tracking.RemoteBranch)
	}

	// Build graph from local working tree.
	fmt.Println("\nBuilding local graph...")
	localGraph, err := buildGraphFromDir(absRepo)
	if err != nil {
		return fmt.Errorf("local graph: %w", err)
	}
	fmt.Printf("  Local: %d nodes, %d edges\n", localGraph.NodeCount(), localGraph.EdgeCount())

	// Build graph from remote tracking branch.
	fmt.Printf("\nBuilding remote graph (%s/%s)...\n", tracking.Remote, tracking.RemoteBranch)
	remoteInfo, err := source.ResolveForBranch(tracking.RemoteURL, tracking.RemoteBranch, "")
	if err != nil {
		return fmt.Errorf("remote branch: %w", err)
	}

	// Sync local ignore files to remote clone so both sides use the same rules.
	syncIgnoreFiles(absRepo, remoteInfo.SourceDir)

	remoteGraph, err := buildGraphFromDir(remoteInfo.SourceDir)
	if err != nil {
		return fmt.Errorf("remote graph: %w", err)
	}
	fmt.Printf("  Remote: %d nodes, %d edges\n", remoteGraph.NodeCount(), remoteGraph.EdgeCount())

	// Compare: remote (before) vs local (after).
	fmt.Println("\nComparing graphs...")
	opts := compare.Options{
		RenameThreshold: 0.6,
		OnProgress: func(step string) {
			fmt.Printf("  %s\n", step)
		},
	}
	remoteLabel := fmt.Sprintf("%s/%s", tracking.Remote, tracking.RemoteBranch)
	localLabel := fmt.Sprintf("local/%s", tracking.LocalBranch)
	result := compare.Compare(remoteGraph, localGraph, remoteLabel, localLabel, opts)

	// Print summary.
	fmt.Printf("\nComposite similarity: %.2f%%\n", result.Summary.CompositeScore*100)
	fmt.Printf("  Nodes: +%d / -%d / ~%d\n",
		result.Summary.NodesAdded, result.Summary.NodesRemoved, result.Summary.NodesModified)
	fmt.Printf("  Edges: +%d / -%d / ~%d\n",
		result.Summary.EdgesAdded, result.Summary.EdgesRemoved, result.Summary.EdgesModified)
	if len(result.Renames) > 0 {
		fmt.Printf("  Rename candidates: %d\n", len(result.Renames))
	}

	fmt.Println()
	fmt.Print(compare.InterpretResults(result))

	// Write report.
	outDir := filepath.Join(absRepo, ".gfy-out")
	if outFlag != "" {
		outDir, _ = filepath.Abs(outFlag)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	ts := time.Now().Format("20060102-150405")
	reportMD := compare.GenerateReport(result)
	p := filepath.Join(outDir, fmt.Sprintf("DIFF_REPORT-%s.md", ts))
	if err := os.WriteFile(p, []byte(reportMD), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	fmt.Printf("\n  Wrote %s\n", p)
	fmt.Println("Done.")
	return nil
}

// syncIgnoreFiles copies .gitignore and .gfyignore from src to dst so that
// both local and remote builds use the same ignore rules during gfy diff.
func syncIgnoreFiles(src, dst string) {
	for _, name := range []string{".gitignore", ".gfyignore"} {
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		data, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		// Check if remote already has the same content.
		existing, _ := os.ReadFile(dstPath)
		if string(existing) == string(data) {
			continue
		}
		if err := os.WriteFile(dstPath, data, 0o644); err == nil {
			if len(existing) == 0 {
				fmt.Printf("  Applied local %s to remote clone\n", name)
			} else {
				fmt.Printf("  Updated remote %s with local version\n", name)
			}
		}
	}
}

// buildGraphFromDir builds a fresh knowledge graph from a directory.
func buildGraphFromDir(dir string) (*graph.Graph, error) {
	result := detect.Detect(dir, false)
	codeFiles := result.Files[types.Code]
	if len(codeFiles) == 0 {
		return nil, fmt.Errorf("no code files found in %s", dir)
	}
	extraction := extract.Extract(codeFiles, dir)
	return build.BuildFromResult(extraction, false), nil
}

func printHeatmap(labels []string, heatmap [][]float64) {
	// Header.
	fmt.Printf("%-12s", "")
	for _, l := range labels {
		fmt.Printf(" %8s", truncate(l, 8))
	}
	fmt.Println()

	for i, l := range labels {
		fmt.Printf("%-12s", truncate(l, 12))
		for j := range labels {
			fmt.Printf(" %8.2f", heatmap[i][j])
		}
		fmt.Println()
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// --- helpers ---

// loadOrBuildGraph accepts either a .json file path or a directory.
// If a .json file, loads it directly. If a directory, looks for
// <dir>/.gfy-out/graph.json — building the graph first if it doesn't exist.
func loadOrBuildGraph(pathArg string) (*graph.Graph, error) {
	absPath, _ := filepath.Abs(pathArg)

	// If it's a .json file, load directly.
	if strings.HasSuffix(strings.ToLower(absPath), ".json") {
		g, err := graph.LoadJSON(absPath)
		if err != nil {
			return nil, fmt.Errorf("load graph: %w", err)
		}
		return g, nil
	}

	// Resolve source (directory, archive, or git URL).
	info, err := source.Resolve(pathArg, outFlag)
	if err != nil {
		return nil, err
	}

	// Check for existing graph.json.
	jsonPath := filepath.Join(info.OutDir, "graph.json")
	if _, err := os.Stat(jsonPath); err == nil {
		g, err := graph.LoadJSON(jsonPath)
		if err != nil {
			return nil, fmt.Errorf("load graph: %w", err)
		}
		return g, nil
	}

	// No graph.json yet — build it.
	fmt.Fprintf(os.Stderr, "No graph found, building from %s...\n", info.SourceDir)
	result := detect.Detect(info.SourceDir, false)
	codeFiles := result.Files[types.Code]
	if len(codeFiles) == 0 {
		return nil, fmt.Errorf("no code files found in %s", info.SourceDir)
	}
	extraction := extract.Extract(codeFiles, info.SourceDir)
	g := build.BuildFromResult(extraction, false)

	// Save for next time.
	os.MkdirAll(filepath.Dir(jsonPath), 0o755)
	export.ToJSON(g, jsonPath, true)
	fmt.Fprintf(os.Stderr, "Built graph: %d nodes, %d edges → %s\n", g.NodeCount(), g.EdgeCount(), jsonPath)
	return g, nil
}

func nodeAttr(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func findNode(g *graph.Graph, label string) string {
	return search.FindNode(g, label)
}

func buildCommunityLabels(g *graph.Graph, communities map[int][]string) map[int]string {
	labels := make(map[int]string)
	for cid, nodes := range communities {
		bestLabel, bestDeg := "", -1
		for _, nid := range nodes {
			attrs := g.NodeAttrs(nid)
			label, _ := attrs["label"].(string)
			if deg := g.Degree(nid); deg > bestDeg {
				bestDeg = deg
				bestLabel = label
			}
		}
		if bestLabel != "" {
			labels[cid] = fmt.Sprintf("%s (%d nodes)", bestLabel, len(nodes))
		} else {
			labels[cid] = fmt.Sprintf("Community %d", cid)
		}
	}
	return labels
}

func collectNonCodeFiles(result *types.DetectionResult) []string {
	var files []string
	for ft, paths := range result.Files {
		if ft != types.Code {
			files = append(files, paths...)
		}
	}
	return files
}

func parseFormats(s string) map[string]bool {
	m := make(map[string]bool)
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(strings.ToLower(f))
		if f != "" {
			m[f] = true
		}
	}
	return m
}
