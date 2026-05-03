package compare

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/qiangli/gfy/internal/graph"
)

// NodeSignature captures the structural identity of a node independent of its
// label. Two nodes with different names but identical signatures represent the
// same structural role in their respective codebases.
type NodeSignature struct {
	// NodeType inferred from edge relations and label patterns.
	// Values: "file", "function", "method", "class", "import", "entity"
	NodeType string

	// Containment path: sorted list of container node types.
	// e.g., ["file"] for a top-level function, ["file", "class"] for a method.
	ContainerTypes []string

	// Behavioral profile: sorted tags (throws, logs, fs, net, etc.)
	Tags []string

	// Structural neighborhood: sorted list of (relation, neighbor_type) pairs.
	// This captures "calls 2 functions, contained by 1 file, imports 0".
	NeighborProfile []string

	// In-degree and out-degree by relation type.
	InDegreeByRel  map[string]int
	OutDegreeByRel map[string]int

	// Arity: number of outgoing "calls" edges (for functions).
	Arity int

	// Hash: deterministic fingerprint of the above fields.
	Hash string
}

// AlignmentResult holds the output of structural node alignment.
type AlignmentResult struct {
	// Matched pairs: nodeID in graph A → nodeID in graph B.
	Matched map[string]string

	// Match scores for each pair.
	Scores map[string]float64

	// Unmatched nodes from each graph.
	UnmatchedA []string
	UnmatchedB []string

	// Statistics.
	MatchedCount   int
	UnmatchedACount int
	UnmatchedBCount int
	AvgScore       float64
}

// NormalizeOptions configures the normalization/alignment behavior.
type NormalizeOptions struct {
	// MinMatchScore is the minimum signature similarity to consider a match.
	// Range [0,1]. Default: 0.4
	MinMatchScore float64

	// LabelWeight controls how much label similarity contributes (0 = pure
	// structural, 1 = label-only). Default: 0.2 (labels are weak signal).
	LabelWeight float64

	// UseWLRefinement enables Weisfeiler-Lehman iterative refinement of
	// signatures (captures multi-hop structure). Default: true
	UseWLRefinement bool

	// WLIterations is the number of WL refinement rounds. Default: 3
	WLIterations int
}

func (o NormalizeOptions) withDefaults() NormalizeOptions {
	if o.MinMatchScore == 0 {
		o.MinMatchScore = 0.4
	}
	if o.LabelWeight == 0 {
		o.LabelWeight = 0.2
	}
	if o.WLIterations == 0 {
		o.WLIterations = 3
		o.UseWLRefinement = true
	}
	return o
}

// dirEdge represents a directed edge endpoint with relation type.
type dirEdge struct {
	peer     string
	relation string
}

// BuildSignatures computes a structural signature for every node in the graph.
func BuildSignatures(g *graph.Graph) map[string]*NodeSignature {
	sigs := make(map[string]*NodeSignature)

	// Precompute directed edge maps using _src/_tgt attributes.
	// inEdges[node] = list of (source, relation)
	// outEdges[node] = list of (target, relation)
	inEdges := make(map[string][]dirEdge)
	outEdges := make(map[string][]dirEdge)
	for _, e := range g.Edges() {
		rel, _ := e.Attrs["relation"].(string)
		src, tgt := e.Source, e.Target
		if s, ok := e.Attrs["_src"].(string); ok {
			src = s
		}
		if t, ok := e.Attrs["_tgt"].(string); ok {
			tgt = t
		}
		outEdges[src] = append(outEdges[src], dirEdge{tgt, rel})
		inEdges[tgt] = append(inEdges[tgt], dirEdge{src, rel})
	}

	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		label, _ := attrs["label"].(string)

		sig := &NodeSignature{
			InDegreeByRel:  make(map[string]int),
			OutDegreeByRel: make(map[string]int),
		}

		// Infer node type from label pattern and edge relations.
		sig.NodeType = inferNodeType(label, inEdges[id], outEdges[id])

		// Container chain: follow incoming "contains"/"method" edges upward.
		sig.ContainerTypes = buildContainerChain(g, id, inEdges)

		// Tags.
		if tags, ok := attrs["tags"]; ok {
			sig.Tags = extractStringSlice(tags)
			sort.Strings(sig.Tags)
		}

		// Build neighbor profile: (relation, neighbor_type) pairs.
		var neighborProfile []string
		for _, e := range outEdges[id] {
			peerAttrs := g.NodeAttrs(e.peer)
			peerLabel, _ := peerAttrs["label"].(string)
			peerType := inferNodeType(peerLabel, inEdges[e.peer], outEdges[e.peer])
			neighborProfile = append(neighborProfile, fmt.Sprintf("out:%s:%s", e.relation, peerType))
			sig.OutDegreeByRel[e.relation]++
		}
		for _, e := range inEdges[id] {
			peerAttrs := g.NodeAttrs(e.peer)
			peerLabel, _ := peerAttrs["label"].(string)
			peerType := inferNodeType(peerLabel, inEdges[e.peer], outEdges[e.peer])
			neighborProfile = append(neighborProfile, fmt.Sprintf("in:%s:%s", e.relation, peerType))
			sig.InDegreeByRel[e.relation]++
		}
		sort.Strings(neighborProfile)
		sig.NeighborProfile = neighborProfile

		// Arity: outgoing calls.
		sig.Arity = sig.OutDegreeByRel["calls"]

		// Compute hash.
		sig.Hash = hashSignature(sig)

		sigs[id] = sig
	}

	return sigs
}

