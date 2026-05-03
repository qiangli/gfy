// Package serve implements an MCP stdio server for querying the knowledge graph.
package serve

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qiangli/gfy/internal/analyze"
	"github.com/qiangli/gfy/internal/cluster"
	"github.com/qiangli/gfy/internal/graph"
	"github.com/qiangli/gfy/internal/search"
	"github.com/qiangli/gfy/internal/trace"
)

// Serve starts an MCP stdio server exposing graph query tools.
func Serve(g *graph.Graph, communities map[int][]string) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gfy",
		Version: "0.1.0",
	}, nil)

	registerTools(server, g, communities)

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func registerTools(server *mcp.Server, g *graph.Graph, communities map[int][]string) {
	// Pre-compute containment maps for hierarchy tools.
	children, parents := buildContainmentMaps(g)

	registerOverviewTools(server, g, communities)
	registerNavigationTools(server, g, children, parents)
	registerSearchTools(server, g)
	registerAnalysisTools(server, g, communities)
}

// --- Overview tools ---

func registerOverviewTools(server *mcp.Server, g *graph.Graph, communities map[int][]string) {
	// graph_stats: comprehensive overview of the graph
	mcp.AddTool(server, &mcp.Tool{
		Name:        "graph_stats",
		Description: "Return graph overview: node/edge counts, communities, confidence breakdown, relation types, and behavioral tags",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		confCounts := map[string]int{}
		relCounts := map[string]int{}
		for _, e := range g.Edges() {
			confCounts[attrStr(e.Attrs, "confidence")]++
			relCounts[attrStr(e.Attrs, "relation")]++
		}
		tagCounts := map[string]int{}
		for _, id := range g.Nodes() {
			for _, tag := range attrTags(g.NodeAttrs(id)) {
				tagCounts[tag]++
			}
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Nodes: %d\nEdges: %d\nCommunities: %d\n", g.NodeCount(), g.EdgeCount(), len(communities))
		fmt.Fprintf(&b, "\nConfidence:\n  EXTRACTED: %d\n  INFERRED: %d\n  AMBIGUOUS: %d\n",
			confCounts["EXTRACTED"], confCounts["INFERRED"], confCounts["AMBIGUOUS"])

		fmt.Fprintf(&b, "\nRelations:\n")
		for _, r := range sortedKeys(relCounts) {
			fmt.Fprintf(&b, "  %s: %d\n", r, relCounts[r])
		}
		if len(tagCounts) > 0 {
			fmt.Fprintf(&b, "\nBehavioral tags:\n")
			for _, t := range sortedKeys(tagCounts) {
				fmt.Fprintf(&b, "  %s: %d\n", t, tagCounts[t])
			}
		}
		return textResult(b.String()), nil, nil
	})
}

// --- Navigation tools ---

func registerNavigationTools(server *mcp.Server, g *graph.Graph, children, parents map[string][]childEntry) {
	// get_node: detailed node info
	type getNodeArgs struct {
		Label string `json:"label" jsonschema:"node label or ID to look up"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_node",
		Description: "Look up a node by label or ID, returning all attributes including tags, comments, and source location",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getNodeArgs) (*mcp.CallToolResult, any, error) {
		id := search.FindNode(g, args.Label)
		if id == "" {
			return textResult("Node not found: " + args.Label), nil, nil
		}
		return textResult(formatNode(g, id, children, parents)), nil, nil
	})

	// get_neighbors: edges from a node
	type getNeighborsArgs struct {
		Label    string `json:"label" jsonschema:"node label or ID"`
		Relation string `json:"relation,omitempty" jsonschema:"filter by relation type (calls, contains, imports, method, etc.)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_neighbors",
		Description: "Return direct neighbors of a node with edge metadata (relation, confidence)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getNeighborsArgs) (*mcp.CallToolResult, any, error) {
		id := search.FindNode(g, args.Label)
		if id == "" {
			return textResult("Node not found: " + args.Label), nil, nil
		}
		var lines []string
		for _, nb := range g.Neighbors(id) {
			eAttrs := g.EdgeAttrs(id, nb)
			rel := attrStr(eAttrs, "relation")
			if args.Relation != "" && rel != args.Relation {
				continue
			}
			conf := attrStr(eAttrs, "confidence")
			nbAttrs := g.NodeAttrs(nb)
			lines = append(lines, fmt.Sprintf("- %s (%s) [%s, %s]",
				attrStr(nbAttrs, "label"), attrStr(nbAttrs, "file_type"), rel, conf))
		}
		if len(lines) == 0 {
			return textResult("No neighbors found"), nil, nil
		}
		return textResult(strings.Join(lines, "\n")), nil, nil
	})

	// get_children: containment hierarchy children
	type getChildrenArgs struct {
		Label string `json:"label" jsonschema:"node label or ID"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_children",
		Description: "Return containment children of a node (functions inside a file, methods inside a class) via contains/method edges",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getChildrenArgs) (*mcp.CallToolResult, any, error) {
		id := search.FindNode(g, args.Label)
		if id == "" {
			return textResult("Node not found: " + args.Label), nil, nil
		}
		kids := children[id]
		if len(kids) == 0 {
			return textResult("No children found (leaf node)"), nil, nil
		}
		var lines []string
		for _, c := range kids {
			attrs := g.NodeAttrs(c.id)
			tags := attrTags(attrs)
			tagStr := ""
			if len(tags) > 0 {
				tagStr = " [" + strings.Join(tags, ", ") + "]"
			}
			lines = append(lines, fmt.Sprintf("- %s (%s)%s", attrStr(attrs, "label"), c.relation, tagStr))
		}
		return textResult(fmt.Sprintf("Children of %s (%d):\n%s", attrStr(g.NodeAttrs(id), "label"), len(kids), strings.Join(lines, "\n"))), nil, nil
	})

	// shortest_path
	type shortestPathArgs struct {
		Source  string `json:"source" jsonschema:"source node label or ID"`
		Target  string `json:"target" jsonschema:"target node label or ID"`
		MaxHops int    `json:"max_hops,omitempty" jsonschema:"max path length (default unlimited)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "shortest_path",
		Description: "Find the shortest path between two nodes",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args shortestPathArgs) (*mcp.CallToolResult, any, error) {
		srcID := search.FindNode(g, args.Source)
		tgtID := search.FindNode(g, args.Target)
		if srcID == "" {
			return textResult("Source not found: " + args.Source), nil, nil
		}
		if tgtID == "" {
			return textResult("Target not found: " + args.Target), nil, nil
		}
		path := g.ShortestPath(srcID, tgtID, args.MaxHops)
		if path == nil {
			return textResult("No path found"), nil, nil
		}
		var labels []string
		for _, nid := range path {
			labels = append(labels, attrStr(g.NodeAttrs(nid), "label"))
		}
		return textResult(fmt.Sprintf("Path (%d hops): %s", len(path)-1, strings.Join(labels, " → "))), nil, nil
	})

	// list_roots: top-level file nodes
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_roots",
		Description: "List root nodes (files/modules) that are not contained by any other node — the top of the containment hierarchy",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		var roots []string
		for _, id := range g.Nodes() {
			if _, hasParent := parents[id]; !hasParent {
				if kids := children[id]; len(kids) > 0 {
					roots = append(roots, id)
				}
			}
		}
		if len(roots) == 0 {
			return textResult("No root nodes found"), nil, nil
		}
		var lines []string
		for _, id := range roots {
			attrs := g.NodeAttrs(id)
			lines = append(lines, fmt.Sprintf("- %s (%d children, degree %d)",
				attrStr(attrs, "label"), len(children[id]), g.Degree(id)))
		}
		return textResult(fmt.Sprintf("Root nodes (%d):\n%s", len(roots), strings.Join(lines, "\n"))), nil, nil
	})

	// list_leaves: nodes with no containment children
	type listLeavesArgs struct {
		Tag string `json:"tag,omitempty" jsonschema:"filter by behavioral tag (throws, logs, fs, net, exec, async, unsafe, test, catches)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_leaves",
		Description: "List leaf nodes (functions/methods) that contain no other nodes — the bottom of the containment hierarchy",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listLeavesArgs) (*mcp.CallToolResult, any, error) {
		var leaves []string
		for _, id := range g.Nodes() {
			if _, isParent := children[id]; isParent {
				continue
			}
			// Must be a child of something to be in the hierarchy.
			if _, isChild := parents[id]; !isChild {
				continue
			}
			if args.Tag != "" && !hasTag(g.NodeAttrs(id), args.Tag) {
				continue
			}
			leaves = append(leaves, id)
		}
		if len(leaves) == 0 {
			msg := "No leaf nodes found"
			if args.Tag != "" {
				msg += " with tag: " + args.Tag
			}
			return textResult(msg), nil, nil
		}
		if len(leaves) > 100 {
			leaves = leaves[:100]
		}
		var lines []string
		for _, id := range leaves {
			attrs := g.NodeAttrs(id)
			tags := attrTags(attrs)
			tagStr := ""
			if len(tags) > 0 {
				tagStr = " [" + strings.Join(tags, ", ") + "]"
			}
			lines = append(lines, fmt.Sprintf("- %s (%s)%s",
				attrStr(attrs, "label"), attrStr(attrs, "source_file"), tagStr))
		}
		header := fmt.Sprintf("Leaf nodes (%d", len(leaves))
		if len(leaves) == 100 {
			header += ", showing first 100"
		}
		header += "):"
		return textResult(fmt.Sprintf("%s\n%s", header, strings.Join(lines, "\n"))), nil, nil
	})

	// list_nodes: filter nodes by attribute
	type listNodesArgs struct {
		SourceFile string `json:"source_file,omitempty" jsonschema:"filter by source file path (substring match)"`
		Tag        string `json:"tag,omitempty" jsonschema:"filter by behavioral tag"`
		FileType   string `json:"file_type,omitempty" jsonschema:"filter by file_type (code, document, concept, rationale)"`
		Limit      int    `json:"limit,omitempty" jsonschema:"max results (default 50)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_nodes",
		Description: "List nodes with optional filters by source file, behavioral tag, or file type",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listNodesArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		var matches []string
		for _, id := range g.Nodes() {
			attrs := g.NodeAttrs(id)
			if args.SourceFile != "" && !strings.Contains(attrStr(attrs, "source_file"), args.SourceFile) {
				continue
			}
			if args.Tag != "" && !hasTag(attrs, args.Tag) {
				continue
			}
			if args.FileType != "" && attrStr(attrs, "file_type") != args.FileType {
				continue
			}
			matches = append(matches, id)
			if len(matches) >= limit {
				break
			}
		}
		if len(matches) == 0 {
			return textResult("No nodes match the given filters"), nil, nil
		}
		var lines []string
		for _, id := range matches {
			attrs := g.NodeAttrs(id)
			tags := attrTags(attrs)
			tagStr := ""
			if len(tags) > 0 {
				tagStr = " [" + strings.Join(tags, ", ") + "]"
			}
			lines = append(lines, fmt.Sprintf("- %s (%s, degree %d)%s",
				attrStr(attrs, "label"), attrStr(attrs, "source_file"), g.Degree(id), tagStr))
		}
		return textResult(fmt.Sprintf("Nodes (%d):\n%s", len(matches), strings.Join(lines, "\n"))), nil, nil
	})
}

