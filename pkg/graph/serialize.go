package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// nodeLinkData is the NetworkX node_link_data JSON format.
type nodeLinkData struct {
	Directed   bool             `json:"directed"`
	Multigraph bool             `json:"multigraph"`
	Graph      map[string]any   `json:"graph"`
	Nodes      []map[string]any `json:"nodes"`
	Links      []map[string]any `json:"links"`
}

// ToJSON serializes the graph in NetworkX node_link_data format.
func (g *Graph) ToJSON() ([]byte, error) {
	data := nodeLinkData{
		Directed:   g.directed,
		Multigraph: false,
		Graph:      g.Metadata,
		Nodes:      make([]map[string]any, 0),
		Links:      make([]map[string]any, 0),
	}
	if data.Graph == nil {
		data.Graph = make(map[string]any)
	}

	for _, id := range g.Nodes() {
		node := map[string]any{"id": id}
		for k, v := range g.nodes[id] {
			node[k] = v
		}
		data.Nodes = append(data.Nodes, node)
	}

	for _, e := range g.Edges() {
		link := map[string]any{
			"source": e.Source,
			"target": e.Target,
		}
		for k, v := range e.Attrs {
			link[k] = v
		}
		data.Links = append(data.Links, link)
	}

	return json.MarshalIndent(data, "", "  ")
}

// SaveJSON writes the graph to a JSON file in NetworkX node_link_data format.
func (g *Graph) SaveJSON(path string) error {
	data, err := g.ToJSON()
	if err != nil {
		return fmt.Errorf("marshal graph: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadJSON reads a graph from a NetworkX node_link_data JSON file.
func LoadJSON(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return FromJSON(data)
}

// FromJSON deserializes a graph from NetworkX node_link_data JSON bytes.
func FromJSON(data []byte) (*Graph, error) {
	var nld nodeLinkData
	if err := json.Unmarshal(data, &nld); err != nil {
		return nil, fmt.Errorf("unmarshal graph JSON: %w", err)
	}

	g := New(nld.Directed)
	if nld.Graph != nil {
		g.Metadata = nld.Graph
	}

	for _, nodeData := range nld.Nodes {
		id, ok := nodeData["id"].(string)
		if !ok {
			continue
		}
		attrs := make(map[string]any)
		for k, v := range nodeData {
			if k != "id" {
				attrs[k] = v
			}
		}
		g.AddNode(id, attrs)
	}

	for _, linkData := range nld.Links {
		source, _ := linkData["source"].(string)
		target, _ := linkData["target"].(string)
		if source == "" || target == "" {
			continue
		}
		attrs := make(map[string]any)
		for k, v := range linkData {
			if k != "source" && k != "target" {
				attrs[k] = v
			}
		}
		g.AddEdge(source, target, attrs)
	}

	return g, nil
}