// WLRefine performs Weisfeiler-Lehman iterative refinement on signatures.
// Each iteration incorporates neighbor signature hashes, building progressively
// richer structural fingerprints. This captures multi-hop patterns:
// "this function calls a function that throws" gets a different hash from
// "this function calls a function that logs".
func WLRefine(g *graph.Graph, sigs map[string]*NodeSignature, iterations int) {
	// Current labels: start with the base signature hash.
	labels := make(map[string]string)
	for id, sig := range sigs {
		labels[id] = sig.Hash
	}

	for iter := 0; iter < iterations; iter++ {
		newLabels := make(map[string]string)
		for _, id := range g.Nodes() {
			// Collect sorted neighbor labels.
			neighbors := g.Neighbors(id)
			neighborLabels := make([]string, 0, len(neighbors))
			for _, nid := range neighbors {
				neighborLabels = append(neighborLabels, labels[nid])
			}
			sort.Strings(neighborLabels)

			// New label = hash(current_label + sorted_neighbor_labels).
			h := sha256.New()
			fmt.Fprintf(h, "%s|%s", labels[id], strings.Join(neighborLabels, ","))
			newLabels[id] = fmt.Sprintf("%x", h.Sum(nil))[:16]
		}
		labels = newLabels
	}

	// Update signature hashes with refined labels.
	for id, sig := range sigs {
		sig.Hash = labels[id]
	}
}

// AlignGraphs finds the best structural alignment between nodes in two graphs.
// It returns a mapping from graph A node IDs to graph B node IDs based on
// structural signature similarity, NOT label similarity.
//
// Algorithm: Two-phase approach
//  1. Block by node type (only match functions to functions, etc.)
//  2. Within each block, compute pairwise signature similarity
//  3. Greedy best-match assignment (Hungarian is O(n³) and overkill when
//     type-blocking keeps blocks small)
func AlignGraphs(a, b *graph.Graph, opts NormalizeOptions) *AlignmentResult {
	opts = opts.withDefaults()

	// Build and refine signatures.
	sigsA := BuildSignatures(a)
	sigsB := BuildSignatures(b)

	if opts.UseWLRefinement {
		WLRefine(a, sigsA, opts.WLIterations)
		WLRefine(b, sigsB, opts.WLIterations)
	}

	// Phase 1: Block by node type.
	blocksA := groupByType(sigsA)
	blocksB := groupByType(sigsB)

	result := &AlignmentResult{
		Matched: make(map[string]string),
		Scores:  make(map[string]float64),
	}

	// Phase 2: Within each type block, find best matches.
	allTypes := make(map[string]bool)
	for t := range blocksA {
		allTypes[t] = true
	}
	for t := range blocksB {
		allTypes[t] = true
	}

	usedB := make(map[string]bool)

	for nodeType := range allTypes {
		idsA := blocksA[nodeType]
		idsB := blocksB[nodeType]
		if len(idsA) == 0 || len(idsB) == 0 {
			continue
		}

		// Compute pairwise similarity matrix.
		type scoredPair struct {
			idA, idB string
			score    float64
		}
		var pairs []scoredPair

		for _, idA := range idsA {
			labA, _ := a.NodeAttrs(idA)["label"].(string)
			for _, idB := range idsB {
				// First check exact ID match — if IDs match, it's the same entity.
				if idA == idB {
					pairs = append(pairs, scoredPair{idA, idB, 1.0})
					continue
				}

				labB, _ := b.NodeAttrs(idB)["label"].(string)
				score := signatureSimilarity(sigsA[idA], sigsB[idB])

				// Blend in label similarity with low weight.
				if opts.LabelWeight > 0 && labA != "" && labB != "" {
					labSim := normalizedLabelSimilarity(labA, labB)
					score = (1-opts.LabelWeight)*score + opts.LabelWeight*labSim
				}

				if score >= opts.MinMatchScore {
					pairs = append(pairs, scoredPair{idA, idB, score})
				}
			}
		}

		// Greedy assignment: best scores first.
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].score > pairs[j].score
		})

		usedA := make(map[string]bool)
		for _, p := range pairs {
			if usedA[p.idA] || usedB[p.idB] {
				continue
			}
			result.Matched[p.idA] = p.idB
			result.Scores[p.idA] = p.score
			usedA[p.idA] = true
			usedB[p.idB] = true
		}
	}

	// Collect unmatched.
	for _, id := range a.Nodes() {
		if _, ok := result.Matched[id]; !ok {
			result.UnmatchedA = append(result.UnmatchedA, id)
		}
	}
	for _, id := range b.Nodes() {
		if !usedB[id] {
			result.UnmatchedB = append(result.UnmatchedB, id)
		}
	}

	result.MatchedCount = len(result.Matched)
	result.UnmatchedACount = len(result.UnmatchedA)
	result.UnmatchedBCount = len(result.UnmatchedB)

	if result.MatchedCount > 0 {
		totalScore := 0.0
		for _, s := range result.Scores {
			totalScore += s
		}
		result.AvgScore = totalScore / float64(result.MatchedCount)
	}

	return result
}

