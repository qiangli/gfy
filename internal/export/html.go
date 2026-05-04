package export

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qiangli/gfy/pkg/graph"
)

// CommunityColors is a 10-color palette for community visualization.
var CommunityColors = []string{
	"#4E79A7", "#F28E2B", "#E15759", "#76B7B2", "#59A14F",
	"#EDC948", "#B07AA1", "#FF9DA7", "#9C755F", "#BAB0AC",
}

const warnNodesThreshold = 10000

var controlCharRe = regexp.MustCompile(`[\x00-\x1f\x7f]`)

func sanitizeLabel(text string) string {
	text = controlCharRe.ReplaceAllString(text, "")
	if len(text) > 256 {
		text = text[:256]
	}
	return text
}

// ToHTML generates an interactive vis.js HTML visualization.
func ToHTML(g *graph.Graph, communities map[int][]string, communityLabels map[int]string, outputPath string) error {
	if g.NodeCount() > warnNodesThreshold {
		fmt.Fprintf(os.Stderr, "  Warning: graph has %d nodes — HTML visualization may be slow in the browser.\n", g.NodeCount())
	}

	nodeCommunity := nodeCommunityMap(communities)

	// Compute degrees.
	maxDeg := 1
	degrees := make(map[string]int)
	for _, id := range g.Nodes() {
		d := g.Degree(id)
		degrees[id] = d
		if d > maxDeg {
			maxDeg = d
		}
	}

	// Build vis.js nodes.
	type visNode struct {
		ID            string         `json:"id"`
		Label         string         `json:"label"`
		Color         map[string]any `json:"color"`
		Size          float64        `json:"size"`
		Font          map[string]any `json:"font"`
		Title         string         `json:"title"`
		Community     int            `json:"community"`
		CommunityName string         `json:"community_name"`
		SourceFile    string         `json:"source_file"`
		FileType      string         `json:"file_type"`
		Degree        int            `json:"degree"`
		Tags          []string       `json:"tags"`
		Comment       string         `json:"comment,omitempty"`
		LogMessages   []string       `json:"log_messages,omitempty"`
		ThrowMessages []string       `json:"throw_messages,omitempty"`
	}

	var visNodes []visNode
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		cid := nodeCommunity[id]
		color := CommunityColors[cid%len(CommunityColors)]
		label := sanitizeLabel(attrStr(attrs, "label", id))
		deg := degrees[id]
		size := 10.0 + 30.0*float64(deg)/float64(maxDeg)
		fontSize := 0
		if float64(deg) >= float64(maxDeg)*0.15 {
			fontSize = 12
		}

		commLabel := fmt.Sprintf("Community %d", cid)
		if l, ok := communityLabels[cid]; ok {
			commLabel = l
		}

		visNodes = append(visNodes, visNode{
			ID:    id,
			Label: label,
			Color: map[string]any{
				"background": color,
				"border":     color,
				"highlight":  map[string]any{"background": "#ffffff", "border": color},
			},
			Size:          float64(int(size*10)) / 10,
			Font:          map[string]any{"size": fontSize, "color": "#ffffff"},
			Title:         html.EscapeString(label),
			Community:     cid,
			CommunityName: sanitizeLabel(commLabel),
			SourceFile:    sanitizeLabel(attrStr(attrs, "source_file", "")),
			FileType:      attrStr(attrs, "file_type", ""),
			Degree:        deg,
			Tags:          attrStrSlice(attrs, "tags"),
			Comment:       attrStr(attrs, "comment", ""),
			LogMessages:   attrStrSlice(attrs, "log_messages"),
			ThrowMessages: attrStrSlice(attrs, "throw_messages"),
		})
	}

	// Build vis.js edges.
	type visEdge struct {
		From       string         `json:"from"`
		To         string         `json:"to"`
		Label      string         `json:"label"`
		Title      string         `json:"title"`
		Dashes     bool           `json:"dashes"`
		Width      int            `json:"width"`
		Color      map[string]any `json:"color"`
		Confidence string         `json:"confidence"`
		Src        string         `json:"_src,omitempty"`
		Tgt        string         `json:"_tgt,omitempty"`
	}

	var visEdges []visEdge
	for _, e := range g.Edges() {
		confidence := attrStr(e.Attrs, "confidence", "EXTRACTED")
		relation := attrStr(e.Attrs, "relation", "")
		dashes := confidence != "EXTRACTED"
		width := 2
		opacity := 0.7
		if dashes {
			width = 1
			opacity = 0.35
		}
		visEdges = append(visEdges, visEdge{
			From:       e.Source,
			To:         e.Target,
			Label:      relation,
			Title:      html.EscapeString(fmt.Sprintf("%s [%s]", relation, confidence)),
			Dashes:     dashes,
			Width:      width,
			Color:      map[string]any{"opacity": opacity},
			Confidence: confidence,
			Src:        attrStr(e.Attrs, "_src", ""),
			Tgt:        attrStr(e.Attrs, "_tgt", ""),
		})
	}

	// Build legend.
	type legendItem struct {
		CID   int    `json:"cid"`
		Color string `json:"color"`
		Label string `json:"label"`
		Count int    `json:"count"`
	}
	var legend []legendItem
	for cid := 0; cid < len(communities); cid++ {
		nodes, ok := communities[cid]
		if !ok {
			continue
		}
		color := CommunityColors[cid%len(CommunityColors)]
		commLabel := fmt.Sprintf("Community %d", cid)
		if l, ok := communityLabels[cid]; ok {
			commLabel = l
		}
		legend = append(legend, legendItem{
			CID: cid, Color: color,
			Label: html.EscapeString(sanitizeLabel(commLabel)),
			Count: len(nodes),
		})
	}

	nodesJSON := jsSafe(visNodes)
	edgesJSON := jsSafe(visEdges)
	legendJSON := jsSafe(legend)

	title := html.EscapeString(sanitizeLabel(filepath.Base(outputPath)))
	stats := fmt.Sprintf("%d nodes &middot; %d edges &middot; %d communities",
		g.NodeCount(), g.EdgeCount(), len(communities))

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>gfy - %s</title>
<script src="https://unpkg.com/vis-network/standalone/umd/vis-network.min.js"></script>
%s
</head>
<body>
<div id="graph"></div>
<div id="sidebar">
  <div id="mode-wrap">
    <button id="reset-btn" onclick="resetAll()">Reset</button>
    <button class="zoom-btn" onclick="zoomIn()">+</button>
    <button class="zoom-btn" onclick="zoomOut()">−</button>
    <button onclick="zoomFit()">Fit</button>
  </div>
  <div id="search-wrap">
    <input id="search" type="text" placeholder="Search nodes..." autocomplete="off">
    <div id="search-results"></div>
  </div>
  <div id="path-section">
    <label id="path-label"><input type="checkbox" id="path-check" onchange="togglePathMode()"> Shortest path</label>
    <div id="path-info"></div>
  </div>
  <div id="info-panel">
    <h3 id="info-heading" onclick="restoreFromTrace()">Node Info</h3>
    <div id="info-content"><span class="empty">Click a node to inspect it</span></div>
  </div>
  <div id="filter-tabs">
    <button class="filter-tab active" onclick="switchTab('tags')">Tags</button>
    <button class="filter-tab" onclick="switchTab('communities')">Communities</button>
    <button class="filter-tab" onclick="switchTab('sources')">Sources</button>
  </div>
  <div id="filter-panels">
    <div id="tags-wrap" class="filter-panel">
      <div id="tags-controls" class="filter-controls">
        <button onclick="toggleAllTags(true)">Check All</button>
        <button onclick="toggleAllTags(false)">Uncheck All</button>
      </div>
      <div id="tags-list"></div>
    </div>
    <div id="legend-wrap" class="filter-panel" style="display:none">
      <div id="legend-controls" class="filter-controls">
        <button onclick="toggleAllCommunities(true)">Check All</button>
        <button onclick="toggleAllCommunities(false)">Uncheck All</button>
      </div>
      <input id="community-search" type="text" placeholder="Filter communities..." autocomplete="off">
      <div id="legend"></div>
    </div>
    <div id="sources-wrap" class="filter-panel" style="display:none">
      <div id="sources-controls" class="filter-controls">
        <button onclick="toggleAllSources(true)">Check All</button>
        <button onclick="toggleAllSources(false)">Uncheck All</button>
      </div>
      <input id="source-search" type="text" placeholder="Filter source files..." autocomplete="off">
      <div id="sources-list"></div>
    </div>
  </div>
  <div id="stats">%s</div>
