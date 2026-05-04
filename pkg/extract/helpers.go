// Package extract performs deterministic structural extraction from source code using tree-sitter.
package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qiangli/gfy/pkg/types"
)

var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// readFileBytes reads a file and returns its contents.
func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// MakeID builds a stable node ID from one or more name parts.
func MakeID(parts ...string) string {
	var filtered []string
	for _, p := range parts {
		p = strings.Trim(p, "_.")
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	combined := strings.Join(filtered, "_")
	cleaned := nonAlphaNum.ReplaceAllString(combined, "_")
	return strings.ToLower(strings.Trim(cleaned, "_"))
}

// FileStem returns a stem qualified with the parent directory name to avoid
// ID collisions when multiple files share the same filename in different directories.
func FileStem(path string) string {
	dir := filepath.Dir(path)
	parent := filepath.Base(dir)
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if parent != "" && parent != "." {
		return parent + "." + stem
	}
	return stem
}

// ReadText extracts a substring from source bytes given start and end byte offsets.
func ReadText(source []byte, startByte, endByte uint32) string {
	if int(startByte) >= len(source) {
		return ""
	}
	end := int(endByte)
	if end > len(source) {
		end = len(source)
	}
	return string(source[startByte:end])
}

// SourceLoc formats a line number as "L{line}" (1-based).
func SourceLoc(line int) string {
	return fmt.Sprintf("L%d", line)
}

// TagNode adds a behavioral tag to a node identified by nid.
// Duplicate tags are ignored.
func TagNode(nodes []types.Node, nid, tag string) {
	for i := range nodes {
		if nodes[i].ID == nid {
			for _, t := range nodes[i].Tags {
				if t == tag {
					return
				}
			}
			nodes[i].Tags = append(nodes[i].Tags, tag)
			return
		}
	}
}

// AddLogMessage appends a log message string to a node.
func AddLogMessage(nodes []types.Node, nid, msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	for i := range nodes {
		if nodes[i].ID == nid {
			nodes[i].LogMessages = append(nodes[i].LogMessages, msg)
			return
		}
	}
}

// AddThrowMessage appends a throw/panic/raise message string to a node.
func AddThrowMessage(nodes []types.Node, nid, msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	for i := range nodes {
		if nodes[i].ID == nid {
			nodes[i].ThrowMessages = append(nodes[i].ThrowMessages, msg)
			return
		}
	}
}

// SetComment sets the comment/docstring on a node identified by nid.
// Only sets if the node's comment is currently empty.
func SetComment(nodes []types.Node, nid, comment string) {
	comment = cleanComment(comment)
	if comment == "" {
		return
	}
	for i := range nodes {
		if nodes[i].ID == nid {
			if nodes[i].Comment == "" {
				nodes[i].Comment = comment
				TagNode(nodes, nid, "comment")
			}
			return
		}
	}
}

const maxCommentLen = 500

// cleanComment strips comment markers and trims whitespace.
func cleanComment(text string) string {
	text = strings.TrimSpace(text)
	// Strip block comment markers.
	text = strings.TrimPrefix(text, "/**")
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSuffix(text, "*/")
	// Strip Python triple-quote docstrings.
	for _, q := range []string{`"""`, `'''`} {
		text = strings.TrimPrefix(text, q)
		text = strings.TrimSuffix(text, q)
	}
	// Process line-by-line: strip leading //, #, ///, *, prefixes.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "///")
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "*")
		lines[i] = strings.TrimSpace(line)
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	// Remove leading/trailing empty lines.
	text = strings.Trim(text, "\n")
	if len(text) > maxCommentLen {
		text = text[:maxCommentLen]
	}
	return text
}