// ReKeyGraph creates a copy of graph B with node IDs remapped according to
// the alignment. Matched nodes get graph A's IDs; unmatched nodes keep their
// original IDs. This allows the standard diff/similarity algorithms to work
// on structurally aligned graphs.
func ReKeyGraph(b *graph.Graph, alignment *AlignmentResult) *graph.Graph {
	// Build reverse mapping: B id → A id.
	bToA := make(map[string]string)
	for idA, idB := range alignment.Matched {
		bToA[idB] = idA
	}

	rekeyed := graph.New(b.IsDirected())

	// Rekey nodes.
	for _, idB := range b.Nodes() {
		newID := idB
		if idA, ok := bToA[idB]; ok {
			newID = idA
		}
		rekeyed.AddNode(newID, b.NodeAttrs(idB))
	}

	// Rekey edges.
	for _, e := range b.Edges() {
		src, tgt := e.Source, e.Target
		if newSrc, ok := bToA[src]; ok {
			src = newSrc
		}
		if newTgt, ok := bToA[tgt]; ok {
			tgt = newTgt
		}
		// Also update _src/_tgt attributes if present.
		newAttrs := make(map[string]any, len(e.Attrs))
		for k, v := range e.Attrs {
			newAttrs[k] = v
		}
		if origSrc, ok := newAttrs["_src"].(string); ok {
			if newSrc, ok2 := bToA[origSrc]; ok2 {
				newAttrs["_src"] = newSrc
			}
		}
		if origTgt, ok := newAttrs["_tgt"].(string); ok {
			if newTgt, ok2 := bToA[origTgt]; ok2 {
				newAttrs["_tgt"] = newTgt
			}
		}
		rekeyed.AddEdge(src, tgt, newAttrs)
	}

	return rekeyed
}

// --- internal helpers ---

