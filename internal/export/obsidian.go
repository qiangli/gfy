package export

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/qiangli/gfy/internal/graph"
)

var unsafeNameRe = regexp.MustCompile(`[\\/*?:"<>|#^\[\]]`)

func safeName(label string) string {
	cleaned := strings.ReplaceAll(label, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	cleaned = unsafeNameRe.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)
	for _, ext := range []string{".md", ".mdx", ".markdown"} {
		if strings.HasSuffix(strings.ToLower(cleaned), ext) {
			cleaned = cleaned[:len(cleaned)-len(ext)]
		}
	}
	if cleaned == "" {
		cleaned = "unnamed"
	}
	return cleaned
}

// ToObsidian generates an Obsidian vault with one markdown file per node and
// community overview pages.
func ToObsidian(g *graph.Graph, communities map[int][]string, communityLabels map[int]string, cohesionScores map[int]float64, outputPath string) error {
	if err := os.MkdirAll(outputPath, 0o755); err != nil {
		return err
	}

	nodeCommunity := nodeCommunityMap(communities)
	usedNames := make(map[string]int)

	// Write one file per node.
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		label := attrStr(attrs, "label", id)
		sourceFile := attrStr(attrs, "source_file", "")
		fileType := attrStr(attrs, "file_type", "")
		sourceLoc := attrStr(attrs, "source_location", "")
		cid := nodeCommunity[id]
		commLabel := fmt.Sprintf("Community %d", cid)
		if l, ok := communityLabels[cid]; ok {
			commLabel = l
		}

		// Deduplicate filenames.
		name := safeName(label)
		if count, ok := usedNames[strings.ToLower(name)]; ok {
			usedNames[strings.ToLower(name)] = count + 1
			name = fmt.Sprintf("%s_%d", name, count+1)
		} else {
			usedNames[strings.ToLower(name)] = 1
		}

		// Connections.
		var connections []string
		for _, nb := range g.Neighbors(id) {
			nbAttrs := g.NodeAttrs(nb)
			nbLabel := attrStr(nbAttrs, "label", nb)
			eAttrs := g.EdgeAttrs(id, nb)
			relation := attrStr(eAttrs, "relation", "related")
			confidence := attrStr(eAttrs, "confidence", "EXTRACTED")
			connections = append(connections, fmt.Sprintf("- [[%s]] - `%s` [%s]", safeName(nbLabel), relation, confidence))
		}

		// Tags.
		ftypeTag := "graphify/document"
		switch fileType {
		case "code":
			ftypeTag = "graphify/code"
		case "paper":
			ftypeTag = "graphify/paper"
		case "image":
			ftypeTag = "graphify/image"
		}
		commTag := "community/" + strings.ReplaceAll(safeName(commLabel), " ", "_")

		var b strings.Builder
		b.WriteString("---\n")
		if sourceFile != "" {
			fmt.Fprintf(&b, "source_file: %q\n", sourceFile)
		}
		if fileType != "" {
			fmt.Fprintf(&b, "type: %q\n", fileType)
		}
		fmt.Fprintf(&b, "community: %q\n", commLabel)
		if sourceLoc != "" {
			fmt.Fprintf(&b, "location: %q\n", sourceLoc)
		}
		fmt.Fprintf(&b, "tags:\n  - %s\n  - %s\n", ftypeTag, commTag)
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "# %s\n\n", label)

		if len(connections) > 0 {
			b.WriteString("## Connections\n")
			for _, c := range connections {
				b.WriteString(c + "\n")
			}
			b.WriteString("\n")
		}

		nodePath := filepath.Join(outputPath, name+".md")
		if err := os.WriteFile(nodePath, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}

	// Write community overview files.
	cids := make([]int, 0, len(communities))
	for cid := range communities {
		cids = append(cids, cid)
	}
	sort.Ints(cids)

	for _, cid := range cids {
		nodes := communities[cid]
		commLabel := fmt.Sprintf("Community %d", cid)
		if l, ok := communityLabels[cid]; ok {
			commLabel = l
		}
		cohesion := cohesionScores[cid]
		cohDesc := "loosely connected"
		if cohesion >= 0.7 {
			cohDesc = "tightly connected"
		} else if cohesion >= 0.4 {
			cohDesc = "moderately connected"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "---\ntype: community\ncohesion: %.2f\nmembers: %d\n---\n\n", cohesion, len(nodes))
		fmt.Fprintf(&b, "# %s\n\n", commLabel)
		fmt.Fprintf(&b, "**Cohesion:** %.2f — %s\n", cohesion, cohDesc)
		fmt.Fprintf(&b, "**Members:** %d nodes\n\n", len(nodes))
		b.WriteString("## Members\n")
		for _, nid := range nodes {
			attrs := g.NodeAttrs(nid)
			label := attrStr(attrs, "label", nid)
			fileType := attrStr(attrs, "file_type", "")
			sourceFile := attrStr(attrs, "source_file", "")
			fmt.Fprintf(&b, "- [[%s]] — %s — %s\n", safeName(label), fileType, sourceFile)
		}
		b.WriteString("\n")

		commPath := filepath.Join(outputPath, fmt.Sprintf("_COMMUNITY_%s.md", safeName(commLabel)))
		if err := os.WriteFile(commPath, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}

	return nil
}
