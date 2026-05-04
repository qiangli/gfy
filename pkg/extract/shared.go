package extract

import (
	"strings"

	"github.com/qiangli/gfy/pkg/types"
)

// buildLabelIndex creates a map from normalized label to node ID.
func buildLabelIndex(nodes []types.Node) map[string]string {
	m := make(map[string]string, len(nodes))
	for _, n := range nodes {
		norm := strings.TrimRight(n.Label, "()")
		norm = strings.TrimLeft(norm, ".")
		if norm != "" {
			m[strings.ToLower(norm)] = n.ID
		}
	}
	return m
}

// resolveCall attempts to resolve a function call within the current file's nodes,
// or records it as an unresolved raw call for cross-file resolution.
func resolveCall(
	calleeName string,
	isMemberCall bool,
	callerNID string,
	labelToNID map[string]string,
	seenCallPairs map[[2]string]bool,
	edges *[]types.Edge,
	rawCalls *[]types.RawCall,
	strPath string,
	line int,
) {
	tgtNID, found := labelToNID[strings.ToLower(calleeName)]
	if found && tgtNID != callerNID {
		pair := [2]string{callerNID, tgtNID}
		if !seenCallPairs[pair] {
			seenCallPairs[pair] = true
			*edges = append(*edges, types.Edge{
				Source: callerNID, Target: tgtNID, Relation: "calls",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(line), Weight: 1.0,
			})
		}
	} else if !found {
		*rawCalls = append(*rawCalls, types.RawCall{
			CallerNID:    callerNID,
			Callee:       calleeName,
			IsMemberCall: isMemberCall,
			SourceFile:   strPath,
			SourceLoc:    SourceLoc(line),
		})
	}
}