// inferNodeType determines the structural role of a node from its label
// pattern and edge relationships.
func inferNodeType(label string, inEdges, outEdges []dirEdge) string {
	// Check edge patterns first (most reliable).
	hasContainsIn := false
	hasContainsOut := false
	hasMethodIn := false
	hasImportsOut := false
	hasCallsOut := false

	for _, e := range inEdges {
		switch e.relation {
		case "contains":
			hasContainsIn = true
		case "method":
			hasMethodIn = true
		}
	}
	for _, e := range outEdges {
		switch e.relation {
		case "contains":
			hasContainsOut = true
		case "imports":
			hasImportsOut = true
		case "calls":
			hasCallsOut = true
		}
	}

	// Files: contain other nodes, may have imports.
	if hasContainsOut && (hasImportsOut || !hasContainsIn) {
		return "file"
	}

	// Methods: linked via "method" relation from a class/type.
	if hasMethodIn || (hasContainsIn && strings.HasPrefix(label, ".") && strings.HasSuffix(label, "()")) {
		return "method"
	}

	// Functions: label ends with "()", contained by a file.
	if strings.HasSuffix(label, "()") {
		return "function"
	}

	// Classes/types: have outgoing "method" edges or are containers.
	for _, e := range outEdges {
		if e.relation == "method" {
			return "class"
		}
	}

	// Imports: target of "imports" edges.
	for _, e := range inEdges {
		if e.relation == "imports" {
			return "import"
		}
	}

	// Fallback: check if it calls things (likely a function without () in label).
	if hasCallsOut || hasContainsIn {
		return "entity"
	}

	return "entity"
}