// --- Search tools ---

func registerSearchTools(server *mcp.Server, g *graph.Graph) {
	// search: fuzzy keyword search
	type searchArgs struct {
		Query string `json:"query" jsonschema:"search terms to match against node labels"`
		Limit int    `json:"limit,omitempty" jsonschema:"max results (default 10)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Fuzzy search for nodes by keyword, returning ranked matches with scores",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		}
		results := search.ScoreNodes(g, args.Query)
		if len(results) > limit {
			results = results[:limit]
		}
		if len(results) == 0 {
			return textResult("No matches found for: " + args.Query), nil, nil
		}
		var lines []string
		for i, r := range results {
			attrs := g.NodeAttrs(r.ID)
			lines = append(lines, fmt.Sprintf("%d. %s (%s, degree %d) [score %.1f]",
				i+1, attrStr(attrs, "label"), attrStr(attrs, "source_file"), g.Degree(r.ID), r.Score))
		}
		return textResult(fmt.Sprintf("Search results for %q:\n%s", args.Query, strings.Join(lines, "\n"))), nil, nil
	})

	// get_subgraph: extract focused view
	type subgraphArgs struct {
		Labels           []string `json:"labels" jsonschema:"node labels or IDs to include"`
		IncludeNeighbors bool     `json:"include_neighbors,omitempty" jsonschema:"also include direct neighbors of listed nodes"`
		Depth            int      `json:"depth,omitempty" jsonschema:"BFS depth from listed nodes (default 0 means exact nodes only, 1 means include neighbors)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_subgraph",
		Description: "Extract a focused subgraph around specific nodes with optional BFS expansion",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args subgraphArgs) (*mcp.CallToolResult, any, error) {
		var nodeIDs []string
		for _, label := range args.Labels {
			id := search.FindNode(g, label)
			if id != "" {
				nodeIDs = append(nodeIDs, id)
			}
		}
		if len(nodeIDs) == 0 {
			return textResult("No matching nodes found"), nil, nil
		}
		depth := args.Depth
		if args.IncludeNeighbors && depth <= 0 {
			depth = 1
		}
		if depth > 0 {
			visited, _ := g.BFS(nodeIDs, depth)
			nodeIDs = visited
		}
		sub := g.Subgraph(nodeIDs)
		return textResult(subgraphToText(sub, sub.Nodes(), sub.Edges())), nil, nil
	})
}

