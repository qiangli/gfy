package compare

import (
	"fmt"
	"math"
	"strings"
)

// RoleDistributionSimilarity computes the cosine similarity between two trees'
// structural role distributions. Each node contributes a feature based on its
// (NodeType, tagSet, arityBucket) tuple. This captures the behavioral profile
// of a codebase independent of naming — "both projects have ~40% functions,
// ~30% that log, ~15% that do I/O" is a strong similarity signal that
// survives complete renames and refactors.
//
// Returns [0, 1] where 1.0 means identical role distributions.
func RoleDistributionSimilarity(a, b *ContainmentTree) float64 {
	vecA := buildRoleVector(a)
	vecB := buildRoleVector(b)
	return cosineSimStr(vecA, vecB)
}

// buildRoleVector builds a sparse feature vector keyed by role descriptors.
func buildRoleVector(ct *ContainmentTree) map[string]float64 {
	vec := make(map[string]float64)
	for _, node := range ct.AllNodes() {
		key := roleKey(node)
		vec[key]++
	}
	// Normalize to frequencies (proportions) so tree size doesn't dominate.
	total := float64(ct.Size)
	if total > 0 {
		for k := range vec {
			vec[k] /= total
		}
	}
	return vec
}

// roleKey builds a canonical string key for a node's functional role.
func roleKey(node *TreeNode) string {
	tagStr := "none"
	if len(node.Tags) > 0 {
		tagStr = strings.Join(node.Tags, "+") // Tags are pre-sorted
	}
	return fmt.Sprintf("%s:%s:a%s", node.NodeType, tagStr, arityBucket(node.Arity))
}

// arityBucket groups call arity into coarse buckets to allow approximate matching.
func arityBucket(arity int) string {
	switch {
	case arity == 0:
		return "0"
	case arity <= 2:
		return "1-2"
	case arity <= 5:
		return "3-5"
	default:
		return "6+"
	}
}

// cosineSimStr computes cosine similarity between two sparse float64 vectors.
func cosineSimStr(a, b map[string]float64) float64 {
	dot := 0.0
	normA := 0.0
	normB := 0.0

	for k, v := range a {
		normA += v * v
		if bv, ok := b[k]; ok {
			dot += v * bv
		}
	}
	for _, v := range b {
		normB += v * v
	}

	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)

	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (normA * normB)
}
