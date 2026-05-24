package export

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/qiangli/gfy/pkg/graph"
)

// MermaidOptions configures the Mermaid call-flow exporter.
type MermaidOptions struct {
	// Direction is the flowchart layout: "TD" (top-down, default), "LR"
	// (left-right), "BT", or "RL".
	Direction string

	// MaxNodes caps how many nodes appear in the diagram. Mermaid renderers
	// (live editor, GitHub) start to choke beyond ~300 nodes. When the call
	// graph is larger, we keep the highest-degree calls subgraph. Default 200.
	MaxNodes int

	// GroupByCommunity wraps each Louvain community in a `subgraph` block.
	// Requires the communities map; otherwise ignored.
	GroupByCommunity bool

	// HighlightTags emits Mermaid classDef styling for behavioural tags
	// (throws, fs, net, exec, ...). Tagged nodes get a `:::tag` suffix.
	HighlightTags bool
}

func defaultMermaidOptions(opts MermaidOptions) MermaidOptions {
	if opts.Direction == "" {
		opts.Direction = "TD"
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 200
	}
	return opts
}

// ToMermaid writes a Mermaid flowchart of the call graph (relation=calls edges
// only) to outputPath. The output is a standalone markdown file with a single
// fenced ` ```mermaid ` block so it renders inline in GitHub, Obsidian, and
// most markdown viewers.
//
// Pass communities/communityLabels (may be nil) to wrap nodes in subgraph
// blocks when opts.GroupByCommunity is true.
func ToMermaid(
	g *graph.Graph,
	communities map[int][]string,
	communityLabels map[int]string,
	outputPath string,
	opts MermaidOptions,
) error {
	opts = defaultMermaidOptions(opts)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Collect call edges (the diagram is specifically a call-flow view).
	type callEdge struct{ src, tgt string }
	var callEdges []callEdge
	for _, e := range g.Edges() {
		if attrStr(e.Attrs, "relation", "") != "calls" {
			continue
		}
		src := attrStr(e.Attrs, "_src", e.Source)
		tgt := attrStr(e.Attrs, "_tgt", e.Target)
		callEdges = append(callEdges, callEdge{src, tgt})
	}

	// Pick the nodes touched by call edges; if more than MaxNodes, prune by
	// keeping the highest-degree subset (preserves hubs that are usually the
	// most informative). The subset becomes a deterministic node set so output
	// is stable across runs.
	touched := make(map[string]int)
	for _, e := range callEdges {
		touched[e.src]++
		touched[e.tgt]++
	}
	nodeSet := make(map[string]bool, len(touched))
	if len(touched) <= opts.MaxNodes {
		for id := range touched {
			nodeSet[id] = true
		}
	} else {
		// Sort by frequency desc, then ID asc for determinism.
		type entry struct {
			id    string
			count int
		}
		entries := make([]entry, 0, len(touched))
		for id, c := range touched {
			entries = append(entries, entry{id, c})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].count != entries[j].count {
				return entries[i].count > entries[j].count
			}
			return entries[i].id < entries[j].id
		})
		for i := 0; i < opts.MaxNodes && i < len(entries); i++ {
			nodeSet[entries[i].id] = true
		}
	}

	// Filter edges to those whose endpoints both survived pruning.
	visible := callEdges[:0]
	for _, e := range callEdges {
		if nodeSet[e.src] && nodeSet[e.tgt] {
			visible = append(visible, e)
		}
	}

	var b strings.Builder
	b.WriteString("# Call Flow\n\n")
	if len(touched) > opts.MaxNodes {
		fmt.Fprintf(&b, "_Graph trimmed: showing top %d of %d call-graph nodes by degree._\n\n",
			opts.MaxNodes, len(touched))
	}
	b.WriteString("```mermaid\n")
	fmt.Fprintf(&b, "flowchart %s\n", opts.Direction)

	// Node declarations. Sorted for determinism.
	nodeIDs := make([]string, 0, len(nodeSet))
	for id := range nodeSet {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	// Determine which tags we'll actually need to declare classDefs for, so
	// the styling block at the end stays tight.
	usedTags := make(map[string]bool)

	if opts.GroupByCommunity && len(communities) > 0 {
		emitCommunityGrouped(&b, g, nodeIDs, communities, communityLabels, opts, usedTags)
	} else {
		for _, id := range nodeIDs {
			emitNode(&b, g, id, "  ", opts, usedTags)
		}
	}

	// Edge declarations.
	b.WriteString("\n")
	sort.Slice(visible, func(i, j int) bool {
		if visible[i].src != visible[j].src {
			return visible[i].src < visible[j].src
		}
		return visible[i].tgt < visible[j].tgt
	})
	for _, e := range visible {
		fmt.Fprintf(&b, "  %s --> %s\n", mermaidID(e.src), mermaidID(e.tgt))
	}

	// Styling for behavioural tags.
	if opts.HighlightTags && len(usedTags) > 0 {
		b.WriteString("\n")
		for _, tag := range sortedKeys(usedTags) {
			fmt.Fprintf(&b, "  classDef %s %s\n", tag, classDefForTag(tag))
		}
	}

	b.WriteString("```\n")
	return os.WriteFile(outputPath, []byte(b.String()), 0o644)
}

