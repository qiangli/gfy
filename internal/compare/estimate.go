package compare

import (
	"fmt"
	"math"
)

// EstimatedRange represents a predicted similarity range for an uncomputed pair.
type EstimatedRange struct {
	LabelA string  `json:"label_a"`
	LabelB string  `json:"label_b"`
	Lower  float64 `json:"lower"`  // lower bound [0, 1]
	Upper  float64 `json:"upper"`  // upper bound [0, 1]
	Mid    float64 `json:"mid"`    // midpoint estimate
	Via    string  `json:"via"`    // which pivot(s) contributed the tightest bound
}

// metricScore extracts one of the 10 individual scores from a Result.
type metricScore struct {
	name     string
	weight   float64
	isMetric bool // true if 1-score satisfies triangle inequality
	extract  func(r *Result) float64
}

var allMetrics = []metricScore{
	{"NodeJaccard", 0.05, true, func(r *Result) float64 { return r.Similarity.NodeJaccard }},
	{"EdgeJaccard", 0.05, true, func(r *Result) float64 { return r.Similarity.EdgeJaccard }},
	{"DegreeSim", 0.05, true, func(r *Result) float64 { return 1 - r.Similarity.DegreeJSD }},
	{"CommunityNMI", 0.05, false, func(r *Result) float64 {
		if r.Similarity.CommunityNMI < 0 {
			return -1
		}
		return r.Similarity.CommunityNMI
	}},
	{"AHU", 0.15, true, func(r *Result) float64 {
		if r.Similarity.TreeScores == nil {
			return -1
		}
		return r.Similarity.TreeScores.AHUSubtreeMatch
	}},
	{"TED", 0.20, true, func(r *Result) float64 {
		if r.Similarity.TreeScores == nil {
			return -1
		}
		return r.Similarity.TreeScores.TreeEditDistSim
	}},
	{"MCS", 0.15, false, func(r *Result) float64 {
		if r.Similarity.TreeScores == nil {
			return -1
		}
		return r.Similarity.TreeScores.MaxCommonSubtree
	}},
	{"SubtreeFreq", 0.10, true, func(r *Result) float64 {
		if r.Similarity.TreeScores == nil {
			return -1
		}
		return r.Similarity.TreeScores.SubtreeFreqCos
	}},
	{"TreeKernel", 0.10, false, func(r *Result) float64 {
		if r.Similarity.TreeScores == nil {
			return -1
		}
		return r.Similarity.TreeScores.TreeKernelNorm
	}},
	{"AntiUnif", 0.10, false, func(r *Result) float64 {
		if r.Similarity.TreeScores == nil {
			return -1
		}
		return r.Similarity.TreeScores.AntiUnifCoverage
	}},
	{"RoleDist", 0.10, false, func(r *Result) float64 {
		if r.Similarity.TreeScores == nil {
			return -1
		}
		return r.Similarity.TreeScores.RoleDistribution
	}},
}

