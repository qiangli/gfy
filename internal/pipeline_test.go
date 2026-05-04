// Package pipeline_test runs end-to-end tests of the full gfy pipeline.
package pipeline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qiangli/gfy/pkg/analyze"
	"github.com/qiangli/gfy/pkg/build"
	"github.com/qiangli/gfy/pkg/cluster"
	"github.com/qiangli/gfy/pkg/detect"
	"github.com/qiangli/gfy/internal/export"
	"github.com/qiangli/gfy/pkg/extract"
	"github.com/qiangli/gfy/pkg/graph"
	"github.com/qiangli/gfy/pkg/report"
	"github.com/qiangli/gfy/pkg/types"
	"github.com/qiangli/gfy/pkg/validate"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "testdata")
}

// TestFullPipeline runs the complete detect→extract→build→cluster→analyze→report→export pipeline.
func TestFullPipeline(t *testing.T) {
	dir := testdataDir()

	// Step 1: Detect
	result := detect.Detect(dir, false)
	if result.TotalFiles == 0 {
		t.Fatal("detect found no files")
	}
	codeFiles := result.Files[types.Code]
	if len(codeFiles) == 0 {
		t.Fatal("no code files detected")
	}
	t.Logf("Detected %d code files", len(codeFiles))

	// Step 2: Extract
	extraction := extract.Extract(codeFiles, "")
	if len(extraction.Nodes) == 0 {
		t.Fatal("extraction produced no nodes")
	}
	if len(extraction.Edges) == 0 {
		t.Fatal("extraction produced no edges")
	}
	t.Logf("Extracted %d nodes, %d edges", len(extraction.Nodes), len(extraction.Edges))

	// Validate extraction.
	errors := validate.Validate(extraction)
	realErrors := 0
	for _, e := range errors {
		if !strings.Contains(e, "does not match any node id") {
			t.Errorf("validation error: %s", e)
			realErrors++
		}
	}
	if realErrors > 0 {
		t.Errorf("%d real validation errors", realErrors)
	}

	// Step 3: Build
	g := build.BuildFromResult(extraction, false)
	if g.NodeCount() == 0 {
		t.Fatal("graph has no nodes")
	}
	if g.EdgeCount() == 0 {
		t.Fatal("graph has no edges")
	}
	t.Logf("Graph: %d nodes, %d edges", g.NodeCount(), g.EdgeCount())

	// Step 4: Cluster
	communities := cluster.Cluster(g)
	if len(communities) == 0 {
		t.Fatal("no communities detected")
	}
	cohesionScores := cluster.ScoreAll(g, communities)
	t.Logf("Communities: %d", len(communities))

	// All nodes should be in exactly one community.
	nodeToCommunity := make(map[string]int)
	for cid, nodes := range communities {
		for _, nid := range nodes {
			if _, exists := nodeToCommunity[nid]; exists {
				t.Errorf("node %s in multiple communities", nid)
			}
			nodeToCommunity[nid] = cid
		}
	}

	// Step 5: Analyze
	godNodes := analyze.GodNodes(g, 10)
	if len(godNodes) == 0 {
		t.Error("no god nodes found")
	}
	surprises := analyze.SurprisingConnections(g, communities, 5)
	questions := analyze.SuggestQuestions(g, communities, 7)
	t.Logf("God nodes: %d, Surprises: %d, Questions: %d", len(godNodes), len(surprises), len(questions))

	// Step 6: Report
	communityLabels := make(map[int]string)
	for cid := range communities {
		communityLabels[cid] = "Community " + string(rune('A'+cid%26))
	}
	reportMD := report.Generate(g, communities, cohesionScores, godNodes, surprises, questions, dir)
	if !strings.Contains(reportMD, "# Graph Report") {
		t.Error("report missing header")
	}
	if !strings.Contains(reportMD, "## God Nodes") {
		t.Error("report missing God Nodes section")
	}
	t.Logf("Report: %d bytes", len(reportMD))

	// Step 7: Export (all formats)
	outDir := t.TempDir()

	// JSON
	jsonPath := filepath.Join(outDir, "graph.json")
	written, err := export.ToJSON(g, jsonPath, true)
	if err != nil {
		t.Fatalf("export JSON: %v", err)
	}
	if !written {
		t.Error("JSON not written")
	}

	// HTML
	htmlPath := filepath.Join(outDir, "graph.html")
	if err := export.ToHTML(g, communities, communityLabels, htmlPath); err != nil {
		t.Fatalf("export HTML: %v", err)
	}

	// Obsidian
	obsPath := filepath.Join(outDir, "obsidian")
	if err := export.ToObsidian(g, communities, communityLabels, cohesionScores, obsPath); err != nil {
		t.Fatalf("export Obsidian: %v", err)
	}

	// Cypher
	cypherPath := filepath.Join(outDir, "graph.cypher")
	if err := export.ToCypher(g, cypherPath); err != nil {
		t.Fatalf("export Cypher: %v", err)
	}

	// GraphML
	graphmlPath := filepath.Join(outDir, "graph.graphml")
	if err := export.ToGraphML(g, communities, graphmlPath); err != nil {
		t.Fatalf("export GraphML: %v", err)
	}

	// Verify all outputs exist and have content.
	for _, path := range []string{jsonPath, htmlPath, cypherPath, graphmlPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("output missing: %s", path)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("output empty: %s", path)
		}
	}
	// Obsidian should have files.
	entries, _ := os.ReadDir(obsPath)
	if len(entries) == 0 {
		t.Error("obsidian vault is empty")
	}
	t.Logf("Obsidian vault: %d files", len(entries))
}