func emitCommunityGrouped(
	b *strings.Builder,
	g *graph.Graph,
	nodeIDs []string,
	communities map[int][]string,
	communityLabels map[int]string,
	opts MermaidOptions,
	usedTags map[string]bool,
) {
	nodeToCommunity := nodeCommunityMap(communities)

	// Group visible nodes by community ID.
	groups := make(map[int][]string)
	var ungrouped []string
	for _, id := range nodeIDs {
		cid, ok := nodeToCommunity[id]
		if !ok {
			ungrouped = append(ungrouped, id)
			continue
		}
		groups[cid] = append(groups[cid], id)
	}

	// Stable ordering by community ID.
	cids := make([]int, 0, len(groups))
	for cid := range groups {
		cids = append(cids, cid)
	}
	sort.Ints(cids)

	for _, cid := range cids {
		label := communityLabels[cid]
		if label == "" {
			label = fmt.Sprintf("Community %d", cid)
		}
		fmt.Fprintf(b, "  subgraph c%d [\"%s\"]\n", cid, mermaidEscapeLabel(label))
		for _, id := range groups[cid] {
			emitNode(b, g, id, "    ", opts, usedTags)
		}
		b.WriteString("  end\n")
	}
	for _, id := range ungrouped {
		emitNode(b, g, id, "  ", opts, usedTags)
	}
}

func emitNode(
	b *strings.Builder,
	g *graph.Graph,
	id, indent string,
	opts MermaidOptions,
	usedTags map[string]bool,
) {
	attrs := g.NodeAttrs(id)
	label := attrStr(attrs, "label", id)

	nodeRef := mermaidID(id)
	fmt.Fprintf(b, "%s%s[\"%s\"]", indent, nodeRef, mermaidEscapeLabel(label))

	if opts.HighlightTags {
		// Pick the most informative tag for styling. Behavioural tags ordered
		// by priority — the first match wins so users see one consistent
		// colour per node.
		if tag := pickStylableTag(attrs); tag != "" {
			fmt.Fprintf(b, ":::%s", tag)
			usedTags[tag] = true
		}
	}
	b.WriteString("\n")
}

// pickStylableTag returns the highest-priority behavioural tag on a node, or
// "" if none qualify. Order is severity-ish: throws/exec/unsafe surface
// risk, fs/net surface side effects, the rest are informational.
var stylableTagPriority = []string{"throws", "exec", "unsafe", "net", "fs", "logs", "async", "test"}

func pickStylableTag(attrs map[string]any) string {
	tags := attrStrSlice(attrs, "tags")
	if len(tags) == 0 {
		return ""
	}
	have := make(map[string]bool, len(tags))
	for _, t := range tags {
		have[t] = true
	}
	for _, t := range stylableTagPriority {
		if have[t] {
			return t
		}
	}
	return ""
}

func classDefForTag(tag string) string {
	switch tag {
	case "throws":
		return "fill:#fee2e2,stroke:#dc2626,color:#7f1d1d;"
	case "exec":
		return "fill:#fef3c7,stroke:#d97706,color:#78350f;"
	case "unsafe":
		return "fill:#fed7aa,stroke:#ea580c,color:#7c2d12;"
	case "net":
		return "fill:#dbeafe,stroke:#2563eb,color:#1e3a8a;"
	case "fs":
		return "fill:#e0e7ff,stroke:#4f46e5,color:#312e81;"
	case "logs":
		return "fill:#f3f4f6,stroke:#6b7280,color:#1f2937;"
	case "async":
		return "fill:#ede9fe,stroke:#7c3aed,color:#4c1d95;"
	case "test":
		return "fill:#dcfce7,stroke:#16a34a,color:#14532d;"
	}
	return "fill:#f1f5f9,stroke:#475569;"
}

// mermaidIDPattern matches characters Mermaid accepts in a node identifier.
// Anything else is replaced with underscore to keep the ID renderer-safe.
var mermaidIDPattern = regexp.MustCompile(`[^A-Za-z0-9_]`)

func mermaidID(id string) string {
	clean := mermaidIDPattern.ReplaceAllString(id, "_")
	if clean == "" {
		return "n_empty"
	}
	if clean[0] >= '0' && clean[0] <= '9' {
		clean = "n_" + clean
	}
	return clean
}

// mermaidEscapeLabel makes a label safe to wrap in double quotes inside a
// Mermaid node declaration. We escape backslashes and double quotes, and
// collapse newlines (Mermaid does support <br/> but it adds noise).
func mermaidEscapeLabel(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