</div>
%s
</body>
</html>`, title, htmlStyles(), stats, htmlScript(nodesJSON, edgesJSON, legendJSON))

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(htmlContent), 0o644)
}

func htmlStyles() string {
	return `<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #0f0f1a; color: #e0e0e0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; display: flex; height: 100vh; overflow: hidden; }
  #graph { flex: 1; }
  #sidebar { width: 280px; background: #1a1a2e; border-left: 1px solid #2a2a4e; display: flex; flex-direction: column; overflow-y: auto; }
  #search-wrap { padding: 12px; border-bottom: 1px solid #2a2a4e; }
  #search { width: 100%; background: #0f0f1a; border: 1px solid #3a3a5e; color: #e0e0e0; padding: 7px 10px; border-radius: 6px; font-size: 13px; outline: none; }
  #search:focus { border-color: #4E79A7; }
  #search-results { max-height: 140px; overflow-y: auto; padding: 4px 12px; border-bottom: 1px solid #2a2a4e; display: none; }
  .search-item { padding: 4px 6px; cursor: pointer; border-radius: 4px; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .search-item:hover { background: #2a2a4e; }
  #info-panel { padding: 14px; border-bottom: 1px solid #2a2a4e; min-height: 140px; }
  #info-panel h3 { font-size: 13px; color: #aaa; margin-bottom: 8px; text-transform: uppercase; letter-spacing: 0.05em; }
  #info-panel h3.clickable { cursor: pointer; }
  #info-panel h3.clickable:hover { color: #4E79A7; }
  #info-content { font-size: 13px; color: #ccc; line-height: 1.6; }
  #info-content .field { margin-bottom: 5px; }
  #info-content .field b { color: #e0e0e0; }
  #info-content .field b.node-name { cursor: pointer; }
  #info-content .field b.node-name:hover { color: #4E79A7; }
  #info-content .empty { color: #555; font-style: italic; }
  .comment-field { font-size: 12px; color: #9a9; font-style: italic; border-left: 2px solid #3a5a3a; padding-left: 8px; white-space: pre-wrap; word-break: break-word; max-height: 120px; overflow-y: auto; }
  .msg-field { font-size: 11px; border-left: 2px solid #555; padding-left: 8px; max-height: 120px; overflow-y: auto; }
  .msg-label { font-weight: bold; font-size: 11px; display: block; margin-bottom: 2px; }
  .msg-item { font-family: monospace; font-size: 11px; white-space: pre-wrap; word-break: break-all; padding: 1px 0; color: #bbb; }
  .throws-field { border-left-color: #E15759; }
  .throws-field .msg-label { color: #E15759; }
  .logs-field { border-left-color: #76B7B2; }
  .logs-field .msg-label { color: #76B7B2; }
  .neighbor-link { display: block; padding: 2px 6px; margin: 2px 0; border-radius: 3px; cursor: pointer; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; border-left: 3px solid #333; }
  .neighbor-link:hover { background: #2a2a4e; }
  #neighbors-list { max-height: 160px; overflow-y: auto; margin-top: 4px; }
  .legend-item { display: flex; align-items: center; gap: 6px; padding: 3px 0; cursor: pointer; font-size: 12px; }
  .legend-item input { accent-color: #4E79A7; cursor: pointer; }
  .legend-dot { width: 10px; height: 10px; border-radius: 50%%; flex-shrink: 0; }
  .legend-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .legend-count { color: #666; font-size: 11px; }
  #stats { padding: 10px 14px; border-top: 1px solid #2a2a4e; font-size: 11px; color: #555; }
  #community-search, #source-search { width: 100%%; background: #0f0f1a; border: 1px solid #3a3a5e; color: #e0e0e0; padding: 5px 8px; border-radius: 4px; font-size: 12px; outline: none; margin-bottom: 8px; }
  #community-search:focus, #source-search:focus { border-color: #4E79A7; }
  #filter-tabs { display: flex; border-bottom: 1px solid #2a2a4e; }
  .filter-tab { flex: 1; background: #0f0f1a; border: none; color: #666; padding: 8px 0; font-size: 12px; cursor: pointer; text-transform: uppercase; letter-spacing: 0.05em; }
  .filter-tab:hover { color: #aaa; }
  .filter-tab.active { color: #e0e0e0; border-bottom: 2px solid #4E79A7; }
  #filter-panels { flex: 1; overflow-y: auto; }
  .filter-panel { padding: 10px 12px; }
  .filter-controls { display: flex; gap: 6px; margin-bottom: 8px; }
  .filter-controls button { flex: 1; background: #0f0f1a; border: 1px solid #3a3a5e; color: #aaa; padding: 4px 0; border-radius: 4px; font-size: 11px; cursor: pointer; }
  .filter-controls button:hover { border-color: #4E79A7; color: #e0e0e0; }
  .tag-check { display: flex; align-items: center; gap: 6px; padding: 3px 0; font-size: 12px; cursor: pointer; }
  .tag-check input { accent-color: #4E79A7; cursor: pointer; }
  .tag-check .tag-count { color: #666; font-size: 11px; margin-left: auto; }
  .source-check { display: flex; align-items: center; gap: 6px; padding: 3px 0; font-size: 12px; cursor: pointer; }
  .source-check input { accent-color: #4E79A7; cursor: pointer; }
  .source-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; direction: rtl; text-align: left; }
  .source-count { color: #666; font-size: 11px; }
  .tag-badge { display: inline-block; background: #2a2a4e; color: #aac; padding: 1px 6px; border-radius: 3px; font-size: 11px; margin: 1px; }
  .trace-btn { background: #2a2a4e; border: 1px solid #4E79A7; color: #aac; padding: 2px 8px; border-radius: 3px; font-size: 11px; cursor: pointer; margin-left: 4px; }
  .trace-btn:hover { background: #4E79A7; color: #fff; }
  #mode-wrap { padding: 8px 12px; border-bottom: 1px solid #2a2a4e; display: flex; flex-wrap: wrap; gap: 6px; }
  #mode-wrap button { flex: 1; background: #0f0f1a; border: 1px solid #3a3a5e; color: #aaa; padding: 6px 0; border-radius: 4px; font-size: 12px; cursor: pointer; }
  #mode-wrap button:hover { border-color: #4E79A7; color: #e0e0e0; }
  #mode-wrap .zoom-btn { font-size: 18px; font-weight: bold; line-height: 1; }
  #reset-btn:hover { border-color: #E15759; color: #E15759; }
  #path-section { padding: 8px 12px; border-bottom: 1px solid #2a2a4e; }
  #path-label { font-size: 12px; color: #aaa; cursor: pointer; display: flex; align-items: center; gap: 6px; }
  #path-label input { accent-color: #4E79A7; cursor: pointer; }
  #path-info { font-size: 12px; color: #aaa; margin-top: 6px; line-height: 1.5; }
  #path-info .path-node { color: #F28E2B; font-weight: bold; cursor: pointer; }
  #path-info .path-node:hover { text-decoration: underline; }
</style>`
}

func htmlScript(nodesJSON, edgesJSON, legendJSON string) string {
	result := fmt.Sprintf(`<script>
const RAW_NODES = %s;
const RAW_EDGES = %s;
const LEGEND = %s;

function esc(s) {
  return String(s).replace(/&/g,'&amp;').replace(/\x3C/g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}

const nodesDS = new vis.DataSet(RAW_NODES.map(n => ({
  id: n.id, label: n.label, color: n.color, size: n.size,
  font: n.font, title: n.title,
  _community: n.community, _community_name: n.community_name,
  _source_file: n.source_file, _file_type: n.file_type, _degree: n.degree,
  _tags: n.tags || [],
  _comment: n.comment || '',
  _log_messages: n.log_messages || [],
  _throw_messages: n.throw_messages || [],
})));

const edgesDS = new vis.DataSet(RAW_EDGES.map((e, i) => ({
  id: i, from: e.from, to: e.to, label: '',
  title: e.title, dashes: e.dashes, width: e.width, color: e.color,
  arrows: { to: { enabled: true, scaleFactor: 0.5 } },
})));

const container = document.getElementById('graph');
const network = new vis.Network(container, { nodes: nodesDS, edges: edgesDS }, {
  physics: {
    enabled: true, solver: 'forceAtlas2Based',
    forceAtlas2Based: { gravitationalConstant: -60, centralGravity: 0.005, springLength: 120, springConstant: 0.08, damping: 0.4, avoidOverlap: 0.8 },
    stabilization: { iterations: 200, fit: true },
  },
  interaction: { hover: true, tooltipDelay: 100, hideEdgesOnDrag: true, navigationButtons: false, keyboard: false },
  nodes: { shape: 'dot', borderWidth: 1.5,
    chosen: { node: function(values) { values.size *= 1.6; values.borderWidth = 4; values.shadowSize = 20; values.shadowColor = 'rgba(255,255,255,0.5)'; } },
    shadow: { enabled: false },
  },
  edges: { smooth: { type: 'continuous', roundness: 0.2 }, selectionWidth: 3 },
});

network.once('stabilizationIterationsDone', () => { network.setOptions({ physics: { enabled: false } }); });

function showInfo(nodeId) {
  const n = nodesDS.get(nodeId);
  if (!n) return;
  const neighborIds = network.getConnectedNodes(nodeId);
  const neighborItems = neighborIds.map(nid => {
    const nb = nodesDS.get(nid);
    const color = nb ? nb.color.background : '#555';
    return '<span class="neighbor-link" style="border-left-color:'+esc(color)+'" onclick="blinkNode(\''+nid.replace(/'/g,"\\'")+ '\')">'+esc(nb ? nb.label : nid)+'</span>';
  }).join('');
  document.getElementById('info-content').innerHTML =
    '<div class="field"><b class="node-name" onclick="blinkNode(\''+n.id.replace(/'/g,"\\'")+'\')">'+esc(n.label)+'</b></div>'+
    '<div class="field">Type: '+esc(n._file_type || 'unknown')+'</div>'+
    '<div class="field">Community: '+esc(n._community_name)+'</div>'+
    '<div class="field">Source: '+esc(n._source_file || '-')+'</div>'+
    '<div class="field">Degree: '+n._degree+'</div>'+
    (n._tags.length ? '<div class="field">Tags: '+n._tags.map(t=>'<span class="tag-badge">'+esc(t)+'</span>').join(' ')+'</div>' : '')+
    (n._comment ? '<div class="field comment-field">'+esc(n._comment)+'</div>' : '')+
    (n._throw_messages.length ? '<div class="field msg-field throws-field"><span class="msg-label">Throws:</span>'+n._throw_messages.map(m=>'<div class="msg-item">'+esc(m)+'</div>').join('')+'</div>' : '')+
    (n._log_messages.length ? '<div class="field msg-field logs-field"><span class="msg-label">Logs:</span>'+n._log_messages.map(m=>'<div class="msg-item">'+esc(m)+'</div>').join('')+'</div>' : '')+
    '<div class="field"><button class="trace-btn" onclick="traceToNode(\''+n.id+'\')">Trace callers</button></div>'+
    (neighborIds.length ? '<div class="field" style="margin-top:8px;color:#aaa;font-size:11px">Neighbors ('+neighborIds.length+')</div><div id="neighbors-list">'+neighborItems+'</div>' : '');
}

let selectedNodeId = null;
let traceActive = false;
let traceTargetId = null;

function restoreFromTrace() {
  if (!traceActive || !traceTargetId) return;
  traceActive = false;
  const heading = document.getElementById('info-heading');
  heading.classList.remove('clickable');
  heading.title = '';
  clearPathHighlight();
  focusNode(traceTargetId);
}

function dimColor(hexColor, alpha) {
  // Convert hex to rgba for dimming.
  const r = parseInt(hexColor.slice(1,3), 16);
  const g = parseInt(hexColor.slice(3,5), 16);
  const b = parseInt(hexColor.slice(5,7), 16);
  return 'rgba('+r+','+g+','+b+','+alpha+')';
}

function highlightNode(nodeId) {
  selectedNodeId = nodeId;
  const neighborIds = new Set(network.getConnectedNodes(nodeId));
  neighborIds.add(nodeId);
  const updates = RAW_NODES.map(n => {
    const bg = n.color.background;
    if (n.id === nodeId) {
      return { id: n.id, color: { background: bg, border: '#ffffff' },
               font: { size: 14, color: '#ffffff', strokeWidth: 3, strokeColor: '#000000' },
               shadow: { enabled: true, size: 25, color: 'rgba(255,255,255,0.4)' }, borderWidth: 4 };
    } else if (neighborIds.has(n.id)) {
      return { id: n.id, color: { background: bg, border: bg },
               font: { size: n.font.size, color: '#ffffff' } };
    } else {
      return { id: n.id, color: { background: dimColor(bg, 0.15), border: dimColor(bg, 0.15) },
               font: { size: 0, color: 'transparent' } };
    }
  });
  nodesDS.update(updates);
}

function clearHighlight() {
  selectedNodeId = null;
  const updates = RAW_NODES.map(n => ({
    id: n.id, color: n.color, font: n.font, shadow: { enabled: false }, borderWidth: 1.5, size: n.size,
  }));
  nodesDS.update(updates);
}

function focusNode(nodeId) {
  network.focus(nodeId, { scale: 0.9, animation: true });
  network.selectNodes([nodeId]);
  highlightNode(nodeId);
  showInfo(nodeId);
}

function blinkNode(nodeId) {
  const pos = network.getPositions([nodeId])[nodeId];
  if (pos) { network.moveTo({ position: { x: pos.x, y: pos.y }, animation: true }); }
  const n = nodesDS.get(nodeId);
  if (!n) return;
  const orig = RAW_NODES.find(r => r.id === nodeId);
  if (!orig) return;
  const blinkColor = '#F28E2B';
  let count = 0;
  const maxBlinks = 5;
  const interval = setInterval(() => {
    if (count >= maxBlinks * 2) {
      clearInterval(interval);
      nodesDS.update({ id: nodeId, color: orig.color, font: orig.font, shadow: { enabled: false }, borderWidth: 1.5, size: orig.size });
      return;
    }
    if (count %% 2 === 0) {
      nodesDS.update({ id: nodeId, color: { background: blinkColor, border: '#fff' },
        font: { size: 18, color: '#ffffff', strokeWidth: 3, strokeColor: '#000000' },
        shadow: { enabled: true, size: 20, color: 'rgba(242,142,43,0.6)' }, borderWidth: 3, size: orig.size * 1.5 });
    } else {
      nodesDS.update({ id: nodeId, color: orig.color, font: orig.font, shadow: { enabled: false }, borderWidth: 1.5, size: orig.size });
    }
    count++;
  }, 300);
}

// --- Path mode ---
let pathMode = false;
let pathSource = null;
const pathInfo = document.getElementById('path-info');
const pathCheck = document.getElementById('path-check');

function togglePathMode() {
  pathMode = pathCheck.checked;
  pathSource = null;
  if (pathMode) {
    pathInfo.innerHTML = 'Click a <b>source</b> node or search above...';
    clearHighlight();
  } else {
    pathInfo.innerHTML = '';
    clearHighlight();
  }
}

function bfsPath(srcId, tgtId) {
  // Build adjacency from edges dataset.
  const adj = {};
  RAW_EDGES.forEach(e => {
    if (!adj[e.from]) adj[e.from] = [];
    if (!adj[e.to]) adj[e.to] = [];
    adj[e.from].push(e.to);
    adj[e.to].push(e.from);
  });
  const visited = new Set([srcId]);
  const parent = {};
  const queue = [srcId];
  while (queue.length > 0) {
    const cur = queue.shift();
    if (cur === tgtId) break;
    for (const nb of (adj[cur] || [])) {
      if (!visited.has(nb)) {
        visited.add(nb);
        parent[nb] = cur;
        queue.push(nb);
      }
    }
  }
  if (!parent[tgtId] && srcId !== tgtId) return null;
  const path = [];
  let cur = tgtId;
  while (cur !== undefined) {
    path.unshift(cur);
    cur = parent[cur];
  }
  return path;
}

function highlightPath(path) {
  const pathSet = new Set(path);
  // Build set of edge pairs on the path.
  const pathEdges = new Set();
  for (let i = 0; i < path.length - 1; i++) {
    pathEdges.add(path[i] + '\x00' + path[i+1]);
    pathEdges.add(path[i+1] + '\x00' + path[i]);
  }
  const nodeUpdates = RAW_NODES.map(n => {
    const bg = n.color.background;
    if (pathSet.has(n.id)) {
      const isEndpoint = n.id === path[0] || n.id === path[path.length-1];
      return { id: n.id,
        color: { background: isEndpoint ? '#F28E2B' : '#59A14F', border: '#fff',
                 highlight: { background: '#fff', border: isEndpoint ? '#F28E2B' : '#59A14F' } },
        font: { size: 14, color: '#ffffff', strokeWidth: 3, strokeColor: '#000000' },
        shadow: { enabled: true, size: 20, color: isEndpoint ? 'rgba(242,142,43,0.5)' : 'rgba(89,161,79,0.4)' },
        borderWidth: isEndpoint ? 4 : 3, size: isEndpoint ? (n.size * 1.8) : (n.size * 1.4) };
    } else {
      return { id: n.id, color: { background: dimColor(bg, 0.1), border: dimColor(bg, 0.1) },
               font: { size: 0, color: 'transparent' } };
    }
  });
  nodesDS.update(nodeUpdates);
  // Highlight path edges.
  const edgeUpdates = RAW_EDGES.map((e, i) => {
    if (pathEdges.has(e.from + '\x00' + e.to)) {
      return { id: i, color: { color: '#F28E2B', opacity: 1.0 }, width: 4 };
    } else {
      return { id: i, color: { opacity: 0.05 }, width: 1 };
    }
  });
  edgesDS.update(edgeUpdates);
}

function clearPathHighlight() {
  clearHighlight();
  traceActive = false;
  const heading = document.getElementById('info-heading');
  heading.classList.remove('clickable');
  heading.title = '';
  const edgeUpdates = RAW_EDGES.map((e, i) => ({
    id: i, color: e.color, width: e.width,
    arrows: { to: { enabled: true, scaleFactor: 0.5 } },
  }));
  edgesDS.update(edgeUpdates);
}

function resetAll() {
  clearPathHighlight();
  pathSource = null;
  pathMode = false;
  pathCheck.checked = false;
  pathInfo.innerHTML = '';
  network.unselectAll();
  document.getElementById('info-content').innerHTML = '<span class="empty">Click a node to inspect it</span>';
}

function switchTab(tab) {
  document.querySelectorAll('.filter-tab').forEach(t => t.classList.remove('active'));
  document.querySelectorAll('.filter-panel').forEach(p => p.style.display = 'none');
  const tabs = { tags: 1, communities: 2, sources: 3 };
  const panels = { tags: 'tags-wrap', communities: 'legend-wrap', sources: 'sources-wrap' };
  document.querySelector('.filter-tab:nth-child('+tabs[tab]+')').classList.add('active');
  document.getElementById(panels[tab]).style.display = 'block';
}

function zoomIn() { const s = network.getScale(); network.moveTo({ scale: s * 1.4, animation: { duration: 200 } }); }
function zoomOut() { const s = network.getScale(); network.moveTo({ scale: s / 1.4, animation: { duration: 200 } }); }
function zoomFit() { network.fit({ animation: { duration: 300 } }); }

function traceToNode(targetId) {
  // Build reverse call graph: callee → [callers].
  // Also build a per-edge index of original caller→callee direction.
  const reverseAdj = {};
  const edgeDirection = {}; // edgeIndex → { caller, callee }
  RAW_EDGES.forEach((e, i) => {
    if (e.title && e.title.includes('calls')) {
      const caller = e._src || e.from;
      const callee = e._tgt || e.to;
      edgeDirection[i] = { caller, callee };
      if (!reverseAdj[callee]) reverseAdj[callee] = [];
      reverseAdj[callee].push(caller);
    }
  });

  // BFS backwards from target to find all callers.
  const visited = new Set([targetId]);
  // Directed set: caller→callee pairs in the trace tree.
  const traceEdgePairs = new Set();
  const queue = [targetId];
  const maxDepth = 10;
  const depthMap = {};
  depthMap[targetId] = 0;

  while (queue.length > 0) {
    const cur = queue.shift();
    if (depthMap[cur] >= maxDepth) continue;
    for (const caller of (reverseAdj[cur] || [])) {
      if (!visited.has(caller)) {
        visited.add(caller);
        depthMap[caller] = depthMap[cur] + 1;
        queue.push(caller);
      }
      // Store canonical pair (sorted) for matching against edges.
      const a = caller < cur ? caller : cur;
      const b = caller < cur ? cur : caller;
      traceEdgePairs.add(a + '\x00' + b);
    }
  }

  if (visited.size <= 1) {
    pathInfo.innerHTML = 'No callers found for this node.';
    return;
  }

  traceActive = true;
  traceTargetId = targetId;
  const heading = document.getElementById('info-heading');
  heading.classList.add('clickable');
  heading.title = 'Click to restore selected node view';

  // Highlight trace tree.
  const nodeUpdates = RAW_NODES.map(n => {
    const bg = n.color.background;
    if (n.id === targetId) {
      return { id: n.id,
        color: { background: '#E15759', border: '#fff', highlight: { background: '#fff', border: '#E15759' } },
        font: { size: 14, color: '#ffffff', strokeWidth: 3, strokeColor: '#000000' },
        shadow: { enabled: true, size: 25, color: 'rgba(225,87,89,0.5)' },
        borderWidth: 4, size: n.size * 1.8 };
    } else if (visited.has(n.id)) {
      const depth = depthMap[n.id] || 0;
      const alpha = Math.max(0.5, 1.0 - depth * 0.1);
      return { id: n.id,
        color: { background: 'rgba(118,183,178,'+alpha+')', border: 'rgba(118,183,178,'+alpha+')',
                 highlight: { background: '#fff', border: '#76B7B2' } },
        font: { size: 12, color: '#ffffff' },
        shadow: { enabled: true, size: 10, color: 'rgba(118,183,178,0.3)' },
        borderWidth: 2 };
    } else {
      return { id: n.id, color: { background: dimColor(bg, 0.08), border: dimColor(bg, 0.08) },
               font: { size: 0, color: 'transparent' } };
    }
  });
  nodesDS.update(nodeUpdates);

  const edgeUpdates = RAW_EDGES.map((e, i) => {
    // Check if this edge is in the trace tree.
    const a = e.from < e.to ? e.from : e.to;
    const b = e.from < e.to ? e.to : e.from;
    if (traceEdgePairs.has(a + '\x00' + b)) {
      // Set arrow direction: caller → callee (toward the target).
      const dir = edgeDirection[i];
      let arrows;
      if (dir) {
        // Arrow points from caller to callee: if edge.from === caller, arrow at 'to'; otherwise at 'from'.
        if (dir.caller === e.from) {
          arrows = { to: { enabled: true, scaleFactor: 0.8 }, from: { enabled: false } };
        } else {
          arrows = { from: { enabled: true, scaleFactor: 0.8 }, to: { enabled: false } };
        }
      } else {
        arrows = { to: { enabled: true, scaleFactor: 0.8 } };
      }
      return { id: i, color: { color: '#E15759', opacity: 0.8 }, width: 3, arrows: arrows };
    } else {
      return { id: i, color: { opacity: 0.03 }, width: 1 };
    }
  });
  edgesDS.update(edgeUpdates);

  const tn = nodesDS.get(targetId);
  pathInfo.innerHTML = 'Trace to <span class="path-node" style="color:#E15759">' + esc(tn ? tn.label : targetId) + '</span>: ' + (visited.size - 1) + ' callers found';
}

function handlePathClick(nodeId) {
  if (!pathSource) {
    pathSource = nodeId;
    const n = nodesDS.get(nodeId);
    pathInfo.innerHTML = 'Source: <span class="path-node" onclick="focusPathNode(\'' + nodeId.replace(/'/g, "\\'") + '\')">' + esc(n ? n.label : nodeId) + '</span><br>Click a <b>target</b> node or search above...';
    highlightNode(nodeId);
  } else {
    const src = pathSource;
    pathSource = null;
    const path = bfsPath(src, nodeId);
    if (!path) {
      const sn = nodesDS.get(src), tn = nodesDS.get(nodeId);
      pathInfo.innerHTML = 'No path between <span class="path-node" onclick="focusPathNode(\'' + src.replace(/'/g, "\\'") + '\')">' + esc(sn ? sn.label : src) + '</span> and <span class="path-node" onclick="focusPathNode(\'' + nodeId.replace(/'/g, "\\'") + '\')">' + esc(tn ? tn.label : nodeId) + '</span>';
      clearHighlight();
    } else {
      highlightPath(path);
      const labels = path.map(id => { const n = nodesDS.get(id); return '<span class="path-node" onclick="focusPathNode(\'' + id.replace(/'/g, "\\'") + '\')">' + esc(n ? n.label : id) + '</span>'; });
      pathInfo.innerHTML = 'Path (' + path.length + ' nodes):<br>' + labels.join(' &rarr; ');
      // Show path in info panel too.
      showInfo(nodeId);
    }
  }
}

function focusPathNode(nodeId) {
  network.focus(nodeId, { scale: 0.9, animation: true });
  network.selectNodes([nodeId]);
  showInfo(nodeId);
}

let hoveredNodeId = null;
network.on('hoverNode', params => { hoveredNodeId = params.node; container.style.cursor = 'pointer'; });
network.on('blurNode', () => { hoveredNodeId = null; container.style.cursor = 'default'; });
container.addEventListener('click', () => {
  if (hoveredNodeId !== null) {
    if (pathMode) { handlePathClick(hoveredNodeId); }
    else { highlightNode(hoveredNodeId); network.selectNodes([hoveredNodeId]); showInfo(hoveredNodeId); }
  }
});
network.on('click', params => {
  if (params.nodes.length > 0) {
    if (pathMode) { handlePathClick(params.nodes[0]); }
    else { highlightNode(params.nodes[0]); showInfo(params.nodes[0]); }
  } else if (hoveredNodeId === null && !traceActive) {
    clearPathHighlight();
    pathSource = null;
    if (pathMode) pathInfo.innerHTML = 'Click a <b>source</b> node or search above...';
    document.getElementById('info-content').innerHTML = '<span class="empty">Click a node to inspect it</span>';
  }
});

const searchInput = document.getElementById('search');
const searchResults = document.getElementById('search-results');
searchInput.addEventListener('input', () => {
  const q = searchInput.value.toLowerCase().trim();
  searchResults.innerHTML = '';
  if (!q) { searchResults.style.display = 'none'; return; }
  const matches = RAW_NODES.filter(n => {
    if (!n.label.toLowerCase().includes(q)) return false;
    if (communityFilterActive && !checkedCommunities.has(n.community)) return false;
    if (tagFilterActive && !(n.tags || []).some(t => checkedTags.has(t))) return false;
    if (sourceFilterActive && !checkedSources.has(n.source_file || '')) return false;
    return true;
  }).slice(0, 20);
  if (!matches.length) { searchResults.style.display = 'none'; return; }
  searchResults.style.display = 'block';
  matches.forEach(n => {
    const el = document.createElement('div');
    el.className = 'search-item';
    el.textContent = n.label;
    el.style.borderLeft = '3px solid '+n.color.background;
    el.style.paddingLeft = '8px';
    el.onclick = () => { network.focus(n.id, { scale: 0.9, animation: true }); searchResults.style.display = 'none'; searchInput.value = ''; if (pathMode) { handlePathClick(n.id); } else { network.selectNodes([n.id]); highlightNode(n.id); showInfo(n.id); } };
    searchResults.appendChild(el);
  });
});
document.addEventListener('click', e => { if (!searchResults.contains(e.target) && e.target !== searchInput) searchResults.style.display = 'none'; });

// --- Shared filter logic ---
const checkedTags = new Set();
let tagFilterActive = false;

function applyFilters() {
  const updates = RAW_NODES.map(n => {
    let hidden = false;
    if (communityFilterActive && !checkedCommunities.has(n.community)) {
      hidden = true;
    }
    if (!hidden && tagFilterActive) {
      const nodeTags = n.tags || [];
      hidden = !nodeTags.some(t => checkedTags.has(t));
    }
    if (!hidden && sourceFilterActive) {
      hidden = !checkedSources.has(n.source_file || '');
    }
    return { id: n.id, hidden };
  });
  nodesDS.update(updates);
}

// --- Tag filter ---
const allTags = {};
RAW_NODES.forEach(n => { (n.tags || []).forEach(t => { allTags[t] = (allTags[t] || 0) + 1; }); });
const tagNames = Object.keys(allTags).sort();

function toggleAllTags(check) {
  checkedTags.clear();
  if (check) tagNames.forEach(t => checkedTags.add(t));
  tagFilterActive = check;
  document.querySelectorAll('#tags-list input[type=checkbox]').forEach(cb => { cb.checked = check; });
  applyFilters();
}

const tagsListEl = document.getElementById('tags-list');
if (tagNames.length === 0) {
  tagsListEl.innerHTML = '<div style="color:#555;font-size:12px;font-style:italic">No tagged nodes</div>';
} else {
  tagNames.forEach(tag => {
    const label = document.createElement('label');
    label.className = 'tag-check';
    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.onchange = () => {
      if (cb.checked) checkedTags.add(tag); else checkedTags.delete(tag);
      tagFilterActive = checkedTags.size > 0;
      applyFilters();
    };
    label.appendChild(cb);
    label.appendChild(document.createTextNode(tag));
    const count = document.createElement('span');
    count.className = 'tag-count';
    count.textContent = allTags[tag];
    label.appendChild(count);
    tagsListEl.appendChild(label);
  });
}

// --- Community filter (checkbox-based, like tags) ---
const checkedCommunities = new Set();
let communityFilterActive = false;

function toggleAllCommunities(check) {
  checkedCommunities.clear();
  if (check) LEGEND.forEach(c => checkedCommunities.add(c.cid));
  communityFilterActive = check;
  document.querySelectorAll('#legend input[type=checkbox]').forEach(cb => { cb.checked = check; });
  applyFilters();
}

const legendEl = document.getElementById('legend');
LEGEND.forEach(c => {
  const label = document.createElement('label');
  label.className = 'legend-item';
  label.dataset.label = c.label.toLowerCase();
  const cb = document.createElement('input');
  cb.type = 'checkbox';
  cb.onchange = () => {
    if (cb.checked) checkedCommunities.add(c.cid); else checkedCommunities.delete(c.cid);
    communityFilterActive = checkedCommunities.size > 0;
    applyFilters();
  };
  label.appendChild(cb);
  const dot = document.createElement('div');
  dot.className = 'legend-dot';
  dot.style.background = c.color;
  label.appendChild(dot);
  const lbl = document.createElement('span');
  lbl.className = 'legend-label';
  lbl.textContent = c.label;
  label.appendChild(lbl);
  const cnt = document.createElement('span');
  cnt.className = 'legend-count';
  cnt.textContent = c.count;
  label.appendChild(cnt);
  legendEl.appendChild(label);
});

// Community search filter.
document.getElementById('community-search').addEventListener('input', function() {
  const q = this.value.toLowerCase().trim();
  document.querySelectorAll('#legend .legend-item').forEach(item => {
    item.style.display = (!q || item.dataset.label.includes(q)) ? 'flex' : 'none';
  });
});

// --- Source file filter ---
const checkedSources = new Set();
let sourceFilterActive = false;
const allSources = {};
RAW_NODES.forEach(n => { const sf = n.source_file || ''; if (sf) allSources[sf] = (allSources[sf] || 0) + 1; });
const sourceNames = Object.keys(allSources).sort();

function toggleAllSources(check) {
  checkedSources.clear();
  if (check) sourceNames.forEach(s => checkedSources.add(s));
  sourceFilterActive = check;
  document.querySelectorAll('#sources-list input[type=checkbox]').forEach(cb => { cb.checked = check; });
  applyFilters();
}

const sourcesListEl = document.getElementById('sources-list');
if (sourceNames.length === 0) {
  sourcesListEl.innerHTML = '<div style="color:#555;font-size:12px;font-style:italic">No source files</div>';
} else {
  sourceNames.forEach(sf => {
    const label = document.createElement('label');
    label.className = 'source-check';
    label.dataset.label = sf.toLowerCase();
    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.onchange = () => {
      if (cb.checked) checkedSources.add(sf); else checkedSources.delete(sf);
      sourceFilterActive = checkedSources.size > 0;
      applyFilters();
    };
    label.appendChild(cb);
    const nameSpan = document.createElement('span');
    nameSpan.className = 'source-name';
    nameSpan.textContent = sf;
    nameSpan.title = sf;
    label.appendChild(nameSpan);
    const count = document.createElement('span');
    count.className = 'source-count';
    count.textContent = allSources[sf];
    label.appendChild(count);
    sourcesListEl.appendChild(label);
  });
}

// Source search filter.
document.getElementById('source-search').addEventListener('input', function() {
  const q = this.value.toLowerCase().trim();
  document.querySelectorAll('#sources-list .source-check').forEach(item => {
    item.style.display = (!q || item.dataset.label.includes(q)) ? 'flex' : 'none';
  });
});
</script>`, nodesJSON, edgesJSON, legendJSON)
	// Escape </ sequences inside <script> to prevent HTML parser from
	// prematurely closing the script block. Preserve the closing </script> tag.
	result = strings.ReplaceAll(result, "</", `<\/`)
	result = strings.Replace(result, `<\/script>`, "</script>", 1)
	return result
}

func jsSafe(v any) string {
	data, _ := json.Marshal(v)
	return strings.ReplaceAll(string(data), "</", "<\\/")
}

func attrStrSlice(attrs map[string]any, key string) []string {
	v, ok := attrs[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		var result []string
		for _, item := range t {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func attrStr(attrs map[string]any, key, fallback string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

func nodeCommunityMap(communities map[int][]string) map[string]int {
	m := make(map[string]int)
	for cid, nodes := range communities {
		for _, n := range nodes {
			m[n] = cid
		}
	}
	return m
}
