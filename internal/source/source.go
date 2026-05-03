// Package source resolves input paths (directories, archives, git URLs)
// into local directories suitable for the analysis pipeline.
// Archives and git clones are cached under ~/.gfy/ for fast re-runs.
package source

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// gitAuth returns the appropriate transport.AuthMethod for a git URL.
// For SSH URLs: uses the SSH agent (same keys as git cli).
// For HTTPS URLs: uses git credential helper (same credentials as git cli).
func gitAuth(url string) transport.AuthMethod {
	if isSSHURL(url) {
		conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
		if err != nil {
			return nil
		}
		sshAgent := agent.NewClient(conn)
		auth := &gitssh.PublicKeysCallback{
			User: "git",
			Callback: func() ([]ssh.Signer, error) {
				return sshAgent.Signers()
			},
			HostKeyCallbackHelper: gitssh.HostKeyCallbackHelper{
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			},
		}
		return auth
	}

	// HTTPS: use git credential helper.
	creds := gitCredentialFill(url)
	if creds != nil {
		return &http.BasicAuth{
			Username: creds.username,
			Password: creds.password,
		}
	}
	return nil
}

type gitCreds struct {
	username string
	password string
}

// gitCredentialFill calls `git credential fill` to get stored credentials.
func gitCredentialFill(url string) *gitCreds {
	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("url=%s\n\n", url))
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	creds := &gitCreds{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			switch k {
			case "username":
				creds.username = v
			case "password":
				creds.password = v
			}
		}
	}
	if creds.username == "" && creds.password == "" {
		return nil
	}
	return creds
}

// isSSHURL returns true if the URL uses SSH transport.
func isSSHURL(url string) bool {
	return strings.HasPrefix(url, "git@") ||
		strings.HasPrefix(url, "ssh://") ||
		strings.HasPrefix(url, "git+ssh://")
}

// Info holds the resolved source and output paths.
type Info struct {
	SourceDir string // local directory to analyze
	OutDir    string // where to write graphify output
}

// Resolve examines inputPath and returns resolved local paths.
//   - Local directory → use as-is
//   - Archive (.zip, .tar, .tar.gz, .tgz) → extract to ~/.gfy/archive/<hash>/
//   - Git URL → clone to ~/.gfy/git/<hash>/
//
// outFlag overrides the default output directory when non-empty.
func Resolve(inputPath, outFlag string) (*Info, error) {
	switch {
	case IsGitURL(inputPath):
		return resolveGit(inputPath, outFlag)
	case IsArchive(inputPath):
		return resolveArchive(inputPath, outFlag)
	default:
		return resolveDir(inputPath, outFlag)
	}
}

func resolveDir(inputPath, outFlag string) (*Info, error) {
	absPath, err := filepath.Abs(inputPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	outDir := filepath.Join(absPath, ".gfy-out")
	if outFlag != "" {
		outDir, err = filepath.Abs(outFlag)
		if err != nil {
			return nil, fmt.Errorf("resolve output path: %w", err)
		}
	}
	return &Info{SourceDir: absPath, OutDir: outDir}, nil
}

func resolveArchive(inputPath, outFlag string) (*Info, error) {
	absPath, err := filepath.Abs(inputPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	hash, err := hashFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("hash archive: %w", err)
	}

	cacheDir := filepath.Join(cacheBase(), "archive", hash)
	sourceDir := cacheDir

	// Check for cache hit.
	if info, err := os.Stat(cacheDir); err == nil && info.IsDir() {
		fmt.Printf("Using cached archive: %s\n", cacheDir)
	} else {
		fmt.Printf("Extracting archive to %s...\n", cacheDir)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, fmt.Errorf("create cache dir: %w", err)
		}
		if err := extractArchive(absPath, cacheDir); err != nil {
			os.RemoveAll(cacheDir)
			return nil, fmt.Errorf("extract archive: %w", err)
		}
	}

	// Unwrap single top-level directory.
	sourceDir = unwrapSingleDir(sourceDir)

	outDir := filepath.Join(sourceDir, ".gfy-out")
	if outFlag != "" {
		outDir, err = filepath.Abs(outFlag)
		if err != nil {
			return nil, fmt.Errorf("resolve output path: %w", err)
		}
	}
	return &Info{SourceDir: sourceDir, OutDir: outDir}, nil
}