// EstimateSimilarity predicts the similarity range between two graphs
// using triangle inequality on existing pairwise results via shared pivots.
//
// Given known results for (pivot, A) and (pivot, B), for each metric
// that satisfies the triangle inequality:
//
//	|d(pivot,A) - d(pivot,B)| ≤ d(A,B) ≤ d(pivot,A) + d(pivot,B)
//
// where d = 1 - similarity. Multiple pivots tighten the bounds.
//
// For non-metric scores (MCS, NMI, Kernel, Anti-Unif), uses the range
// of known values as a heuristic bound.
func EstimateSimilarity(
	labelA, labelB string,
	pivotResults []PivotResult,
) *EstimatedRange {
	if len(pivotResults) == 0 {
		return &EstimatedRange{
			LabelA: labelA, LabelB: labelB,
			Lower: 0, Upper: 1, Mid: 0.5,
			Via: "no pivots",
		}
	}

	// For each metric, compute bounds from all pivots and take the tightest.
	type metricBound struct {
		lower, upper float64
		weight       float64
		active       bool
	}
	bounds := make([]metricBound, len(allMetrics))
	for i := range bounds {
		bounds[i] = metricBound{lower: 0, upper: 1, weight: allMetrics[i].weight, active: false}
	}

	bestVia := ""
	tightestRange := 2.0

	for _, pivot := range pivotResults {
		for i, m := range allMetrics {
			scorePA := m.extract(pivot.ResultPA)
			scorePB := m.extract(pivot.ResultPB)
			if scorePA < 0 || scorePB < 0 {
				continue // metric was skipped
			}
			bounds[i].active = true

			if m.isMetric {
				// Triangle inequality on distance d = 1 - s.
				dPA := 1 - scorePA
				dPB := 1 - scorePB
				lower := math.Abs(dPA - dPB)
				upper := dPA + dPB
				// Convert back to similarity bounds.
				simLower := 1 - upper
				simUpper := 1 - lower
				// Clamp to [0, 1].
				if simLower < 0 {
					simLower = 0
				}
				if simUpper > 1 {
					simUpper = 1
				}
				// Tighten: take the best bounds across all pivots.
				if simLower > bounds[i].lower {
					bounds[i].lower = simLower
				}
				if simUpper < bounds[i].upper {
					bounds[i].upper = simUpper
				}
			} else {
				// Non-metric heuristic: use min/max of known values as soft bounds.
				lo := math.Min(scorePA, scorePB)
				hi := math.Max(scorePA, scorePB)
				// Widen slightly since non-metric scores can exceed the range.
				lo = lo - 0.1
				hi = hi + 0.05
				if lo < 0 {
					lo = 0
				}
				if hi > 1 {
					hi = 1
				}
				if lo > bounds[i].lower {
					bounds[i].lower = lo
				}
				if hi < bounds[i].upper {
					bounds[i].upper = hi
				}
			}
		}

		// Track which pivot gave tightest composite range.
		var composLo, composHi, totalW float64
		for i, b := range bounds {
			if b.active {
				composLo += b.lower * allMetrics[i].weight
				composHi += b.upper * allMetrics[i].weight
				totalW += allMetrics[i].weight
			}
		}
		if totalW > 0 {
			rng := (composHi - composLo) / totalW
			if rng < tightestRange {
				tightestRange = rng
				bestVia = pivot.PivotLabel
			}
		}
	}

	// Compute composite bounds as weighted average of per-metric bounds.
	var compositeLower, compositeUpper, totalWeight float64
	for i, b := range bounds {
		if b.active {
			compositeLower += b.lower * allMetrics[i].weight
			compositeUpper += b.upper * allMetrics[i].weight
			totalWeight += allMetrics[i].weight
		}
	}

	if totalWeight > 0 {
		compositeLower /= totalWeight
		compositeUpper /= totalWeight
	}

	// Ensure lower ≤ upper after all the tightening.
	if compositeLower > compositeUpper {
		compositeLower, compositeUpper = compositeUpper, compositeLower
	}

	return &EstimatedRange{
		LabelA: labelA,
		LabelB: labelB,
		Lower:  math.Round(compositeLower*1000) / 1000,
		Upper:  math.Round(compositeUpper*1000) / 1000,
		Mid:    math.Round((compositeLower+compositeUpper)/2*1000) / 1000,
		Via:    bestVia,
	}
}

// PivotResult holds the known comparison results from a shared pivot graph
// to both target graphs.
type PivotResult struct {
	PivotLabel string
	ResultPA   *Result // pivot vs A
	ResultPB   *Result // pivot vs B
}

// EstimateFromNWay computes estimated ranges for all uncomputed pairs
// in an N-way comparison, using all other graphs as pivots.
func EstimateFromNWay(r *NWayResult) []EstimatedRange {
	n := len(r.Labels)
	var estimates []EstimatedRange

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			// Collect all pivots that have results with both i and j.
			var pivots []PivotResult
			for k := 0; k < n; k++ {
				if k == i || k == j {
					continue
				}
				rki := getPairResult(r, k, i)
				rkj := getPairResult(r, k, j)
				if rki != nil && rkj != nil {
					pivots = append(pivots, PivotResult{
						PivotLabel: r.Labels[k],
						ResultPA:   rki,
						ResultPB:   rkj,
					})
				}
			}
			if len(pivots) > 0 {
				est := EstimateSimilarity(r.Labels[i], r.Labels[j], pivots)
				estimates = append(estimates, *est)
			}
		}
	}
	return estimates
}

// FormatEstimate produces a human-readable string for an estimated range.
func FormatEstimate(e *EstimatedRange) string {
	return fmt.Sprintf("%s vs %s: estimated %.2f–%.2f (midpoint %.2f, via %s)",
		e.LabelA, e.LabelB, e.Lower, e.Upper, e.Mid, e.Via)
}

// getPairResult returns the pairwise result for (i, j) from the NWayResult,
// handling the upper-triangle storage (i < j).
func getPairResult(r *NWayResult, i, j int) *Result {
	if i == j {
		return nil
	}
	if i < j {
		return r.PairwiseResults[i][j]
	}
	return r.PairwiseResults[j][i]
}
