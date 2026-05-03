package semantic

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qiangli/gfy/internal/types"
)

// ToolParam describes a parameter for an AST query tool.
type ToolParam struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ASTTool defines a tool the LLM can call to query the AST extraction result.
type ASTTool struct {
	Name        string
	Description string
	Parameters  map[string]ToolParam
	Required    []string
	Execute     func(args map[string]any) string
}

// NewASTTools creates the set of AST query tools backed by the given extraction result.
func NewASTTools(result *types.ExtractionResult) []ASTTool {
	// Pre-build indexes for efficient lookups.
	nodeByID := make(map[string]*types.Node, len(result.Nodes))
	nodesByFile := make(map[string][]*types.Node)
	for i := range result.Nodes {
		n := &result.Nodes[i]
		nodeByID[n.ID] = n
		if n.SourceFile != "" {
			nodesByFile[n.SourceFile] = append(nodesByFile[n.SourceFile], n)
		}
	}

	// Pre-build edge index: node ID → edges.
	edgesByNode := make(map[string][]types.Edge)
	for _, e := range result.Edges {
		edgesByNode[e.Source] = append(edgesByNode[e.Source], e)
		edgesByNode[e.Target] = append(edgesByNode[e.Target], e)
	}

	// Pre-build TF-IDF index for similarity search.
	allDocs := make([][]string, len(result.Nodes))
	for i := range result.Nodes {
		allDocs[i] = tokenize(buildNodeDocument(result.Nodes[i]))
	}
	tfidf := newTFIDF(allDocs)

	// Pre-build tag index: tag → node IDs.
	nodesByTag := make(map[string][]string)
	for _, n := range result.Nodes {
		for _, tag := range n.Tags {
			nodesByTag[tag] = append(nodesByTag[tag], n.ID)
		}
	}

	// Pre-build incoming/outgoing edge indexes.
	outgoing := make(map[string][]types.Edge) // source → edges
	incoming := make(map[string][]types.Edge) // target → edges
	for _, e := range result.Edges {
		outgoing[e.Source] = append(outgoing[e.Source], e)
		incoming[e.Target] = append(incoming[e.Target], e)
	}

	// Pre-compute root nodes: nodes with no incoming edges (entry points).
	incomingSet := make(map[string]bool)
	for _, e := range result.Edges {
		incomingSet[e.Target] = true
	}

	return []ASTTool{
		{
			Name:        "search_nodes",
			Description: "Search AST nodes by label or keyword. Returns matching node IDs, labels, and source files.",
			Parameters: map[string]ToolParam{
				"query":       {Type: "string", Description: "Search query (matched against node labels)"},
				"max_results": {Type: "integer", Description: "Maximum results to return (default 10)"},
			},
			Required: []string{"query"},
			Execute: func(args map[string]any) string {
				query := argStr(args, "query")
				maxResults := argInt(args, "max_results", 10)
				return toolSearchNodes(result.Nodes, query, maxResults)
			},
		},
		{
			Name:        "get_node_detail",
			Description: "Get full details of a node: label, comment/docstring, tags, log messages, throw messages, source location.",
			Parameters: map[string]ToolParam{
				"node_id": {Type: "string", Description: "The node ID to look up"},
			},
			Required: []string{"node_id"},
			Execute: func(args map[string]any) string {
				id := argStr(args, "node_id")
				n, ok := nodeByID[id]
				if !ok {
					return "Node not found: " + id
				}
				return toolNodeDetail(n)
			},
		},
		{
			Name:        "get_neighbors",
			Description: "Get all edges connected to a node, showing related nodes and relationship types.",
			Parameters: map[string]ToolParam{
				"node_id":  {Type: "string", Description: "The node ID to get neighbors for"},
				"relation": {Type: "string", Description: "Optional: filter by relation type (calls, contains, imports, etc.)"},
			},
			Required: []string{"node_id"},
			Execute: func(args map[string]any) string {
				id := argStr(args, "node_id")
				relation := argStr(args, "relation")
				edges := edgesByNode[id]
				if len(edges) == 0 {
					return "No edges found for: " + id
				}
				return toolNeighbors(edges, id, relation, nodeByID)
			},
		},
		{
			Name:        "list_files",
			Description: "List all source files in the codebase with node counts per file.",
			Parameters:  map[string]ToolParam{},
			Required:    nil,
			Execute: func(args map[string]any) string {
				return toolListFiles(nodesByFile)
			},
		},
		{
			Name:        "get_file_nodes",
			Description: "Get all nodes defined in a specific source file.",
			Parameters: map[string]ToolParam{
				"source_file": {Type: "string", Description: "The source file path to look up"},
			},
			Required: []string{"source_file"},
			Execute: func(args map[string]any) string {
				file := argStr(args, "source_file")
				nodes := nodesByFile[file]
				if len(nodes) == 0 {
					return "No nodes found in: " + file
				}
				return toolFileNodes(nodes, file)
			},
		},
		{
			Name:        "similar_nodes",
			Description: "Find nodes semantically similar to a text query using TF-IDF cosine similarity. Matches against node labels, comments, log messages, and throw messages.",
			Parameters: map[string]ToolParam{
				"query":       {Type: "string", Description: "Natural language query to find similar nodes (e.g., 'authentication error handling')"},
				"max_results": {Type: "integer", Description: "Maximum results to return (default 5)"},
			},
			Required: []string{"query"},
			Execute: func(args map[string]any) string {
				query := argStr(args, "query")
				maxResults := argInt(args, "max_results", 5)
				return toolSimilarNodes(result.Nodes, tfidf, query, maxResults)
			},
		},
		{
			Name:        "search_by_tag",
			Description: "Find nodes by behavioral tag. Tags: throws, logs, fs, net, exec, async, unsafe, test, catches, comment.",
			Parameters: map[string]ToolParam{
				"tag": {Type: "string", Description: "The tag to search for (e.g., 'throws', 'logs', 'fs')"},
			},
			Required: []string{"tag"},
			Execute: func(args map[string]any) string {
				tag := argStr(args, "tag")
				ids := nodesByTag[tag]
				if len(ids) == 0 {
					return "No nodes with tag: " + tag
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d nodes tagged '%s':\n", len(ids), tag)
				for _, id := range ids {
					if n, ok := nodeByID[id]; ok {
						fmt.Fprintf(&b, "  %s: %s [%s]\n", id, n.Label, n.SourceFile)
					}
				}
				return b.String()
			},
		},
		{
			Name:        "search_comments",
			Description: "Search through node comments, log messages, and throw messages for a keyword.",
			Parameters: map[string]ToolParam{
				"keyword":     {Type: "string", Description: "Keyword to search for in comments/logs/throws"},
				"max_results": {Type: "integer", Description: "Maximum results to return (default 10)"},
			},
			Required: []string{"keyword"},
			Execute: func(args map[string]any) string {
				keyword := strings.ToLower(argStr(args, "keyword"))
				maxResults := argInt(args, "max_results", 10)
				return toolSearchComments(result.Nodes, keyword, maxResults)
			},
		},
		{
			Name:        "get_callers",
			Description: "Get nodes that call or reference this node (incoming edges only).",
			Parameters: map[string]ToolParam{
				"node_id": {Type: "string", Description: "The node ID to find callers for"},
			},
			Required: []string{"node_id"},
			Execute: func(args map[string]any) string {
				id := argStr(args, "node_id")
				edges := incoming[id]
				if len(edges) == 0 {
					return "No callers found for: " + id
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d callers of %s:\n", len(edges), id)
				for _, e := range edges {
					label := e.Source
					if n, ok := nodeByID[e.Source]; ok {
						label = n.Label
					}
					fmt.Fprintf(&b, "  %s (%s) [%s]\n", e.Source, label, e.Relation)
				}
				return b.String()
			},
		},
		{
			Name:        "get_callees",
			Description: "Get nodes that this node calls or references (outgoing edges only).",
			Parameters: map[string]ToolParam{
				"node_id": {Type: "string", Description: "The node ID to find callees for"},
			},
			Required: []string{"node_id"},
			Execute: func(args map[string]any) string {
				id := argStr(args, "node_id")
				edges := outgoing[id]
				if len(edges) == 0 {
					return "No callees found for: " + id
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d callees of %s:\n", len(edges), id)
				for _, e := range edges {
					label := e.Target
					if n, ok := nodeByID[e.Target]; ok {
						label = n.Label
					}
					fmt.Fprintf(&b, "  %s (%s) [%s]\n", e.Target, label, e.Relation)
				}
				return b.String()
			},
		},
		{
			Name:        "search_files",
			Description: "Find source files matching a path pattern (substring match).",
			Parameters: map[string]ToolParam{
				"pattern": {Type: "string", Description: "Path substring to match (e.g., 'auth', 'internal/db')"},
			},
			Required: []string{"pattern"},
			Execute: func(args map[string]any) string {
				pattern := strings.ToLower(argStr(args, "pattern"))
				var b strings.Builder
				count := 0
				for path, nodes := range nodesByFile {
					if strings.Contains(strings.ToLower(path), pattern) {
						fmt.Fprintf(&b, "  %s (%d nodes)\n", path, len(nodes))
						count++
					}
				}
				if count == 0 {
					return "No files matching: " + pattern
				}
				return fmt.Sprintf("%d files matching '%s':\n%s", count, pattern, b.String())
			},
		},
		{
			Name:        "get_root_nodes",
			Description: "Get root nodes — entry points with no incoming edges. These are top-level entities like files, main functions, and packages that nothing else calls or references.",
			Parameters: map[string]ToolParam{
				"max_results": {Type: "integer", Description: "Maximum results to return (default 20)"},
			},
			Required: nil,
			Execute: func(args map[string]any) string {
				maxResults := argInt(args, "max_results", 20)
				var roots []types.Node
				for _, n := range result.Nodes {
					if !incomingSet[n.ID] {
						roots = append(roots, n)
					}
				}
				if len(roots) == 0 {
					return "No root nodes found"
				}
				if len(roots) > maxResults {
					roots = roots[:maxResults]
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d root nodes (no incoming edges):\n", len(roots))
				for _, n := range roots {
					out := len(outgoing[n.ID])
					fmt.Fprintf(&b, "  %s: %s [%s] → %d outgoing\n", n.ID, n.Label, n.SourceFile, out)
				}
				return b.String()
			},
		},
		{
			Name:        "get_leaf_nodes",
			Description: "Get leaf nodes — terminal entities with no outgoing edges. These are utility functions, constants, and sinks that don't call or reference anything else.",
			Parameters: map[string]ToolParam{
				"max_results": {Type: "integer", Description: "Maximum results to return (default 20)"},
			},
			Required: nil,
			Execute: func(args map[string]any) string {
				maxResults := argInt(args, "max_results", 20)
				var leaves []types.Node
				for _, n := range result.Nodes {
					if len(outgoing[n.ID]) == 0 {
						leaves = append(leaves, n)
					}
				}
				if len(leaves) == 0 {
					return "No leaf nodes found"
				}
				if len(leaves) > maxResults {
					leaves = leaves[:maxResults]
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d leaf nodes (no outgoing edges):\n", len(leaves))
				for _, n := range leaves {
					in := len(incoming[n.ID])
					fmt.Fprintf(&b, "  %s: %s [%s] ← %d incoming\n", n.ID, n.Label, n.SourceFile, in)
				}
				return b.String()
			},
		},
		{
			Name:        "get_path",
			Description: "Find the shortest path between two nodes, showing each hop and the relationship type.",
			Parameters: map[string]ToolParam{
				"from":     {Type: "string", Description: "Source node ID"},
				"to":       {Type: "string", Description: "Target node ID"},
				"max_hops": {Type: "integer", Description: "Maximum path length (default 10)"},
			},
			Required: []string{"from", "to"},
			Execute: func(args map[string]any) string {
				from := argStr(args, "from")
				to := argStr(args, "to")
				maxHops := argInt(args, "max_hops", 10)
				return toolGetPath(from, to, maxHops, edgesByNode, nodeByID)
			},
		},
		{
			Name:        "get_subgraph",
			Description: "Get the neighborhood of a node via BFS traversal. Returns all nodes and edges within the given depth.",
			Parameters: map[string]ToolParam{
				"node_id": {Type: "string", Description: "The starting node ID"},
				"depth":   {Type: "integer", Description: "BFS traversal depth (default 2)"},
			},
			Required: []string{"node_id"},
			Execute: func(args map[string]any) string {
				id := argStr(args, "node_id")
				depth := argInt(args, "depth", 2)
				return toolGetSubgraph(id, depth, edgesByNode, nodeByID)
			},
		},
		{
			Name:        "walk_chain",
			Description: "Follow edges of a specific relation type transitively from a starting node. Useful for tracing call chains, containment hierarchies, or import paths.",
			Parameters: map[string]ToolParam{
				"node_id":   {Type: "string", Description: "The starting node ID"},
				"relation":  {Type: "string", Description: "Relation type to follow (e.g., 'calls', 'contains', 'imports')"},
				"direction": {Type: "string", Description: "Direction: 'outgoing' (default) or 'incoming'"},
				"max_depth": {Type: "integer", Description: "Maximum chain depth (default 5)"},
			},
			Required: []string{"node_id", "relation"},
			Execute: func(args map[string]any) string {
				id := argStr(args, "node_id")
				relation := argStr(args, "relation")
				direction := argStr(args, "direction")
				if direction == "" {
					direction = "outgoing"
				}
				maxDepth := argInt(args, "max_depth", 5)
				return toolWalkChain(id, relation, direction, maxDepth, outgoing, incoming, nodeByID)
			},
		},
		{
			Name:        "get_hub_nodes",
			Description: "Get the most connected nodes in the graph (highest degree). These are the central entities that many other nodes depend on or reference.",
			Parameters: map[string]ToolParam{
				"top_n": {Type: "integer", Description: "Number of top nodes to return (default 10)"},
			},
			Required: nil,
			Execute: func(args map[string]any) string {
				topN := argInt(args, "top_n", 10)
				return toolGetHubNodes(result.Nodes, edgesByNode, topN)
			},
		},
	}
}

// --- Tool implementations ---

func toolSearchNodes(nodes []types.Node, query string, maxResults int) string {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return "No valid search terms"
	}

	type match struct {
		node  *types.Node
		score int
	}
	var matches []match

	for i := range nodes {
		n := &nodes[i]
		labelTokens := tokenize(n.Label)
		score := 0
		for _, qt := range queryTokens {
			for _, lt := range labelTokens {
				if lt == qt {
					score += 3 // exact token match
				} else if strings.HasPrefix(lt, qt) || strings.HasPrefix(qt, lt) {
					score += 1 // prefix match
				}
			}
			// Also check ID.
			if strings.Contains(strings.ToLower(n.ID), strings.ToLower(qt)) {
				score += 2
			}
		}
		if score > 0 {
			matches = append(matches, match{n, score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d matches:\n", len(matches))
	for _, m := range matches {
		fmt.Fprintf(&b, "  %s: %s [%s]\n", m.node.ID, m.node.Label, m.node.SourceFile)
	}
	return b.String()
}

func toolNodeDetail(n *types.Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\n", n.ID)
	fmt.Fprintf(&b, "Label: %s\n", n.Label)
	fmt.Fprintf(&b, "Type: %s\n", n.FileType)
	fmt.Fprintf(&b, "File: %s\n", n.SourceFile)
	if n.SourceLocation != "" {
		fmt.Fprintf(&b, "Location: %s\n", n.SourceLocation)
	}
	if n.Comment != "" {
		fmt.Fprintf(&b, "Comment: %s\n", n.Comment)
	}
	if len(n.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(n.Tags, ", "))
	}
	if len(n.LogMessages) > 0 {
		fmt.Fprintf(&b, "Logs: %s\n", strings.Join(n.LogMessages, "; "))
	}
	if len(n.ThrowMessages) > 0 {
		fmt.Fprintf(&b, "Throws: %s\n", strings.Join(n.ThrowMessages, "; "))
	}
	return b.String()
}

func toolNeighbors(edges []types.Edge, nodeID, relationFilter string, nodeByID map[string]*types.Node) string {
	var b strings.Builder
	count := 0
	for _, e := range edges {
		if relationFilter != "" && e.Relation != relationFilter {
			continue
		}
		// Determine the "other" node.
		otherID := e.Target
		direction := "→"
		if e.Target == nodeID {
			otherID = e.Source
			direction = "←"
		}
		label := otherID
		if n, ok := nodeByID[otherID]; ok {
			label = n.Label
		}
		fmt.Fprintf(&b, "  %s %s %s (%s) [%s, %.1f]\n",
			direction, e.Relation, otherID, label, e.Confidence, e.ConfidenceScore)
		count++
	}
	if count == 0 {
		return "No matching edges"
	}
	header := fmt.Sprintf("%d edges for %s:\n", count, nodeID)
	return header + b.String()
}

func toolListFiles(nodesByFile map[string][]*types.Node) string {
	type fileInfo struct {
		path  string
		count int
	}
	files := make([]fileInfo, 0, len(nodesByFile))
	for path, nodes := range nodesByFile {
		files = append(files, fileInfo{path, len(nodes)})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})

	var b strings.Builder
	fmt.Fprintf(&b, "%d files:\n", len(files))
	for _, f := range files {
		fmt.Fprintf(&b, "  %s (%d nodes)\n", f.path, f.count)
	}
	return b.String()
}

func toolFileNodes(nodes []*types.Node, file string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes in %s:\n", len(nodes), file)
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s: %s (%s)\n", n.ID, n.Label, n.FileType)
	}
	return b.String()
}

func toolSearchComments(nodes []types.Node, keyword string, maxResults int) string {
	var b strings.Builder
	count := 0
	for i := range nodes {
		n := &nodes[i]
		matched := ""
		if strings.Contains(strings.ToLower(n.Comment), keyword) {
			matched = "comment: " + truncate(n.Comment, 80)
		}
		if matched == "" {
			for _, msg := range n.LogMessages {
				if strings.Contains(strings.ToLower(msg), keyword) {
					matched = "log: " + msg
					break
				}
			}
		}
		if matched == "" {
			for _, msg := range n.ThrowMessages {
				if strings.Contains(strings.ToLower(msg), keyword) {
					matched = "throw: " + msg
					break
				}
			}
		}
		if matched != "" {
			fmt.Fprintf(&b, "  %s: %s [%s] — %s\n", n.ID, n.Label, n.SourceFile, matched)
			count++
			if count >= maxResults {
				break
			}
		}
	}
	if count == 0 {
		return "No matches for keyword: " + keyword
	}
	return fmt.Sprintf("%d matches for '%s':\n%s", count, keyword, b.String())
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func toolSimilarNodes(nodes []types.Node, idx *tfidfIndex, query string, maxResults int) string {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return "No valid search terms"
	}

	// Build a query vector using the corpus IDF.
	tf := make(map[string]int)
	for _, t := range queryTokens {
		tf[t]++
	}
	queryVec := make(map[string]float64, len(tf))
	docLen := float64(len(queryTokens))
	for term, count := range tf {
		if idfVal, ok := idx.idf[term]; ok {
			queryVec[term] = (float64(count) / docLen) * idfVal
		}
	}

	// Find all node indices (we search all nodes).
	allIndices := make([]int, len(nodes))
	for i := range nodes {
		allIndices[i] = i
	}

	matches := findTopMatches(queryVec, idx.vectors, allIndices, maxResults, 0.05)

	if len(matches) == 0 {
		return "No similar nodes found"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Top %d similar nodes:\n", len(matches))
	for _, m := range matches {
		n := nodes[m.index]
		fmt.Fprintf(&b, "  %.2f  %s: %s [%s]\n", m.similarity, n.ID, n.Label, n.SourceFile)
		if n.Comment != "" {
			comment := n.Comment
			if len(comment) > 80 {
				comment = comment[:80] + "..."
			}
			fmt.Fprintf(&b, "        %s\n", comment)
		}
	}
	return b.String()
}

func toolGetPath(from, to string, maxHops int, edgesByNode map[string][]types.Edge, nodeByID map[string]*types.Node) string {
	if _, ok := nodeByID[from]; !ok {
		return "Source node not found: " + from
	}
	if _, ok := nodeByID[to]; !ok {
		return "Target node not found: " + to
	}

	// BFS shortest path.
	type step struct {
		node string
		path []string
	}
	visited := map[string]bool{from: true}
	queue := []step{{node: from, path: []string{from}}}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if len(curr.path) > maxHops+1 {
			break
		}

		for _, e := range edgesByNode[curr.node] {
			next := e.Target
			if next == curr.node {
				next = e.Source
			}
			if visited[next] {
				continue
			}
			visited[next] = true
			newPath := make([]string, len(curr.path)+1)
			copy(newPath, curr.path)
			newPath[len(curr.path)] = next

			if next == to {
				// Found — format the path.
				var b strings.Builder
				fmt.Fprintf(&b, "Path (%d hops):\n", len(newPath)-1)
				for i, nid := range newPath {
					label := nid
					if n, ok := nodeByID[nid]; ok {
						label = n.Label
					}
					if i > 0 {
						// Find the relation between previous and current.
						prev := newPath[i-1]
						rel := findRelation(prev, nid, edgesByNode)
						fmt.Fprintf(&b, "  -[%s]→\n", rel)
					}
					fmt.Fprintf(&b, "  %s (%s)\n", nid, label)
				}
				return b.String()
			}
			queue = append(queue, step{node: next, path: newPath})
		}
	}
	return fmt.Sprintf("No path found between %s and %s (max %d hops)", from, to, maxHops)
}

func findRelation(a, b string, edgesByNode map[string][]types.Edge) string {
	for _, e := range edgesByNode[a] {
		if e.Target == b || e.Source == b {
			return e.Relation
		}
	}
	return "?"
}

func toolGetSubgraph(startID string, depth int, edgesByNode map[string][]types.Edge, nodeByID map[string]*types.Node) string {
	if _, ok := nodeByID[startID]; !ok {
		return "Node not found: " + startID
	}

	visited := map[string]bool{startID: true}
	frontier := []string{startID}
	var edgeSet []string

	for d := 0; d < depth && len(frontier) > 0; d++ {
		var nextFrontier []string
		for _, nid := range frontier {
			for _, e := range edgesByNode[nid] {
				other := e.Target
				if other == nid {
					other = e.Source
				}
				edgeKey := fmt.Sprintf("  %s -[%s]→ %s", e.Source, e.Relation, e.Target)
				edgeSet = append(edgeSet, edgeKey)
				if !visited[other] {
					visited[other] = true
					nextFrontier = append(nextFrontier, other)
				}
			}
		}
		frontier = nextFrontier
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Subgraph around %s (depth %d): %d nodes\n", startID, depth, len(visited))
	b.WriteString("Nodes:\n")
	for nid := range visited {
		label := nid
		if n, ok := nodeByID[nid]; ok {
			label = n.Label
		}
		fmt.Fprintf(&b, "  %s (%s)\n", nid, label)
	}
	if len(edgeSet) > 0 {
		// Deduplicate edges.
		seen := make(map[string]bool)
		b.WriteString("Edges:\n")
		for _, e := range edgeSet {
			if !seen[e] {
				seen[e] = true
				b.WriteString(e)
				b.WriteRune('\n')
			}
		}
	}
	return b.String()
}

func toolWalkChain(startID, relation, direction string, maxDepth int, outgoing, incoming map[string][]types.Edge, nodeByID map[string]*types.Node) string {
	if _, ok := nodeByID[startID]; !ok {
		return "Node not found: " + startID
	}

	visited := map[string]bool{startID: true}
	var b strings.Builder

	label := startID
	if n, ok := nodeByID[startID]; ok {
		label = n.Label
	}
	fmt.Fprintf(&b, "Chain from %s (%s), relation=%s, direction=%s:\n", startID, label, relation, direction)

	current := []string{startID}
	depth := 0

	for depth < maxDepth && len(current) > 0 {
		depth++
		var next []string
		for _, nid := range current {
			var edges []types.Edge
			if direction == "incoming" {
				edges = incoming[nid]
			} else {
				edges = outgoing[nid]
			}
			for _, e := range edges {
				if e.Relation != relation {
					continue
				}
				target := e.Target
				if direction == "incoming" {
					target = e.Source
				}
				if visited[target] {
					continue
				}
				visited[target] = true
				tLabel := target
				if n, ok := nodeByID[target]; ok {
					tLabel = n.Label
				}
				indent := strings.Repeat("  ", depth)
				fmt.Fprintf(&b, "%s→ %s (%s)\n", indent, target, tLabel)
				next = append(next, target)
			}
		}
		current = next
	}

	if len(visited) == 1 {
		return fmt.Sprintf("No %s '%s' edges from %s", direction, relation, startID)
	}
	fmt.Fprintf(&b, "Total: %d nodes in chain\n", len(visited))
	return b.String()
}

func toolGetHubNodes(nodes []types.Node, edgesByNode map[string][]types.Edge, topN int) string {
	type hubEntry struct {
		node   *types.Node
		degree int
	}
	var hubs []hubEntry
	for i := range nodes {
		n := &nodes[i]
		deg := len(edgesByNode[n.ID])
		if deg > 0 {
			hubs = append(hubs, hubEntry{n, deg})
		}
	}
	sort.Slice(hubs, func(i, j int) bool {
		return hubs[i].degree > hubs[j].degree
	})
	if len(hubs) > topN {
		hubs = hubs[:topN]
	}
	if len(hubs) == 0 {
		return "No connected nodes found"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Top %d hub nodes by degree:\n", len(hubs))
	for i, h := range hubs {
		fmt.Fprintf(&b, "  %d. %s: %s [%s] (degree %d)\n", i+1, h.node.ID, h.node.Label, h.node.SourceFile, h.degree)
	}
	return b.String()
}

// --- Argument helpers ---

func argStr(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func argInt(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return defaultVal
}

// buildNodeDirectory creates a compact listing of all node IDs and labels.
// This is sent as the first "document" so the LLM knows what code entities
// exist and can use tools to drill into specific nodes.
func buildNodeDirectory(nodes []types.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		if n.FileType == "document" || n.FileType == "paper" ||
			n.FileType == "concept" || n.FileType == "rationale" {
			continue
		}
		b.WriteString(n.ID)
		b.WriteString(": ")
		b.WriteString(n.Label)
		if n.SourceFile != "" {
			b.WriteString(" [")
			b.WriteString(n.SourceFile)
			b.WriteRune(']')
		}
		b.WriteRune('\n')
	}
	return b.String()
}
