// Package serve implements an MCP stdio server for querying the knowledge graph.
package serve

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qiangli/gfy/internal/analyze"
	"github.com/qiangli/gfy/internal/graph"
	"github.com/qiangli/gfy/internal/search"
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
	// query_graph
	type queryArgs struct {
		Question string `json:"question" jsonschema:"description=search terms to match against node labels"`
		Depth    int    `json:"depth,omitempty" jsonschema:"description=BFS traversal depth (default 2)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_graph",
		Description: "Search the knowledge graph by keyword and return a subgraph context",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArgs) (*mcp.CallToolResult, any, error) {
		depth := args.Depth
		if depth <= 0 {
			depth = 2
		}
		results := search.ScoreNodes(g, args.Question)
		if len(results) > 5 {
			results = results[:5]
		}
		startNodes := make([]string, len(results))
		for i, r := range results {
			startNodes[i] = r.ID
		}
		visited, edges := g.BFS(startNodes, depth)
		text := subgraphToText(g, visited, edges)
		return textResult(text), nil, nil
	})

	// get_node
	type getNodeArgs struct {
		Label string `json:"label" jsonschema:"description=node label or ID to look up"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_node",
		Description: "Look up a node by label or ID",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getNodeArgs) (*mcp.CallToolResult, any, error) {
		id := search.FindNode(g, args.Label)
		if id == "" {
			return textResult("Node not found: " + args.Label), nil, nil
		}
		attrs := g.NodeAttrs(id)
		text := fmt.Sprintf("ID: %s\nLabel: %s\nType: %s\nSource: %s\nDegree: %d",
			id, attrStr(attrs, "label"), attrStr(attrs, "file_type"),
			attrStr(attrs, "source_file"), g.Degree(id))
		return textResult(text), nil, nil
	})

	// get_neighbors
	type getNeighborsArgs struct {
		Label          string `json:"label" jsonschema:"description=node label or ID"`
		RelationFilter string `json:"relation_filter,omitempty" jsonschema:"description=filter by relation type"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_neighbors",
		Description: "Return direct neighbors of a node with edge metadata",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getNeighborsArgs) (*mcp.CallToolResult, any, error) {
		id := search.FindNode(g, args.Label)
		if id == "" {
			return textResult("Node not found: " + args.Label), nil, nil
		}
		var lines []string
		for _, nb := range g.Neighbors(id) {
			eAttrs := g.EdgeAttrs(id, nb)
			rel := attrStr(eAttrs, "relation")
			if args.RelationFilter != "" && rel != args.RelationFilter {
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

	// get_community
	type getCommunityArgs struct {
		CommunityID int `json:"community_id" jsonschema:"description=community ID to retrieve"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_community",
		Description: "Return all nodes in a community",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getCommunityArgs) (*mcp.CallToolResult, any, error) {
		nodes, ok := communities[args.CommunityID]
		if !ok {
			return textResult(fmt.Sprintf("Community %d not found", args.CommunityID)), nil, nil
		}
		var lines []string
		for _, nid := range nodes {
			attrs := g.NodeAttrs(nid)
			lines = append(lines, fmt.Sprintf("- %s (%s) [degree %d]",
				attrStr(attrs, "label"), attrStr(attrs, "file_type"), g.Degree(nid)))
		}
		return textResult(fmt.Sprintf("Community %d (%d nodes):\n%s",
			args.CommunityID, len(nodes), strings.Join(lines, "\n"))), nil, nil
	})

	// god_nodes
	type godNodesArgs struct {
		TopN int `json:"top_n,omitempty" jsonschema:"description=number of top nodes (default 10)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "god_nodes",
		Description: "Return the most connected entities in the graph",
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

	// graph_stats
	mcp.AddTool(server, &mcp.Tool{
		Name:        "graph_stats",
		Description: "Return node/edge counts, community count, confidence breakdown",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		confCounts := map[string]int{}
		for _, e := range g.Edges() {
			conf := attrStr(e.Attrs, "confidence")
			confCounts[conf]++
		}
		text := fmt.Sprintf("Nodes: %d\nEdges: %d\nCommunities: %d\nExtracted: %d\nInferred: %d\nAmbiguous: %d",
			g.NodeCount(), g.EdgeCount(), len(communities),
			confCounts["EXTRACTED"], confCounts["INFERRED"], confCounts["AMBIGUOUS"])
		return textResult(text), nil, nil
	})

	// shortest_path
	type shortestPathArgs struct {
		Source  string `json:"source" jsonschema:"description=source node label or ID"`
		Target  string `json:"target" jsonschema:"description=target node label or ID"`
		MaxHops int    `json:"max_hops,omitempty" jsonschema:"description=max path length (default unlimited)"`
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
			attrs := g.NodeAttrs(nid)
			labels = append(labels, attrStr(attrs, "label"))
		}
		return textResult(fmt.Sprintf("Path (%d hops): %s", len(path)-1, strings.Join(labels, " → "))), nil, nil
	})
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

// subgraphToText renders a subgraph as concise text.
func subgraphToText(g *graph.Graph, nodeIDs []string, edges []graph.EdgeData) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Subgraph: %d nodes, %d edges\n\n", len(nodeIDs), len(edges)))
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
