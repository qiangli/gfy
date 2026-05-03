package compare

import (
	"fmt"
	"strings"
)

// GenerateReport produces a markdown comparison report.
func GenerateReport(r *Result) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Graph Comparison Report: %s vs %s\n\n", r.Labels[0], r.Labels[1]))

	// Summary table.
	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Metric | " + r.Labels[0] + " | " + r.Labels[1] + " | Delta |\n")
	sb.WriteString("|--------|------|------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Nodes | %d | %d | %+d |\n",
		r.Summary.NodesA, r.Summary.NodesB, r.Summary.NodesB-r.Summary.NodesA))
	sb.WriteString(fmt.Sprintf("| Edges | %d | %d | %+d |\n",
		r.Summary.EdgesA, r.Summary.EdgesB, r.Summary.EdgesB-r.Summary.EdgesA))

	sb.WriteString(fmt.Sprintf("\n**Overall similarity:** %.2f (Jaccard on node sets)\n", r.Similarity.NodeJaccard))
	sb.WriteString(fmt.Sprintf("**Edge similarity:** %.2f (Jaccard on edge sets)\n", r.Similarity.EdgeJaccard))
	sb.WriteString(fmt.Sprintf("**Structural divergence:** %.4f (Jensen-Shannon divergence on degree distributions)\n", r.Similarity.DegreeJSD))
	if r.Similarity.CommunityNMI >= 0 {
		sb.WriteString(fmt.Sprintf("**Community alignment:** %.2f (Normalized Mutual Information)\n", r.Similarity.CommunityNMI))
	}
	sb.WriteString(fmt.Sprintf("**Approximate edit distance:** %d\n", r.Summary.ApproxGED))
	sb.WriteString(fmt.Sprintf("\n**Composite similarity: %.2f**\n", r.Summary.CompositeScore))

	// Tree scores.
	if r.Similarity.TreeScores != nil {
		ts := r.Similarity.TreeScores
		sb.WriteString("\n### Tree Comparison Scores\n\n")
		sb.WriteString("| Algorithm | Score | What it measures |\n")
		sb.WriteString("|-----------|-------|------------------|\n")
		sb.WriteString(fmt.Sprintf("| AHU Subtree Match | %.2f | Fraction of isomorphic subtrees |\n", ts.AHUSubtreeMatch))
		sb.WriteString(fmt.Sprintf("| Tree Edit Distance | %.2f | Structural edit similarity |\n", ts.TreeEditDistSim))
		sb.WriteString(fmt.Sprintf("| Max Common Subtree | %.2f | Largest shared structural fragment |\n", ts.MaxCommonSubtree))
		sb.WriteString(fmt.Sprintf("| Subtree Frequency | %.2f | Local pattern distribution |\n", ts.SubtreeFreqCos))
		sb.WriteString(fmt.Sprintf("| Tree Kernel | %.2f | Partial structural similarity |\n", ts.TreeKernelNorm))
		sb.WriteString(fmt.Sprintf("| Anti-Unification | %.2f | Shared structural template |\n", ts.AntiUnifCoverage))
		sb.WriteString(fmt.Sprintf("| Role Distribution | %.2f | Behavioral profile similarity |\n", ts.RoleDistribution))
		if ts.SemanticAHU {
			sb.WriteString("\n*Semantic AHU hashing enabled — tree metrics use NodeType + behavioral tags for rename-invariant comparison.*\n")
		}
	}

	// Alignment info.
	if r.Alignment != nil {
		sb.WriteString(fmt.Sprintf("\n**Structural alignment:** %d matched pairs (avg score %.2f), %d unmatched in %s, %d unmatched in %s\n",
			r.Alignment.MatchedCount, r.Alignment.AvgScore,
			r.Alignment.UnmatchedACount, r.Labels[0],
			r.Alignment.UnmatchedBCount, r.Labels[1]))
	}

	// Node changes.
	sb.WriteString("\n## Node Changes\n")

	if len(r.NodeDiff.Added) > 0 {
		sb.WriteString(fmt.Sprintf("\n### Added (%d nodes)\n\n", len(r.NodeDiff.Added)))
		sb.WriteString("| Entity | Type | File | Connections |\n")
		sb.WriteString("|--------|------|------|-------------|\n")
		for _, n := range r.NodeDiff.Added {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d |\n", n.Label, n.FileType, n.File, n.Degree))
		}
	} else {
		sb.WriteString("\n### Added\n\n(none)\n")
	}

	if len(r.NodeDiff.Removed) > 0 {
		sb.WriteString(fmt.Sprintf("\n### Removed (%d nodes)\n\n", len(r.NodeDiff.Removed)))
		sb.WriteString("| Entity | Type | File | Connections |\n")
		sb.WriteString("|--------|------|------|-------------|\n")
		for _, n := range r.NodeDiff.Removed {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d |\n", n.Label, n.FileType, n.File, n.Degree))
		}
	} else {
		sb.WriteString("\n### Removed\n\n(none)\n")
	}

	if len(r.NodeDiff.Modified) > 0 {
		sb.WriteString(fmt.Sprintf("\n### Modified (%d nodes)\n\n", len(r.NodeDiff.Modified)))
		sb.WriteString("| Entity | Change |\n")
		sb.WriteString("|--------|--------|\n")
		for _, n := range r.NodeDiff.Modified {
			changes := describeNodeChanges(n)
			sb.WriteString(fmt.Sprintf("| %s | %s |\n", n.Label, changes))
		}
	} else {
		sb.WriteString("\n### Modified\n\n(none)\n")
	}

	// Rename candidates.
	if len(r.Renames) > 0 {
		sb.WriteString(fmt.Sprintf("\n### Renamed/Moved (%d candidates)\n\n", len(r.Renames)))
		sb.WriteString("| Old | New | Confidence | Evidence |\n")
		sb.WriteString("|-----|-----|------------|----------|\n")
		for _, rc := range r.Renames {
			evidence := fmt.Sprintf("edit distance %d, %.0f%% neighbor overlap",
				rc.EditDistance, rc.NeighborOverlap*100)
			sb.WriteString(fmt.Sprintf("| %s | %s | %.2f | %s |\n",
				rc.OldLabel, rc.NewLabel, rc.Confidence, evidence))
		}
	}

	// Edge changes.
	sb.WriteString("\n## Edge Changes\n")
	sb.WriteString(fmt.Sprintf("\n- **Added:** %d edges\n", len(r.EdgeDiff.Added)))
	sb.WriteString(fmt.Sprintf("- **Removed:** %d edges\n", len(r.EdgeDiff.Removed)))
	sb.WriteString(fmt.Sprintf("- **Modified:** %d edges\n", len(r.EdgeDiff.Modified)))

	// Dependency drift.
	if len(r.Drift) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Dependency Drift (%d files)\n\n", len(r.Drift)))
		sb.WriteString("| File | Added Imports | Removed Imports |\n")
		sb.WriteString("|------|---------------|------------------|\n")
		for _, d := range r.Drift {
			added := strings.Join(d.AddedImports, ", ")
			removed := strings.Join(d.RemovedImports, ", ")
			if added == "" {
				added = "-"
			}
			if removed == "" {
				removed = "-"
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", d.SourceFile, added, removed))
		}
	}

	// Change impact.
	if len(r.Impact) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Change Impact (top %d)\n\n", len(r.Impact)))
		sb.WriteString("| Rank | Entity | Change | Affected |\n")
		sb.WriteString("|------|--------|--------|----------|\n")
		for i, entry := range r.Impact {
			sb.WriteString(fmt.Sprintf("| %d | %s | %s | %d transitive dependents |\n",
				i+1, entry.Label, entry.Change, entry.AffectedCount))
		}
	}

	// Community changes.
	if r.Communities != nil {
		sb.WriteString("\n## Community Changes\n\n")
		sb.WriteString(fmt.Sprintf("- **Communities in %s:** %d\n", r.Labels[0], r.Communities.CommunitiesA))
		sb.WriteString(fmt.Sprintf("- **Communities in %s:** %d\n", r.Labels[1], r.Communities.CommunitiesB))

		if len(r.Communities.Splits) > 0 {
			sb.WriteString(fmt.Sprintf("\n### Splits (%d)\n", len(r.Communities.Splits)))
			for i, s := range r.Communities.Splits {
				sb.WriteString(fmt.Sprintf("\n%d. Community of %d nodes split into %d groups\n",
					i+1, len(s.OldNodes), len(s.NewParts)))
			}
		}
		if len(r.Communities.Merges) > 0 {
			sb.WriteString(fmt.Sprintf("\n### Merges (%d)\n", len(r.Communities.Merges)))
			for i, m := range r.Communities.Merges {
				sb.WriteString(fmt.Sprintf("\n%d. %d groups merged into community of %d nodes\n",
					i+1, len(m.OldParts), len(m.NewNodes)))
			}
		}
		if len(r.Communities.Stable) > 0 {
			sb.WriteString(fmt.Sprintf("\n### Stable (%d communities with >50%% overlap)\n", len(r.Communities.Stable)))
			for _, s := range r.Communities.Stable {
				sb.WriteString(fmt.Sprintf("- %d nodes (%.0f%% overlap)\n", len(s.NodesA), s.Overlap*100))
			}
		}
	}

	// Interpretation section.
	sb.WriteString("\n## Interpretation\n\n")
	sb.WriteString(InterpretResults(r))

	return sb.String()
}

