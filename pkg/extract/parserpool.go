package extract

import (
	"sync"

	ts "github.com/odvcencio/gotreesitter"
)

// parserPools caches a ParserPool per language to avoid creating a new
// Parser (with its large lookup tables) for every file. This is the
// primary fix for OOM on large codebases.
var (
	parserPoolsMu sync.Mutex
	parserPools   = make(map[*ts.Language]*ts.ParserPool)
)

// getParserPool returns a shared, concurrency-safe ParserPool for the
// given language. Parsers are reused across files, dramatically reducing
// memory allocation.
func getParserPool(lang *ts.Language) *ts.ParserPool {
	parserPoolsMu.Lock()
	defer parserPoolsMu.Unlock()
	if pool, ok := parserPools[lang]; ok {
		return pool
	}
	pool := ts.NewParserPool(lang)
	parserPools[lang] = pool
	return pool
}