// --- Analysis tools ---

func registerAnalysisTools(server *mcp.Server, g *graph.Graph, communities map[int][]string) {
	// god_nodes
	type godNodesArgs struct {
		TopN int `json:"top_n,omitempty" jsonschema:"number of top nodes (default 10)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "god_nodes",
		Description: "Return the most connected entities in the graph (excluding file-level hubs)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args godNodesArgs) (*mcp.CallToolResult, any, error) {
		n := args.TopN
		if n <= 0 {
			n = 10
		}
		gods := analyze.GodNodes(g, n)
		var lines []string
		for i, gn := range gods {
			lines = append(lines, fmt.Sprintf("%d. %s (degree %d)", i+1, gn.Label, gn.Degree))
		}
		return textResult(strings.Join(lines, "\n")), nil, nil
	})

	// surprising_connections
	type surprisingArgs struct {
		TopN int `json:"top_n,omitempty" jsonschema:"number of connections to return (default 5)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "surprising_connections",
		Description: "Find the most surprising cross-file and cross-community edges ranked by anomaly score",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args surprisingArgs) (*mcp.CallToolResult, any, error) {
		n := args.TopN
		if n <= 0 {
			n = 5
		}
		conns := analyze.SurprisingConnections(g, communities, n)
		if len(conns) == 0 {
			return textResult("No surprising connections found"), nil, nil
		}
		var lines []string
		for i, c := range conns {
			lines = append(lines, fmt.Sprintf("%d. %s -[%s]-> %s (%s: %s)",
				i+1, c.Source, c.Relation, c.Target, c.Confidence, c.Why))
		}
		return textResult(strings.Join(lines, "\n")), nil, nil
	})

	// suggest_questions
	type suggestArgs struct {
		TopN int `json:"top_n,omitempty" jsonschema:"number of questions to return (default 5)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "suggest_questions",
		Description: "Generate investigation questions based on graph anomalies (ambiguous edges, isolated nodes)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args suggestArgs) (*mcp.CallToolResult, any, error) {
		n := args.TopN
		if n <= 0 {
			n = 5
		}
		questions := analyze.SuggestQuestions(g, communities, n)
		if len(questions) == 0 {
			return textResult("No questions to suggest"), nil, nil
		}
		var lines []string
		for i, q := range questions {
			lines = append(lines, fmt.Sprintf("%d. [%s] %s\n   Why: %s", i+1, q.Type, q.Question, q.Why))
		}
		return textResult(strings.Join(lines, "\n")), nil, nil
	})

	// community_info: merged get_community + community_cohesion
	type communityInfoArgs struct {
		CommunityID int `json:"community_id,omitempty" jsonschema:"community ID to inspect (-1 or omit to list all communities with cohesion scores)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "community_info",
		Description: "List all communities with cohesion scores, or inspect a single community's members",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args communityInfoArgs) (*mcp.CallToolResult, any, error) {
		if args.CommunityID > 0 {
			nodes, ok := communities[args.CommunityID]
			if !ok {
				return textResult(fmt.Sprintf("Community %d not found", args.CommunityID)), nil, nil
			}
			score := cluster.CohesionScore(g, nodes)
			var lines []string
			for _, nid := range nodes {
				attrs := g.NodeAttrs(nid)
				lines = append(lines, fmt.Sprintf("  - %s (%s) [degree %d]",
					attrStr(attrs, "label"), attrStr(attrs, "file_type"), g.Degree(nid)))
			}
			return textResult(fmt.Sprintf("Community %d (%d nodes, cohesion %.2f):\n%s",
				args.CommunityID, len(nodes), score, strings.Join(lines, "\n"))), nil, nil
		}
		// List all communities.
		scores := cluster.ScoreAll(g, communities)
		var cids []int
		for cid := range communities {
			cids = append(cids, cid)
		}
		sort.Ints(cids)
		var lines []string
		for _, cid := range cids {
			lines = append(lines, fmt.Sprintf("Community %d: %d nodes, cohesion %.2f",
				cid, len(communities[cid]), scores[cid]))
		}
		if len(lines) == 0 {
			return textResult("No communities found"), nil, nil
		}
		return textResult(fmt.Sprintf("Communities (%d):\n%s", len(cids), strings.Join(lines, "\n"))), nil, nil
	})

	// trace_calls
	type traceCallsArgs struct {
		Tag        string `json:"tag" jsonschema:"behavioral tag: throws catches logs fs net exec async unsafe test"`
		MaxDepth   int    `json:"max_depth,omitempty" jsonschema:"max call chain depth (default 10)"`
		MaxResults int    `json:"max_results,omitempty" jsonschema:"max chains to return (default 20)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "trace_calls",
		Description: "Trace call chains leading to functions with a behavioral tag (throws, logs, fs, net, exec, async, unsafe, test, catches)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args traceCallsArgs) (*mcp.CallToolResult, any, error) {
		chains := trace.TraceTag(g, args.Tag, args.MaxDepth, args.MaxResults)
		if len(chains) == 0 {
			return textResult(fmt.Sprintf("No call chains found for tag: %s", args.Tag)), nil, nil
		}
		var lines []string
		for i, chain := range chains {
			var labels []string
			for _, node := range chain.Path {
				labels = append(labels, node.Label)
			}
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, strings.Join(labels, " → ")))
		}
		return textResult(fmt.Sprintf("Call chains to [%s] (%d found):\n%s",
			args.Tag, len(chains), strings.Join(lines, "\n"))), nil, nil
	})
}

// --- Helpers ---

// childEntry records a containment child with the edge relation.
type childEntry struct {
	id       string
	relation string
}

// buildContainmentMaps builds parent→children and child→parents maps from contains/method edges.
func buildContainmentMaps(g *graph.Graph) (children, parents map[string][]childEntry) {
	children = make(map[string][]childEntry)
	parents = make(map[string][]childEntry)
	for _, e := range g.Edges() {
		rel := attrStr(e.Attrs, "relation")
		if rel != "contains" && rel != "method" {
			continue
		}
		// Use _src/_tgt for direction.
		src := attrStr(e.Attrs, "_src")
		tgt := attrStr(e.Attrs, "_tgt")
		if src == "" || tgt == "" {
			src = e.Source
			tgt = e.Target
		}
		children[src] = append(children[src], childEntry{id: tgt, relation: rel})
		parents[tgt] = append(parents[tgt], childEntry{id: src, relation: rel})
	}
	return children, parents
}

func formatNode(g *graph.Graph, id string, children, parents map[string][]childEntry) string {
	attrs := g.NodeAttrs(id)
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\n", id)
	fmt.Fprintf(&b, "Label: %s\n", attrStr(attrs, "label"))
	fmt.Fprintf(&b, "Type: %s\n", attrStr(attrs, "file_type"))
	fmt.Fprintf(&b, "Source: %s", attrStr(attrs, "source_file"))
	if loc := attrStr(attrs, "source_location"); loc != "" {
		fmt.Fprintf(&b, " %s", loc)
	}
	fmt.Fprintf(&b, "\nDegree: %d\n", g.Degree(id))

	if tags := attrTags(attrs); len(tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(tags, ", "))
	}
	if comment := attrStr(attrs, "comment"); comment != "" {
		fmt.Fprintf(&b, "Comment: %s\n", comment)
	}

	if p := parents[id]; len(p) > 0 {
		var pLabels []string
		for _, pe := range p {
			pLabels = append(pLabels, attrStr(g.NodeAttrs(pe.id), "label"))
		}
		fmt.Fprintf(&b, "Parent: %s\n", strings.Join(pLabels, ", "))
	}
	if c := children[id]; len(c) > 0 {
		fmt.Fprintf(&b, "Children: %d\n", len(c))
	}
	return b.String()
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func attrStr(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func attrTags(attrs map[string]any) []string {
	tags, ok := attrs["tags"]
	if !ok {
		return nil
	}
	switch t := tags.(type) {
	case []string:
		return t
	case []any:
		var out []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func hasTag(attrs map[string]any, tag string) bool {
	for _, t := range attrTags(attrs) {
		if t == tag {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// subgraphToText renders a subgraph as concise text.
func subgraphToText(g *graph.Graph, nodeIDs []string, edges []graph.EdgeData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Subgraph: %d nodes, %d edges\n\n", len(nodeIDs), len(edges))
	b.WriteString("Nodes:\n")
	for _, id := range nodeIDs {
		attrs := g.NodeAttrs(id)
		fmt.Fprintf(&b, "  - %s (%s)\n", attrStr(attrs, "label"), attrStr(attrs, "file_type"))
	}
	if len(edges) > 0 {
		b.WriteString("\nEdges:\n")
		for _, e := range edges {
			srcLabel := attrStr(g.NodeAttrs(e.Source), "label")
			tgtLabel := attrStr(g.NodeAttrs(e.Target), "label")
			rel := attrStr(e.Attrs, "relation")
			fmt.Fprintf(&b, "  - %s -[%s]-> %s\n", srcLabel, rel, tgtLabel)
		}
	}
	return b.String()
}
