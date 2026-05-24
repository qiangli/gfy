// Package watch monitors a directory for file changes and rebuilds the graph.
package watch

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/qiangli/gfy/internal/export"
	"github.com/qiangli/gfy/pkg/build"
	"github.com/qiangli/gfy/pkg/cluster"
	"github.com/qiangli/gfy/pkg/detect"
	"github.com/qiangli/gfy/pkg/extract"
	"github.com/qiangli/gfy/pkg/graph"
	"github.com/qiangli/gfy/pkg/search"
	"github.com/qiangli/gfy/pkg/types"
)

// Watch monitors a directory for file changes, rebuilds the graph, and
// serves a live-reloading visualization at http://localhost:<port>.
func Watch(rootPath string, debounce time.Duration) error {
	rootPath, _ = filepath.Abs(rootPath)
	if debounce <= 0 {
		debounce = 3 * time.Second
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	// Add all directories recursively.
	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || detect.SkipDirs[name] {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk directories: %w", err)
	}

	// Live-reload state.
	liveState := &liveState{}

	// Initial build.
	liveState.rebuild(rootPath)

	// Start HTTP server for live view.
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	addr := ln.Addr().String()
	go serveLiveUI(ln, liveState)
	fmt.Printf("Live graph: http://%s\n", addr)
	fmt.Printf("Watching %s for changes (debounce: %s)...\n", rootPath, debounce)

	var timer *time.Timer
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if !isCodeFile(event.Name) {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, func() {
					liveState.rebuild(rootPath)
				})
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "watch error: %v\n", err)
		}
	}
}

func isCodeFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return detect.CodeExtensions[ext]
}

// liveState holds the latest graph data and notifies SSE clients on rebuild.
type liveState struct {
	mu          sync.RWMutex
	g           *graph.Graph
	communities map[int][]string
	version     int64
	clients     []chan struct{}
	clientsMu   sync.Mutex
}

func (s *liveState) rebuild(rootPath string) {
	fmt.Printf("[%s] Rebuilding graph...\n", time.Now().Format("15:04:05"))

	result := detect.Detect(rootPath, false)
	codeFiles := result.Files[types.Code]
	if len(codeFiles) == 0 {
		fmt.Println("  No code files found.")
		return
	}

	extraction := extract.Extract(codeFiles, rootPath)
	g := build.BuildFromResult(extraction, false)
	communities := cluster.Cluster(g)

	// Also write to disk.
	outDir := filepath.Join(rootPath, ".gfy-out")
	os.MkdirAll(outDir, 0o755)
	export.ToJSON(g, filepath.Join(outDir, "graph.json"), true)

	s.mu.Lock()
	s.g = g
	s.communities = communities
	s.version = time.Now().UnixMilli()
	s.mu.Unlock()

	fmt.Printf("  %d nodes, %d edges, %d communities\n",
		g.NodeCount(), g.EdgeCount(), len(communities))

	// Notify all SSE clients.
	s.clientsMu.Lock()
	for _, ch := range s.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.clientsMu.Unlock()
}

func (s *liveState) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	s.clientsMu.Lock()
	s.clients = append(s.clients, ch)
	s.clientsMu.Unlock()
	return ch
}

func (s *liveState) unsubscribe(ch chan struct{}) {
	s.clientsMu.Lock()
	for i, c := range s.clients {
		if c == ch {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			break
		}
	}
	s.clientsMu.Unlock()
}

func (s *liveState) graphData() (nodes, edges []map[string]any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.g == nil {
		return nil, nil
	}

	nodeCommunity := make(map[string]int)
	for cid, ns := range s.communities {
		for _, n := range ns {
			nodeCommunity[n] = cid
		}
	}

	colors := []string{
		"#4E79A7", "#F28E2B", "#E15759", "#76B7B2", "#59A14F",
		"#EDC948", "#B07AA1", "#FF9DA7", "#9C755F", "#BAB0AC",
	}
	maxDeg := 1
	degrees := make(map[string]int)
	for _, id := range s.g.Nodes() {
		d := s.g.Degree(id)
		degrees[id] = d
		if d > maxDeg {
			maxDeg = d
		}
	}

	for _, id := range s.g.Nodes() {
		attrs := s.g.NodeAttrs(id)
		cid := nodeCommunity[id]
		color := colors[cid%len(colors)]
		label, _ := attrs["label"].(string)
		fileType, _ := attrs["file_type"].(string)
		deg := degrees[id]
		size := 10.0 + 30.0*float64(deg)/float64(maxDeg)
		fontSize := 0
		if float64(deg) >= float64(maxDeg)*0.15 {
			fontSize = 12
		}
		nodes = append(nodes, map[string]any{
			"id": id, "label": label,
			"color":     map[string]any{"background": color, "border": color, "highlight": map[string]any{"background": "#ffffff", "border": color}},
			"size":      int(size*10) / 10,
			"font":      map[string]any{"size": fontSize, "color": "#ffffff"},
			"title":     label,
			"community": cid, "file_type": fileType, "degree": deg,
		})
	}
	for _, e := range s.g.Edges() {
		conf, _ := e.Attrs["confidence"].(string)
		rel, _ := e.Attrs["relation"].(string)
		dashes := conf != "EXTRACTED"
		width := 2
		opacity := 0.7
		if dashes {
			width = 1
			opacity = 0.35
		}
		edges = append(edges, map[string]any{
			"from": e.Source, "to": e.Target,
			"title":  rel + " [" + conf + "]",
			"dashes": dashes, "width": width,
			"color": map[string]any{"opacity": opacity},
		})
	}
	return nodes, edges
}

