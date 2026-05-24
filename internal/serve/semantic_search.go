package serve

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qiangli/gfy/internal/semantic"
	"github.com/qiangli/gfy/pkg/graph"
	"github.com/qiangli/gfy/pkg/search"
)

// registerSemanticSearch adds the `semantic_search` MCP tool, which embeds the
// query through Ollama and ranks nodes by cosine similarity against the
// embeddings sidecar built at `gfy build` time.
func registerSemanticSearch(server *mcp.Server, g *graph.Graph, store *search.EmbeddingStore, ollamaURL string) {
	type semanticArgs struct {
		Query string `json:"query" jsonschema:"natural-language query to embed and match against node vectors"`
		Limit int    `json:"limit,omitempty" jsonschema:"max results (default 10)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name: "semantic_search",
		Description: "Embedding-based semantic search over node labels, comments, and document content. " +
			"Better than `search` for concept queries (e.g. \"rate limiting\", \"how does auth work\"). " +
			"Requires that the graph was built with embeddings enabled.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args semanticArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Query) == "" {
			return textResult("Query is empty"), nil, nil
		}
		client := &semantic.Client{BaseURL: ollamaURL}
		vec, err := client.Embed(store.Model, args.Query)
		if err != nil {
			return textResult(fmt.Sprintf("Embed failed: %v", err)), nil, nil
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		}
		results := search.RankByEmbedding(store, vec, limit)
		if len(results) == 0 {
			return textResult("No semantic matches found for: " + args.Query), nil, nil
		}
		var lines []string
		for i, r := range results {
			attrs := g.NodeAttrs(r.ID)
			lines = append(lines, fmt.Sprintf("%d. %s (%s, degree %d) [cos %.3f]",
				i+1, attrStr(attrs, "label"), attrStr(attrs, "source_file"), g.Degree(r.ID), r.Score))
		}
		return textResult(fmt.Sprintf("Semantic results for %q (model=%s):\n%s",
			args.Query, store.Model, strings.Join(lines, "\n"))), nil, nil
	})
}