// InterpretResults generates a human-readable explanation of the comparison results.
func InterpretResults(r *Result) string {
	var sb strings.Builder
	composite := r.Summary.CompositeScore

	// Headline characterization.
	sb.WriteString("### Overall Assessment\n\n")
	displayPct := fmt.Sprintf("%.2f", composite*100)
	switch {
	case displayPct == "100.00":
		sb.WriteString("These two codebases are **identical** (100% composite similarity). ")
		sb.WriteString("No structural differences were detected.\n\n")
	case composite >= 0.95:
		fmt.Fprintf(&sb, "These two codebases are **nearly identical** (%s%% composite similarity). ", displayPct)
		sb.WriteString("The differences are minor — likely a small feature addition, bug fix, or routine maintenance.\n\n")
	case composite >= 0.80:
		fmt.Fprintf(&sb, "These two codebases are **highly similar** (%s%% composite similarity). ", displayPct)
		sb.WriteString("They share the same architecture with moderate changes — likely a feature branch or incremental release.\n\n")
	case composite >= 0.60:
		fmt.Fprintf(&sb, "These two codebases are **moderately similar** (%s%% composite similarity). ", displayPct)
		sb.WriteString("They share a common core but have diverged significantly — possibly a major refactor or parallel development.\n\n")
	case composite >= 0.35:
		fmt.Fprintf(&sb, "These two codebases are **loosely related** (%s%% composite similarity). ", displayPct)
		sb.WriteString("They share some structural patterns but differ substantially — possibly independent implementations of similar requirements.\n\n")
	default:
		fmt.Fprintf(&sb, "These two codebases are **structurally unrelated** (%s%% composite similarity). ", displayPct)
		sb.WriteString("Very little shared structure was found.\n\n")
	}

	// What changed.
	sb.WriteString("### What Changed\n\n")
	added := r.Summary.NodesAdded
	removed := r.Summary.NodesRemoved
	modified := r.Summary.NodesModified
	edgesAdded := r.Summary.EdgesAdded
	edgesRemoved := r.Summary.EdgesRemoved

	if added > 0 {
		avgEdgesPerNode := 0.0
		if added > 0 {
			avgEdgesPerNode = float64(edgesAdded) / float64(added)
		}
		fmt.Fprintf(&sb, "- **%d new entities** were added", added)
		if avgEdgesPerNode > 1 {
			fmt.Fprintf(&sb, ", bringing ~%.1f new relationships each on average", avgEdgesPerNode)
		}
		sb.WriteString(".\n")
	}
	if removed > 0 {
		fmt.Fprintf(&sb, "- **%d entities** were removed", removed)
		if edgesRemoved > 0 {
			fmt.Fprintf(&sb, " along with %d relationships", edgesRemoved)
		}
		sb.WriteString(".\n")
	}
	if modified > 0 {
		fmt.Fprintf(&sb, "- **%d existing entities** had attribute changes (degree shifts, tag changes, etc.) but still exist in both.\n", modified)
	}
	if len(r.Renames) > 0 {
		fmt.Fprintf(&sb, "- **%d rename candidate(s)** detected — entities that were likely renamed rather than deleted and re-created.\n", len(r.Renames))
	}
	if added == 0 && removed == 0 && modified == 0 {
		sb.WriteString("- No structural changes detected.\n")
	}

	// Graph-level observations.
	sb.WriteString("\n### Graph-Level Observations\n\n")
	if r.Similarity.DegreeJSD < 0.01 {
		sb.WriteString("- **Connectivity patterns are virtually identical** — no coupling shift detected.\n")
	} else if r.Similarity.DegreeJSD < 0.05 {
		sb.WriteString("- **Connectivity patterns are very similar** — minor coupling changes.\n")
	} else if r.Similarity.DegreeJSD < 0.15 {
		sb.WriteString("- **Moderate coupling shift** — the degree distribution changed noticeably, suggesting some restructuring of dependencies.\n")
	} else {
		sb.WriteString("- **Significant coupling shift** — the connectivity patterns diverged substantially, indicating major architectural changes.\n")
	}

	if r.Similarity.CommunityNMI >= 0 {
		if r.Similarity.CommunityNMI >= 0.95 {
			sb.WriteString("- **Module structure is preserved** — community detection finds the same groupings.\n")
		} else if r.Similarity.CommunityNMI >= 0.80 {
			sb.WriteString("- **Module structure is mostly preserved** — a few entities moved between communities.\n")
		} else if r.Similarity.CommunityNMI >= 0.60 {
			sb.WriteString("- **Module structure partially changed** — some reorganization of code across packages/modules.\n")
		} else {
			sb.WriteString("- **Module structure significantly changed** — major reorganization of code groupings.\n")
		}
	}

	if len(r.Drift) > 0 {
		totalAdded := 0
		totalRemoved := 0
		for _, d := range r.Drift {
			totalAdded += len(d.AddedImports)
			totalRemoved += len(d.RemovedImports)
		}
		if totalAdded > 0 || totalRemoved > 0 {
			fmt.Fprintf(&sb, "- **Dependency drift**: %d import(s) added, %d removed across %d file(s).\n",
				totalAdded, totalRemoved, len(r.Drift))
		}
	}

	// Tree-level observations.
	if r.Similarity.TreeScores != nil {
		ts := r.Similarity.TreeScores
		sb.WriteString("\n### Structural Hierarchy Observations\n\n")

		if ts.SubtreeFreqCos >= 0.99 && ts.TreeKernelNorm >= 0.99 {
			sb.WriteString("- **Organizational patterns are identical** — the same mix of file/class/function structures appears in both codebases. New code follows the same style.\n")
		} else if ts.SubtreeFreqCos >= 0.90 {
			sb.WriteString("- **Organizational patterns are very similar** — the distribution of structural shapes is nearly the same.\n")
		} else if ts.SubtreeFreqCos >= 0.70 {
			sb.WriteString("- **Organizational patterns diverged somewhat** — different structural shapes are appearing.\n")
		} else {
			sb.WriteString("- **Organizational patterns differ significantly** — the codebases use different code organization styles.\n")
		}

		if ts.TreeEditDistSim >= 0.95 {
			sb.WriteString("- **Containment hierarchy barely changed** — very few structural insertions/deletions in the file→class→function tree.\n")
		} else if ts.TreeEditDistSim >= 0.80 {
			sb.WriteString("- **Containment hierarchy mostly preserved** — moderate structural changes to the code organization.\n")
		} else if ts.TreeEditDistSim >= 0.60 {
			fmt.Fprintf(&sb, "- **Containment hierarchy changed notably** (%.0f%% tree edit similarity) — substantial reorganization of the code structure.\n", ts.TreeEditDistSim*100)
		} else {
			fmt.Fprintf(&sb, "- **Containment hierarchy diverged significantly** (%.0f%% tree edit similarity) — the file/class/function organization is very different.\n", ts.TreeEditDistSim*100)
		}

		if ts.AntiUnifCoverage >= 0.95 {
			fmt.Fprintf(&sb, "- **%.0f%% of the structure is shared** as a common template — nearly all code organization is present in both.\n", ts.AntiUnifCoverage*100)
		} else if ts.AntiUnifCoverage >= 0.70 {
			fmt.Fprintf(&sb, "- **%.0f%% of the structure is shared** as a common template.\n", ts.AntiUnifCoverage*100)
		} else {
			fmt.Fprintf(&sb, "- **Only %.0f%% of the structure is shared** as a common template — most of the organization differs.\n", ts.AntiUnifCoverage*100)
		}
	}

	return sb.String()
}

