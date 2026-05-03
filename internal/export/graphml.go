package export

import (
	"fmt"
	"html"
	"os"
	"path/filepath"

	"github.com/qiangli/gfy/internal/graph"
)

func xmlEscape(s string) string {
	return html.EscapeString(s)
}

// ToGraphML writes the graph in GraphML XML format with community attributes.
func ToGraphML(g *graph.Graph, communities map[int][]string, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	nodeCommunity := nodeCommunityMap(communities)

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write XML header.
	fmt.Fprintln(f, `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintln(f, `<graphml xmlns="http://graphml.graphstruct.org/graphml"`)
	fmt.Fprintln(f, `  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`)
	fmt.Fprintln(f, `  xsi:schemaLocation="http://graphml.graphstruct.org/graphml http://graphml.graphstruct.org/xmlns/1.0/graphml.xsd">`)

	// Key definitions for node attributes.
	fmt.Fprintln(f, `  <key id="label" for="node" attr.name="label" attr.type="string"/>`)
	fmt.Fprintln(f, `  <key id="file_type" for="node" attr.name="file_type" attr.type="string"/>`)
	fmt.Fprintln(f, `  <key id="source_file" for="node" attr.name="source_file" attr.type="string"/>`)
	fmt.Fprintln(f, `  <key id="source_location" for="node" attr.name="source_location" attr.type="string"/>`)
	fmt.Fprintln(f, `  <key id="community" for="node" attr.name="community" attr.type="int"/>`)

	// Key definitions for edge attributes.
	fmt.Fprintln(f, `  <key id="relation" for="edge" attr.name="relation" attr.type="string"/>`)
	fmt.Fprintln(f, `  <key id="confidence" for="edge" attr.name="confidence" attr.type="string"/>`)
	fmt.Fprintln(f, `  <key id="weight" for="edge" attr.name="weight" attr.type="double"/>`)

	edgeDefault := "undirected"
	if g.IsDirected() {
		edgeDefault = "directed"
	}
	fmt.Fprintf(f, `  <graph id="G" edgedefault="%s">`+"\n", edgeDefault)

	// Nodes.
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		fmt.Fprintf(f, "    <node id=\"%s\">\n", xmlEscape(id))
		writeDataElem(f, "label", attrStr(attrs, "label", id))
		writeDataElem(f, "file_type", attrStr(attrs, "file_type", ""))
		writeDataElem(f, "source_file", attrStr(attrs, "source_file", ""))
		writeDataElem(f, "source_location", attrStr(attrs, "source_location", ""))
		cid := nodeCommunity[id]
		fmt.Fprintf(f, "      <data key=\"community\">%d</data>\n", cid)
		fmt.Fprintln(f, "    </node>")
	}

	// Edges.
	for i, e := range g.Edges() {
		fmt.Fprintf(f, "    <edge id=\"e%d\" source=\"%s\" target=\"%s\">\n",
			i, xmlEscape(e.Source), xmlEscape(e.Target))
		writeDataElem(f, "relation", attrStr(e.Attrs, "relation", ""))
		writeDataElem(f, "confidence", attrStr(e.Attrs, "confidence", ""))
		if w, ok := e.Attrs["weight"]; ok {
			fmt.Fprintf(f, "      <data key=\"weight\">%v</data>\n", w)
		}
		fmt.Fprintln(f, "    </edge>")
	}

	fmt.Fprintln(f, "  </graph>")
	fmt.Fprintln(f, "</graphml>")
	return nil
}

func writeDataElem(f *os.File, key, value string) {
	if value != "" {
		fmt.Fprintf(f, "      <data key=\"%s\">%s</data>\n", key, xmlEscape(value))
	}
}