// buildContainerChain follows incoming "contains" and "method" edges upward
// to build a containment path.
func buildContainerChain(g *graph.Graph, id string, inEdges map[string][]dirEdge) []string {
	var chain []string
	visited := map[string]bool{id: true}
	current := id

	for {
		found := false
		for _, e := range inEdges[current] {
			if e.relation == "contains" || e.relation == "method" {
				if visited[e.peer] {
					continue
				}
				visited[e.peer] = true
				peerAttrs := g.NodeAttrs(e.peer)
				peerLabel, _ := peerAttrs["label"].(string)
				peerType := inferNodeType(peerLabel, inEdges[e.peer], nil)
				chain = append([]string{peerType}, chain...)
				current = e.peer
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return chain
}

// signatureSimilarity computes how similar two signatures are (0-1).
// Completely ignores labels — uses only structural features.
func signatureSimilarity(a, b *NodeSignature) float64 {
	if a.Hash == b.Hash {
		return 1.0
	}

	score := 0.0
	weights := 0.0

	// Node type must match (handled by blocking, but verify).
	if a.NodeType == b.NodeType {
		score += 1.0
	}
	weights += 1.0

	// Container chain similarity.
	chainSim := sliceJaccard(a.ContainerTypes, b.ContainerTypes)
	score += chainSim * 0.5
	weights += 0.5

	// Tag similarity.
	tagSim := sliceJaccard(a.Tags, b.Tags)
	score += tagSim * 1.5
	weights += 1.5

	// Neighbor profile similarity (the strongest structural signal).
	profileSim := sliceJaccard(a.NeighborProfile, b.NeighborProfile)
	score += profileSim * 3.0
	weights += 3.0

	// Degree profile similarity by relation type.
	degreeSim := degreeProfileSimilarity(a, b)
	score += degreeSim * 2.0
	weights += 2.0

	// Arity similarity (for functions).
	if a.NodeType == "function" || a.NodeType == "method" {
		aritySim := 1.0 - math.Abs(float64(a.Arity-b.Arity))/math.Max(float64(max(a.Arity, b.Arity)), 1.0)
		score += aritySim * 1.0
		weights += 1.0
	}

	if weights == 0 {
		return 0
	}
	return score / weights
}

// degreeProfileSimilarity compares the in/out degree distributions by relation type.
func degreeProfileSimilarity(a, b *NodeSignature) float64 {
	allRels := make(map[string]bool)
	for r := range a.InDegreeByRel {
		allRels[r] = true
	}
	for r := range a.OutDegreeByRel {
		allRels[r] = true
	}
	for r := range b.InDegreeByRel {
		allRels[r] = true
	}
	for r := range b.OutDegreeByRel {
		allRels[r] = true
	}

	if len(allRels) == 0 {
		return 1.0
	}

	total := 0.0
	count := 0.0
	for r := range allRels {
		aIn := float64(a.InDegreeByRel[r])
		bIn := float64(b.InDegreeByRel[r])
		aOut := float64(a.OutDegreeByRel[r])
		bOut := float64(b.OutDegreeByRel[r])

		// Ratio similarity: min/max for each direction.
		if aIn > 0 || bIn > 0 {
			total += math.Min(aIn, bIn) / math.Max(aIn, bIn)
			count++
		}
		if aOut > 0 || bOut > 0 {
			total += math.Min(aOut, bOut) / math.Max(aOut, bOut)
			count++
		}
	}

	if count == 0 {
		return 1.0
	}
	return total / count
}

// normalizedLabelSimilarity computes label similarity with canonicalization.
// Splits camelCase/snake_case into tokens, lowercases, and computes token Jaccard.
// This makes "MY_CONST" and "your_constant" somewhat similar via shared concept tokens.
func normalizedLabelSimilarity(a, b string) float64 {
	tokA := canonicalTokens(a)
	tokB := canonicalTokens(b)
	if len(tokA) == 0 && len(tokB) == 0 {
		return 1.0
	}
	return sliceJaccard(tokA, tokB)
}

// canonicalTokens splits a label into normalized tokens.
// "MY_CONST" → ["my", "const"]
// "calculateTotal" → ["calculate", "total"]
// ".processRequest()" → ["process", "request"]
func canonicalTokens(label string) []string {
	// Strip method/function markers.
	label = strings.TrimPrefix(label, ".")
	label = strings.TrimSuffix(label, "()")

	// Split on non-alphanumeric.
	parts := strings.FieldsFunc(label, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})

	var tokens []string
	for _, part := range parts {
		// Split camelCase.
		tokens = append(tokens, splitCamelTokens(part)...)
	}

	// Lowercase all.
	for i, t := range tokens {
		tokens[i] = strings.ToLower(t)
	}

	// Filter very short tokens.
	var filtered []string
	for _, t := range tokens {
		if len(t) >= 2 {
			filtered = append(filtered, t)
		}
	}

	sort.Strings(filtered)
	return filtered
}

// splitCamelTokens splits "calculateTotal" into ["calculate", "Total"].
func splitCamelTokens(s string) []string {
	var tokens []string
	start := 0
	runes := []rune(s)
	for i := 1; i < len(runes); i++ {
		if isUpper(runes[i]) && !isUpper(runes[i-1]) {
			tokens = append(tokens, string(runes[start:i]))
			start = i
		} else if isUpper(runes[i-1]) && isUpper(runes[i]) && i+1 < len(runes) && !isUpper(runes[i+1]) {
			// Handle "HTMLParser" → "HTML", "Parser"
			tokens = append(tokens, string(runes[start:i]))
			start = i
		}
	}
	tokens = append(tokens, string(runes[start:]))
	return tokens
}

func isUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

// hashSignature produces a deterministic hash of a signature's structural fields.
func hashSignature(sig *NodeSignature) string {
	h := sha256.New()
	fmt.Fprintf(h, "type:%s\n", sig.NodeType)
	fmt.Fprintf(h, "containers:%s\n", strings.Join(sig.ContainerTypes, ","))
	fmt.Fprintf(h, "tags:%s\n", strings.Join(sig.Tags, ","))
	fmt.Fprintf(h, "profile:%s\n", strings.Join(sig.NeighborProfile, ","))
	fmt.Fprintf(h, "arity:%d\n", sig.Arity)

	// Sorted degree profile.
	var degParts []string
	for r, c := range sig.InDegreeByRel {
		degParts = append(degParts, fmt.Sprintf("in:%s:%d", r, c))
	}
	for r, c := range sig.OutDegreeByRel {
		degParts = append(degParts, fmt.Sprintf("out:%s:%d", r, c))
	}
	sort.Strings(degParts)
	fmt.Fprintf(h, "degrees:%s\n", strings.Join(degParts, ","))

	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// groupByType partitions node IDs by their inferred type.
func groupByType(sigs map[string]*NodeSignature) map[string][]string {
	groups := make(map[string][]string)
	for id, sig := range sigs {
		groups[sig.NodeType] = append(groups[sig.NodeType], id)
	}
	// Sort within each group for determinism.
	for _, ids := range groups {
		sort.Strings(ids)
	}
	return groups
}

// sliceJaccard computes Jaccard similarity between two sorted string slices,
// treating them as multisets.
func sliceJaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	aMap := make(map[string]int)
	bMap := make(map[string]int)
	for _, s := range a {
		aMap[s]++
	}
	for _, s := range b {
		bMap[s]++
	}

	// Multiset intersection and union.
	intersection := 0
	union := 0
	allKeys := make(map[string]bool)
	for k := range aMap {
		allKeys[k] = true
	}
	for k := range bMap {
		allKeys[k] = true
	}
	for k := range allKeys {
		ca, cb := aMap[k], bMap[k]
		if ca < cb {
			intersection += ca
		} else {
			intersection += cb
		}
		if ca > cb {
			union += ca
		} else {
			union += cb
		}
	}

	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// extractStringSlice converts an interface{} to []string.
func extractStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