// GenerateNWayReport produces a markdown report for N-way comparison.
func GenerateNWayReport(r *NWayResult) string {
	var sb strings.Builder

	sb.WriteString("# N-Way Graph Comparison Report\n\n")

	// Similarity matrix.
	sb.WriteString("## Similarity Matrix (Composite Score)\n\n")
	sb.WriteString("|")
	for _, l := range r.Labels {
		sb.WriteString(fmt.Sprintf(" | %s", l))
	}
	sb.WriteString(" |\n|")
	for range r.Labels {
		sb.WriteString("------|")
	}
	sb.WriteString("------|\n")
	for i, l := range r.Labels {
		sb.WriteString(fmt.Sprintf("| **%s**", l))
		for j := range r.Labels {
			sb.WriteString(fmt.Sprintf(" | %.2f", r.Heatmap[i][j]))
		}
		sb.WriteString(" |\n")
	}

	// Core entities.
	sb.WriteString(fmt.Sprintf("\n## Core Entities (in all %d graphs)\n\n", len(r.Labels)))
	sb.WriteString(fmt.Sprintf("- **Nodes:** %d\n", r.Core.NodeCount))
	sb.WriteString(fmt.Sprintf("- **Edges:** %d\n", r.Core.EdgeCount))

	// Unique entities.
	sb.WriteString("\n## Unique Entities\n\n")
	sb.WriteString("| Graph | Unique Nodes | Unique Edges |\n")
	sb.WriteString("|-------|--------------|--------------|\n")
	for _, u := range r.Unique {
		sb.WriteString(fmt.Sprintf("| %s | %d | %d |\n", u.Label, u.NodeCount, u.EdgeCount))
	}

	// Pairwise summaries.
	sb.WriteString("\n## Pairwise Comparisons\n\n")
	n := len(r.Labels)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			res := r.PairwiseResults[i][j]
			if res == nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("### %s vs %s\n\n", r.Labels[i], r.Labels[j]))
			sb.WriteString(fmt.Sprintf("- Similarity: %.2f (Jaccard), JSD: %.4f\n",
				res.Similarity.NodeJaccard, res.Similarity.DegreeJSD))
			sb.WriteString(fmt.Sprintf("- Nodes: +%d / -%d / ~%d\n",
				res.Summary.NodesAdded, res.Summary.NodesRemoved, res.Summary.NodesModified))
			sb.WriteString(fmt.Sprintf("- Edges: +%d / -%d / ~%d\n\n",
				res.Summary.EdgesAdded, res.Summary.EdgesRemoved, res.Summary.EdgesModified))
		}
	}

	// Estimated ranges from triangle inequality.
	if len(r.Estimates) > 0 {
		sb.WriteString("## Estimated Similarity (via triangle inequality)\n\n")
		sb.WriteString("These ranges are computed from existing pairwise results without running the full comparison.\n")
		sb.WriteString("Metrics satisfying the triangle inequality (Jaccard, TED, AHU, JSD, SubtreeFreq) provide exact bounds;\n")
		sb.WriteString("non-metric scores (MCS, NMI, Kernel, Anti-Unif) contribute heuristic bounds.\n\n")
		sb.WriteString("| Pair | Lower | Upper | Midpoint | Best Pivot |\n")
		sb.WriteString("|------|-------|-------|----------|------------|\n")
		for _, e := range r.Estimates {
			sb.WriteString(fmt.Sprintf("| %s vs %s | %.2f | %.2f | %.2f | %s |\n",
				e.LabelA, e.LabelB, e.Lower, e.Upper, e.Mid, e.Via))
		}
	}

	return sb.String()
}

func describeNodeChanges(n NodeModification) string {
	var parts []string
	if n.DegreeOld != n.DegreeNew {
		parts = append(parts, fmt.Sprintf("degree %d → %d", n.DegreeOld, n.DegreeNew))
	}
	for field, vals := range n.Changes {
		parts = append(parts, fmt.Sprintf("%s changed", field))
		_ = vals // values available for detailed diff
	}
	if len(parts) == 0 {
		return "attributes changed"
	}
	return strings.Join(parts, ", ")
}