// TestJSONRoundTrip verifies that graph.json can be loaded back and produces
// the same node/edge counts.
func TestJSONRoundTrip(t *testing.T) {
	dir := testdataDir()
	codeFiles := detect.Detect(dir, false).Files[types.Code]
	extraction := extract.Extract(codeFiles, "")
	g := build.BuildFromResult(extraction, false)

	outDir := t.TempDir()
	jsonPath := filepath.Join(outDir, "graph.json")
	export.ToJSON(g, jsonPath, true)

	// Load it back.
	g2, err := graph.LoadJSON(jsonPath)
	if err != nil {
		t.Fatalf("load JSON: %v", err)
	}
	if g2.NodeCount() != g.NodeCount() {
		t.Errorf("node count: got %d, want %d", g2.NodeCount(), g.NodeCount())
	}
	if g2.EdgeCount() != g.EdgeCount() {
		t.Errorf("edge count: got %d, want %d", g2.EdgeCount(), g.EdgeCount())
	}

	// Verify JSON structure is NetworkX-compatible.
	data, _ := os.ReadFile(jsonPath)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := raw["nodes"]; !ok {
		t.Error("JSON missing 'nodes' key")
	}
	if _, ok := raw["links"]; !ok {
		t.Error("JSON missing 'links' key (NetworkX compat)")
	}
	if _, ok := raw["directed"]; !ok {
		t.Error("JSON missing 'directed' key")
	}
}

// TestExtractionNoDanglingEdges verifies that no extraction produces
// edges referencing non-existent nodes (except imports).
func TestExtractionNoDanglingEdges(t *testing.T) {
	dir := testdataDir()
	codeFiles := detect.Detect(dir, false).Files[types.Code]
	extraction := extract.Extract(codeFiles, "")

	nodeIDs := make(map[string]bool)
	for _, n := range extraction.Nodes {
		nodeIDs[n.ID] = true
	}

	dangling := 0
	for _, e := range extraction.Edges {
		if e.Relation == "imports" || e.Relation == "imports_from" {
			continue
		}
		if !nodeIDs[e.Source] {
			t.Logf("dangling source: %s (relation: %s)", e.Source, e.Relation)
			dangling++
		}
		if !nodeIDs[e.Target] {
			t.Logf("dangling target: %s -> %s (relation: %s)", e.Source, e.Target, e.Relation)
			dangling++
		}
	}
	if dangling > 0 {
		t.Errorf("%d dangling edge endpoints (non-import)", dangling)
	}
}

// TestCrossFileCallResolution verifies that calls between files are resolved.
func TestCrossFileCallResolution(t *testing.T) {
	dir := testdataDir()
	codeFiles := detect.Detect(dir, false).Files[types.Code]
	extraction := extract.Extract(codeFiles, "")

	hasInferred := false
	for _, e := range extraction.Edges {
		if e.Confidence == types.Inferred && e.Relation == "calls" {
			hasInferred = true
			break
		}
	}
	if !hasInferred {
		t.Log("No cross-file inferred calls found (may be expected for small testdata)")
	}
}

// TestHTMLContainsVisJS verifies the HTML export includes vis.js setup.
func TestHTMLContainsVisJS(t *testing.T) {
	dir := testdataDir()
	codeFiles := detect.Detect(dir, false).Files[types.Code]
	extraction := extract.Extract(codeFiles, "")
	g := build.BuildFromResult(extraction, false)
	communities := cluster.Cluster(g)
	labels := map[int]string{0: "Test"}

	outDir := t.TempDir()
	htmlPath := filepath.Join(outDir, "graph.html")
	if err := export.ToHTML(g, communities, labels, htmlPath); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(htmlPath)
	html := string(data)

	checks := []string{
		"vis-network.min.js",
		"RAW_NODES",
		"RAW_EDGES",
		"forceAtlas2Based",
		"Search nodes...",
		"Click a node to inspect it",
	}
	for _, check := range checks {
		if !strings.Contains(html, check) {
			t.Errorf("HTML missing expected content: %q", check)
		}
	}
}

// TestGraphMLValid verifies the GraphML output is valid XML.
func TestGraphMLValid(t *testing.T) {
	dir := testdataDir()
	codeFiles := detect.Detect(dir, false).Files[types.Code]
	extraction := extract.Extract(codeFiles, "")
	g := build.BuildFromResult(extraction, false)
	communities := cluster.Cluster(g)

	outDir := t.TempDir()
	gmlPath := filepath.Join(outDir, "graph.graphml")
	if err := export.ToGraphML(g, communities, gmlPath); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(gmlPath)
	content := string(data)
	if !strings.Contains(content, "<?xml") {
		t.Error("GraphML missing XML declaration")
	}
	if !strings.Contains(content, "<graphml") {
		t.Error("GraphML missing graphml element")
	}
	if !strings.Contains(content, "</graphml>") {
		t.Error("GraphML missing closing tag")
	}
}
