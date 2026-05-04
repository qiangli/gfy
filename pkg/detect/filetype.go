package detect

import "regexp"

// Extension sets for file classification.
var (
	CodeExtensions = map[string]bool{
		".py": true, ".ts": true, ".js": true, ".jsx": true, ".tsx": true, ".mjs": true, ".ejs": true,
		".go": true, ".rs": true, ".java": true,
		".cpp": true, ".cc": true, ".cxx": true, ".c": true, ".h": true, ".hpp": true,
		".rb": true, ".swift": true, ".kt": true, ".kts": true,
		".cs": true, ".scala": true, ".php": true,
		".lua": true, ".toc": true, ".zig": true, ".ps1": true,
		".ex": true, ".exs": true, ".m": true, ".mm": true, ".jl": true,
		".vue": true, ".svelte": true, ".dart": true, ".v": true, ".sv": true,
	}

	DocExtensions    = map[string]bool{".md": true, ".mdx": true, ".txt": true, ".rst": true, ".html": true, ".yaml": true, ".yml": true}
	PaperExtensions  = map[string]bool{".pdf": true}
	ImageExtensions  = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true}
	OfficeExtensions = map[string]bool{".docx": true, ".xlsx": true}
	VideoExtensions  = map[string]bool{
		".mp4": true, ".mov": true, ".webm": true, ".mkv": true, ".avi": true, ".m4v": true,
		".mp3": true, ".wav": true, ".m4a": true, ".ogg": true,
	}
)

// Corpus thresholds.
const (
	CorpusWarnThreshold  = 50_000  // words — below this, warn "may not need a graph"
	CorpusUpperThreshold = 500_000 // words — above this, warn about token cost
	FileCountUpper       = 200     // files — above this, warn about token cost
)

// Directories to skip during traversal.
var SkipDirs = map[string]bool{
	"venv": true, ".venv": true, "env": true, ".env": true,
	"node_modules": true, "__pycache__": true, ".git": true,
	"dist": true, "build": true, "target": true, "out": true,
	"site-packages": true, "lib64": true,
	".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true,
	".tox": true, ".eggs": true,
	".gfy-out": true,
}

// Lock and generated files to skip.
var SkipFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"Cargo.lock": true, "poetry.lock": true, "Gemfile.lock": true,
	"composer.lock": true, "go.sum": true, "go.work.sum": true,
}

// Patterns for files likely containing secrets.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|[\\/])\.(env|envrc)(\.|$)`),
	regexp.MustCompile(`(?i)\.(pem|key|p12|pfx|cert|crt|der|p8)$`),
	regexp.MustCompile(`(?i)(credential|secret|passwd|password|token|private_key)`),
	regexp.MustCompile(`(id_rsa|id_dsa|id_ecdsa|id_ed25519)(\.pub)?$`),
	regexp.MustCompile(`(?i)(\.netrc|\.pgpass|\.htpasswd)$`),
	regexp.MustCompile(`(?i)(aws_credentials|gcloud_credentials|service.account)`),
}

// Signals that a .md/.txt is an academic paper.
var paperSignals = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\barxiv\b`),
	regexp.MustCompile(`(?i)\bdoi\s*:`),
	regexp.MustCompile(`(?i)\babstract\b`),
	regexp.MustCompile(`(?i)\bproceedings\b`),
	regexp.MustCompile(`(?i)\bjournal\b`),
	regexp.MustCompile(`(?i)\bpreprint\b`),
	regexp.MustCompile(`\\cite\{`),
	regexp.MustCompile(`\[\d+\]`),
	regexp.MustCompile(`(?s)\[\n\d+\n\]`),
	regexp.MustCompile(`(?i)eq\.\s*\d+|equation\s+\d+`),
	regexp.MustCompile(`\d{4}\.\d{4,5}`),
	regexp.MustCompile(`(?i)\bwe propose\b`),
	regexp.MustCompile(`(?i)\bliterature\b`),
}

const paperSignalThreshold = 3

// Xcode asset catalog markers — PDFs inside these are vector icons, not papers.
var assetDirMarkers = map[string]bool{
	".imageset": true, ".xcassets": true, ".appiconset": true,
	".colorset": true, ".launchimage": true,
}