func resolveGit(inputPath, outFlag string) (*Info, error) {
	url := normalizeGitURL(inputPath)
	hash := hashString(url)
	cacheDir := filepath.Join(cacheBase(), "git", hash)

	// Check for existing clone.
	if repo, err := git.PlainOpen(cacheDir); err == nil {
		fmt.Printf("Updating cached clone: %s\n", cacheDir)
		w, err := repo.Worktree()
		if err == nil {
			pullErr := w.Pull(&git.PullOptions{Depth: 1, Auth: gitAuth(url)})
			if pullErr != nil && pullErr != git.NoErrAlreadyUpToDate {
				fmt.Printf("  Warning: pull failed: %v\n", pullErr)
			}
		}
	} else {
		fmt.Printf("Cloning %s to %s...\n", url, cacheDir)
		if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
			return nil, fmt.Errorf("create cache dir: %w", err)
		}
		_, err := git.PlainClone(cacheDir, false, &git.CloneOptions{
			URL:           url,
			Depth:         1,
			SingleBranch:  true,
			ReferenceName: plumbing.HEAD,
			Auth:          gitAuth(url),
		})
		if err != nil {
			os.RemoveAll(cacheDir)
			return nil, fmt.Errorf("git clone: %w", err)
		}
	}

	var outDir string
	var err error
	outDir = filepath.Join(cacheDir, ".gfy-out")
	if outFlag != "" {
		outDir, err = filepath.Abs(outFlag)
		if err != nil {
			return nil, fmt.Errorf("resolve output path: %w", err)
		}
	}
	return &Info{SourceDir: cacheDir, OutDir: outDir}, nil
}

// IsArchive returns true if the path has a recognized archive extension.
func IsArchive(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".tar") ||
		strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz")
}

// IsGitURL returns true if the string looks like a git remote URL.
func IsGitURL(s string) bool {
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "git://") ||
		strings.HasPrefix(lower, "ssh://") {
		return true
	}
	// user@host:path format
	if strings.Contains(s, "@") && strings.Contains(s, ":") && !strings.Contains(s, "://") {
		return true
	}
	return false
}

// ResolveForBranch clones a specific branch of a repository into a
// cached directory under ~/.gfy/compare/<hash>/<branch>/.
// The repoPath can be a local directory or git URL.
func ResolveForBranch(repoPath, branch, outFlag string) (*Info, error) {
	// Determine the repo URL for cloning.
	var repoURL string
	if IsGitURL(repoPath) {
		repoURL = normalizeGitURL(repoPath)
	} else {
		absPath, err := filepath.Abs(repoPath)
		if err != nil {
			return nil, fmt.Errorf("resolve path: %w", err)
		}
		repoURL = absPath
	}

	hash := hashString(repoURL)
	cacheDir := filepath.Join(cacheBase(), "compare", hash, branch)

	// Check for existing clone.
	if _, err := git.PlainOpen(cacheDir); err == nil {
		fmt.Printf("Using cached branch clone: %s\n", cacheDir)
		repo, err := git.PlainOpen(cacheDir)
		if err == nil {
			w, err := repo.Worktree()
			if err == nil {
				pullErr := w.Pull(&git.PullOptions{Depth: 1, Auth: gitAuth(repoURL)})
				if pullErr != nil && pullErr != git.NoErrAlreadyUpToDate {
					fmt.Printf("  Warning: pull failed: %v\n", pullErr)
				}
			}
		}
	} else {
		fmt.Printf("Cloning branch %q to %s...\n", branch, cacheDir)
		if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
			return nil, fmt.Errorf("create cache dir: %w", err)
		}
		_, err := git.PlainClone(cacheDir, false, &git.CloneOptions{
			URL:           repoURL,
			Depth:         1,
			SingleBranch:  true,
			ReferenceName: plumbing.NewBranchReferenceName(branch),
			Auth:          gitAuth(repoURL),
		})
		if err != nil {
			os.RemoveAll(cacheDir)
			return nil, fmt.Errorf("clone branch %s: %w", branch, err)
		}
	}

	var outDir string
	var err error
	outDir = filepath.Join(cacheDir, ".gfy-out")
	if outFlag != "" {
		outDir, err = filepath.Abs(outFlag)
		if err != nil {
			return nil, fmt.Errorf("resolve output path: %w", err)
		}
	}
	return &Info{SourceDir: cacheDir, OutDir: outDir}, nil
}

// --- internal helpers ---

func cacheBase() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gfy")
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16], nil // 16-char prefix is enough
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8]) // 16-char hex
}

func normalizeGitURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	return url
}

// unwrapSingleDir returns the inner directory if dir contains exactly
// one entry and that entry is a directory (common in archives like project-v1.0/).
func unwrapSingleDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		return dir
	}
	if entries[0].IsDir() {
		return filepath.Join(dir, entries[0].Name())
	}
	return dir
}

// extractArchive dispatches to the correct extractor based on file extension.
func extractArchive(archivePath, destDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", archivePath)
	}
}

func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)
		// Zip-slip protection.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("zip slip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTarReader(tar.NewReader(gz), destDir)
}

func extractTar(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTarReader(tar.NewReader(f), destDir)
}

func extractTarReader(tr *tar.Reader, destDir string) error {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, hdr.Name)
		// Zip-slip protection.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("tar slip: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}