// --- HTTP server ---

func serveLiveUI(ln net.Listener, state *liveState) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(liveHTML))
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes, edges := state.graphData()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"nodes": nodes, "edges": edges})
	})

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		state.mu.RLock()
		g := state.g
		state.mu.RUnlock()
		if g == nil || q == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]any{})
			return
		}
		results := search.ScoreNodes(g, q)
		if len(results) > 20 {
			results = results[:20]
		}
		type item struct {
			ID    string  `json:"id"`
			Label string  `json:"label"`
			Score float64 `json:"score"`
		}
		out := make([]item, len(results))
		for i, r := range results {
			out[i] = item{r.ID, r.Label, r.Score}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := state.subscribe()
		defer state.unsubscribe(ch)

		for {
			select {
			case <-ch:
				fmt.Fprintf(w, "data: reload\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	http.Serve(ln, mux)
}

const liveHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>gfy — live</title>
<script src="https://unpkg.com/vis-network/standalone/umd/vis-network.min.js"></script>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #0f0f1a; color: #e0e0e0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; display: flex; height: 100vh; overflow: hidden; }
  #graph { flex: 1; }
  #sidebar { width: 280px; background: #1a1a2e; border-left: 1px solid #2a2a4e; display: flex; flex-direction: column; overflow: hidden; }
  #search-wrap { padding: 12px; border-bottom: 1px solid #2a2a4e; }
  #search { width: 100%; background: #0f0f1a; border: 1px solid #3a3a5e; color: #e0e0e0; padding: 7px 10px; border-radius: 6px; font-size: 13px; outline: none; }
  #search:focus { border-color: #4E79A7; }
  #search-results { max-height: 140px; overflow-y: auto; padding: 4px 12px; border-bottom: 1px solid #2a2a4e; display: none; }
  .search-item { padding: 4px 6px; cursor: pointer; border-radius: 4px; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .search-item:hover { background: #2a2a4e; }
  #info-panel { padding: 14px; border-bottom: 1px solid #2a2a4e; min-height: 140px; }
  #info-panel h3 { font-size: 13px; color: #aaa; margin-bottom: 8px; text-transform: uppercase; letter-spacing: 0.05em; }
  #info-content { font-size: 13px; color: #ccc; line-height: 1.6; }
  #info-content .field { margin-bottom: 5px; }
  #info-content .field b { color: #e0e0e0; }
  #info-content .empty { color: #555; font-style: italic; }
  .neighbor-link { display: block; padding: 2px 6px; margin: 2px 0; border-radius: 3px; cursor: pointer; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; border-left: 3px solid #333; }
  .neighbor-link:hover { background: #2a2a4e; }
  #neighbors-list { max-height: 160px; overflow-y: auto; margin-top: 4px; }
  #stats { padding: 10px 14px; border-top: 1px solid #2a2a4e; font-size: 11px; color: #555; flex: 1; display: flex; align-items: flex-end; }
  #status { padding: 10px 14px; border-top: 1px solid #2a2a4e; font-size: 11px; }
  .live { color: #59A14F; }
  .reloading { color: #F28E2B; }
</style>
</head>
<body>
<div id="graph"></div>
<div id="sidebar">
  <div id="search-wrap">
    <input id="search" type="text" placeholder="Search nodes..." autocomplete="off">
    <div id="search-results"></div>
  </div>
  <div id="info-panel">
    <h3>Node Info</h3>
    <div id="info-content"><span class="empty">Click a node to inspect it</span></div>
  </div>
  <div id="stats"></div>
  <div id="status" class="live">&#9679; Live</div>
</div>
<script>
let network, nodesDS, edgesDS, rawNodes = [];

function esc(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}

async function loadGraph() {
  const statusEl = document.getElementById('status');
  statusEl.className = 'reloading';
  statusEl.innerHTML = '&#8635; Reloading...';

  const resp = await fetch('/api/graph');
  const data = await resp.json();
  rawNodes = data.nodes || [];
  const rawEdges = data.edges || [];

  const nodes = rawNodes.map(n => ({
    id: n.id, label: n.label, color: n.color, size: n.size,
    font: n.font, title: n.title,
    _community: n.community, _file_type: n.file_type, _degree: n.degree,
  }));
  const edges = rawEdges.map((e, i) => ({
    id: i, from: e.from, to: e.to, label: '',
    title: e.title, dashes: e.dashes, width: e.width, color: e.color,
    arrows: { to: { enabled: true, scaleFactor: 0.5 } },
  }));

  if (!network) {
    nodesDS = new vis.DataSet(nodes);
    edgesDS = new vis.DataSet(edges);
    const container = document.getElementById('graph');
    network = new vis.Network(container, { nodes: nodesDS, edges: edgesDS }, {
      physics: {
        enabled: true, solver: 'forceAtlas2Based',
        forceAtlas2Based: { gravitationalConstant: -60, centralGravity: 0.005, springLength: 120, springConstant: 0.08, damping: 0.4, avoidOverlap: 0.8 },
        stabilization: { iterations: 200, fit: true },
      },
      interaction: { hover: true, tooltipDelay: 100, hideEdgesOnDrag: true },
      nodes: { shape: 'dot', borderWidth: 1.5 },
      edges: { smooth: { type: 'continuous', roundness: 0.2 }, selectionWidth: 3 },
    });
    network.once('stabilizationIterationsDone', () => network.setOptions({ physics: { enabled: false } }));

    let hoveredNodeId = null;
    const container2 = document.getElementById('graph');
    network.on('hoverNode', p => { hoveredNodeId = p.node; container2.style.cursor = 'pointer'; });
    network.on('blurNode', () => { hoveredNodeId = null; container2.style.cursor = 'default'; });
    container2.addEventListener('click', () => { if (hoveredNodeId !== null) { showInfo(hoveredNodeId); network.selectNodes([hoveredNodeId]); } });
    network.on('click', p => {
      if (p.nodes.length > 0) showInfo(p.nodes[0]);
      else if (hoveredNodeId === null) document.getElementById('info-content').innerHTML = '<span class="empty">Click a node to inspect it</span>';
    });
  } else {
    nodesDS.clear();
    nodesDS.add(nodes);
    edgesDS.clear();
    edgesDS.add(edges);
  }

  document.getElementById('stats').textContent = rawNodes.length + ' nodes \u00b7 ' + rawEdges.length + ' edges';
  statusEl.className = 'live';
  statusEl.innerHTML = '&#9679; Live';
}

function showInfo(nodeId) {
  const n = nodesDS.get(nodeId);
  if (!n) return;
  const neighborIds = network.getConnectedNodes(nodeId);
  const items = neighborIds.map(nid => {
    const nb = nodesDS.get(nid);
    const c = nb ? nb.color.background : '#555';
    return '<span class="neighbor-link" style="border-left-color:'+esc(c)+'" onclick="focusNode(\''+esc(nid)+'\')">'+esc(nb?nb.label:nid)+'</span>';
  }).join('');
  document.getElementById('info-content').innerHTML =
    '<div class="field"><b>'+esc(n.label)+'</b></div>'+
    '<div class="field">Type: '+esc(n._file_type||'unknown')+'</div>'+
    '<div class="field">Degree: '+n._degree+'</div>'+
    (neighborIds.length?'<div class="field" style="margin-top:8px;color:#aaa;font-size:11px">Neighbors ('+neighborIds.length+')</div><div id="neighbors-list">'+items+'</div>':'');
}

function focusNode(nodeId) {
  network.focus(nodeId, { scale: 1.4, animation: true });
  network.selectNodes([nodeId]);
  showInfo(nodeId);
}

// Search (server-side fuzzy via /api/search)
const searchInput = document.getElementById('search');
const searchResults = document.getElementById('search-results');
let searchTimer = null;
searchInput.addEventListener('input', () => {
  const q = searchInput.value.trim();
  searchResults.innerHTML = '';
  if (!q) { searchResults.style.display = 'none'; return; }
  clearTimeout(searchTimer);
  searchTimer = setTimeout(async () => {
    const resp = await fetch('/api/search?q=' + encodeURIComponent(q));
    const matches = await resp.json();
    searchResults.innerHTML = '';
    if (!matches.length) { searchResults.style.display = 'none'; return; }
    searchResults.style.display = 'block';
    matches.forEach(m => {
      const n = nodesDS.get(m.id);
      const el = document.createElement('div');
      el.className = 'search-item';
      el.textContent = m.label + ' (' + m.score.toFixed(1) + ')';
      el.style.borderLeft = '3px solid '+(n && n.color ? n.color.background : '#555');
      el.style.paddingLeft = '8px';
      el.onclick = () => { focusNode(m.id); searchResults.style.display='none'; searchInput.value=''; };
      searchResults.appendChild(el);
    });
  }, 150);
});

// SSE live reload
const evtSource = new EventSource('/api/events');
evtSource.onmessage = () => loadGraph();

loadGraph();
</script>
</body>
</html>`
