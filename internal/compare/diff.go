package compare

import (
	"fmt"
	"sort"

	"github.com/qiangli/gfy/internal/graph"
)

// diffNodes computes added, removed, and modified nodes between two graphs.
func diffNodes(a, b *graph.Graph) NodeDiff {
	aNodes := make(map[string]bool)
	for _, id := range a.Nodes() {
		aNodes[id] = true
	}
	bNodes := make(map[string]bool)
	for _, id := range b.Nodes() {
		bNodes[id] = true
	}

	var diff NodeDiff

	// Removed: in A but not B.
	for id := range aNodes {
		if !bNodes[id] {
			diff.Removed = append(diff.Removed, nodeInfoFrom(a, id))
		}
	}

	// Added: in B but not A.
	for id := range bNodes {
		if !aNodes[id] {
			diff.Added = append(diff.Added, nodeInfoFrom(b, id))
		}
	}

	// Modified: in both, but with changed attributes.
	for id := range aNodes {
		if !bNodes[id] {
			continue
		}
		changes := diffAttrs(a.NodeAttrs(id), b.NodeAttrs(id))
		if len(changes) > 0 || a.Degree(id) != b.Degree(id) {
			diff.Modified = append(diff.Modified, NodeModification{
				ID:        id,
				Label:     attrStr(b.NodeAttrs(id), "label"),
				Changes:   changes,
				DegreeOld: a.Degree(id),
				DegreeNew: b.Degree(id),
			})
		}
	}

	sort.Slice(diff.Added, func(i, j int) bool { return diff.Added[i].ID < diff.Added[j].ID })
	sort.Slice(diff.Removed, func(i, j int) bool { return diff.Removed[i].ID < diff.Removed[j].ID })
	sort.Slice(diff.Modified, func(i, j int) bool { return diff.Modified[i].ID < diff.Modified[j].ID })

	return diff
}

// edgeKey uniquely identifies an edge by its endpoints and relation type.
type edgeKey struct {
	source, target, relation string
}

func makeEdgeKey(e graph.EdgeData) edgeKey {
	rel, _ := e.Attrs["relation"].(string)
	src, tgt := e.Source, e.Target
	// Normalize undirected edge order.
	if src > tgt {
		src, tgt = tgt, src
	}
	return edgeKey{src, tgt, rel}
}

// diffEdges computes added, removed, and modified edges between two graphs.
func diffEdges(a, b *graph.Graph) EdgeDiff {
	aEdges := make(map[edgeKey]graph.EdgeData)
	for _, e := range a.Edges() {
		aEdges[makeEdgeKey(e)] = e
	}
	bEdges := make(map[edgeKey]graph.EdgeData)
	for _, e := range b.Edges() {
		bEdges[makeEdgeKey(e)] = e
	}

	var diff EdgeDiff

	// Removed edges.
	for k, e := range aEdges {
		if _, ok := bEdges[k]; !ok {
			diff.Removed = append(diff.Removed, edgeInfoFrom(e))
		}
	}

	// Added edges.
	for k, e := range bEdges {
		if _, ok := aEdges[k]; !ok {
			diff.Added = append(diff.Added, edgeInfoFrom(e))
		}
	}

	// Modified edges (same key, different attributes).
	for k, ea := range aEdges {
		eb, ok := bEdges[k]
		if !ok {
			continue
		}
		changes := diffEdgeAttrs(ea.Attrs, eb.Attrs)
		if len(changes) > 0 {
			rel, _ := ea.Attrs["relation"].(string)
			diff.Modified = append(diff.Modified, EdgeModification{
				Source:   k.source,
				Target:   k.target,
				Relation: rel,
				Changes:  changes,
			})
		}
	}

	sort.Slice(diff.Added, func(i, j int) bool { return edgeInfoKey(diff.Added[i]) < edgeInfoKey(diff.Added[j]) })
	sort.Slice(diff.Removed, func(i, j int) bool { return edgeInfoKey(diff.Removed[i]) < edgeInfoKey(diff.Removed[j]) })
	sort.Slice(diff.Modified, func(i, j int) bool {
		ki := diff.Modified[i].Source + "|" + diff.Modified[i].Target + "|" + diff.Modified[i].Relation
		kj := diff.Modified[j].Source + "|" + diff.Modified[j].Target + "|" + diff.Modified[j].Relation
		return ki < kj
	})

	return diff
}

func nodeInfoFrom(g *graph.Graph, id string) NodeInfo {
	attrs := g.NodeAttrs(id)
	return NodeInfo{
		ID:       id,
		Label:    attrStr(attrs, "label"),
		FileType: attrStr(attrs, "file_type"),
		File:     attrStr(attrs, "source_file"),
		Degree:   g.Degree(id),
	}
}

func edgeInfoFrom(e graph.EdgeData) EdgeInfo {
	return EdgeInfo{
		Source:     e.Source,
		Target:     e.Target,
		Relation:   attrStr(e.Attrs, "relation"),
		Confidence: attrStr(e.Attrs, "confidence"),
	}
}

func edgeInfoKey(e EdgeInfo) string {
	return e.Source + "|" + e.Target + "|" + e.Relation
}

// diffAttrs compares two attribute maps and returns fields that changed.
// Skips "id" (identity field) and transient fields.
func diffAttrs(a, b map[string]any) map[string][2]any {
	skip := map[string]bool{"id": true, "source_location": true}
	changes := make(map[string][2]any)

	allKeys := make(map[string]bool)
	for k := range a {
		allKeys[k] = true
	}
	for k := range b {
		allKeys[k] = true
	}

	for k := range allKeys {
		if skip[k] {
			continue
		}
		va, vb := a[k], b[k]
		if !attrEqual(va, vb) {
			changes[k] = [2]any{va, vb}
		}
	}
	return changes
}

// diffEdgeAttrs compares edge attributes, skipping structural fields.
func diffEdgeAttrs(a, b map[string]any) map[string][2]any {
	skip := map[string]bool{
		"source": true, "target": true, "relation": true,
		"_src": true, "_tgt": true, "source_location": true,
	}
	changes := make(map[string][2]any)

	allKeys := make(map[string]bool)
	for k := range a {
		allKeys[k] = true
	}
	for k := range b {
		allKeys[k] = true
	}

	for k := range allKeys {
		if skip[k] {
			continue
		}
		va, vb := a[k], b[k]
		if !attrEqual(va, vb) {
			changes[k] = [2]any{va, vb}
		}
	}
	return changes
}

func attrStr(attrs map[string]any, key string) string {
	v, _ := attrs[key].(string)
	return v
}

func attrEqual(a, b any) bool {
	// Handle nil cases.
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Compare string slices.
	if as, ok := a.([]any); ok {
		bs, ok2 := b.([]any)
		if !ok2 || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if fmt.Sprint(as[i]) != fmt.Sprint(bs[i]) {
				return false
			}
		}
		return true
	}
	// Compare string slices (typed).
	if as, ok := a.([]string); ok {
		bs, ok2 := b.([]string)
		if !ok2 || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if as[i] != bs[i] {
				return false
			}
		}
		return true
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
