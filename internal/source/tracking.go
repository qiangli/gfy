package source

import (
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// TrackingInfo holds the current branch and its remote tracking branch.
type TrackingInfo struct {
	LocalBranch  string // e.g., "feature-x"
	Remote       string // e.g., "origin"
	RemoteBranch string // e.g., "main"
	RemoteURL    string // e.g., "https://github.com/user/repo.git"
}

// RemoteURL returns the fetch URL for the named remote in the git repo at repoPath.
func RemoteURL(repoPath, remoteName string) (string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		return "", fmt.Errorf("cannot read git config: %w", err)
	}
	remoteCfg, exists := cfg.Remotes[remoteName]
	if !exists || len(remoteCfg.URLs) == 0 {
		return "", fmt.Errorf("remote %q not configured", remoteName)
	}
	return remoteCfg.URLs[0], nil
}

// DetectTracking opens the git repo at repoPath and determines:
// 1. Current HEAD branch
// 2. The configured remote tracking branch (from .git/config)
// 3. The remote URL for cloning
func DetectTracking(repoPath string) (*TrackingInfo, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("cannot read HEAD: %w", err)
	}
	if !head.Name().IsBranch() {
		return nil, fmt.Errorf("detached HEAD at %s; use --base to specify branch", head.Hash().String()[:8])
	}
	localBranch := head.Name().Short()

	cfg, err := repo.Config()
	if err != nil {
		return nil, fmt.Errorf("cannot read git config: %w", err)
	}

	branchCfg, exists := cfg.Branches[localBranch]
	if exists && branchCfg.Remote != "" {
		// Explicit upstream configured.
		remoteName := branchCfg.Remote
		remoteBranch := branchCfg.Merge.Short()

		remoteCfg, exists := cfg.Remotes[remoteName]
		if !exists || len(remoteCfg.URLs) == 0 {
			return nil, fmt.Errorf("remote %q not configured in git config", remoteName)
		}

		return &TrackingInfo{
			LocalBranch:  localBranch,
			Remote:       remoteName,
			RemoteBranch: remoteBranch,
			RemoteURL:    remoteCfg.URLs[0],
		}, nil
	}

	// No explicit upstream: fall back to "origin" remote with the same branch name,
	// or try common default branch names (main, master).
	remoteName := "origin"
	remoteCfg, exists := cfg.Remotes[remoteName]
	if !exists || len(remoteCfg.URLs) == 0 {
		return nil, fmt.Errorf("no upstream configured and no 'origin' remote found; use --base to specify branch")
	}

	// Try the current branch name first, then common defaults.
	remoteBranch := localBranch
	candidates := []string{localBranch, "main", "master"}
	for _, candidate := range candidates {
		// Check if the remote ref exists locally.
		refName := fmt.Sprintf("refs/remotes/%s/%s", remoteName, candidate)
		if _, err := repo.Reference(plumbing.ReferenceName(refName), true); err == nil {
			remoteBranch = candidate
			break
		}
	}

	return &TrackingInfo{
		LocalBranch:  localBranch,
		Remote:       remoteName,
		RemoteBranch: remoteBranch,
		RemoteURL:    remoteCfg.URLs[0],
	}, nil
}
