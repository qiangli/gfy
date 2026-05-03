package source

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestIsArchive(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"foo.zip", true},
		{"foo.ZIP", true},
		{"foo.tar", true},
		{"foo.tar.gz", true},
		{"foo.tgz", true},
		{"foo.TGZ", true},
		{"foo.go", false},
		{"foo.tar.bz2", false},
		{"/path/to/archive.zip", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsArchive(tt.path); got != tt.want {
			t.Errorf("IsArchive(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"https://github.com/user/repo", true},
		{"http://github.com/user/repo", true},
		{"git://github.com/user/repo.git", true},
		{"ssh://git@github.com/user/repo.git", true},
		{"git@github.com:user/repo.git", true},
		{"/local/path", false},
		{"./relative", false},
		{"foo.zip", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsGitURL(tt.s); got != tt.want {
			t.Errorf("IsGitURL(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestResolveDir(t *testing.T) {
	dir := t.TempDir()
	info, err := Resolve(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.SourceDir != dir {
		t.Errorf("SourceDir = %q, want %q", info.SourceDir, dir)
	}
	wantOut := filepath.Join(dir, ".gfy-out")
	if info.OutDir != wantOut {
		t.Errorf("OutDir = %q, want %q", info.OutDir, wantOut)
	}
}

func TestResolveDirWithOutFlag(t *testing.T) {
	dir := t.TempDir()
	outDir := t.TempDir()
	info, err := Resolve(dir, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.OutDir != outDir {
		t.Errorf("OutDir = %q, want %q", info.OutDir, outDir)
	}
}

func TestResolveZipArchive(t *testing.T) {
	// Create a small zip archive with a Go file inside a directory.
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	createTestZip(t, zipPath)

	info, err := Resolve(zipPath, "")
	if err != nil {
		t.Fatal(err)
	}

	// Should have extracted the content.
	if _, err := os.Stat(info.SourceDir); err != nil {
		t.Fatalf("SourceDir %q does not exist", info.SourceDir)
	}

	// Verify a file exists in the extracted dir.
	matches, _ := filepath.Glob(filepath.Join(info.SourceDir, "*.go"))
	if len(matches) == 0 {
		// Could be inside unwrapped subdir.
		matches, _ = filepath.Glob(filepath.Join(info.SourceDir, "**", "*.go"))
	}

	// Second resolve should be a cache hit (no re-extraction).
	info2, err := Resolve(zipPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if info2.SourceDir != info.SourceDir {
		t.Errorf("cache miss: got different SourceDir %q vs %q", info2.SourceDir, info.SourceDir)
	}

	// Cleanup.
	os.RemoveAll(filepath.Dir(info.SourceDir))
}

func TestResolveTarGzArchive(t *testing.T) {
	tarPath := filepath.Join(t.TempDir(), "test.tar.gz")
	createTestTarGz(t, tarPath)

	info, err := Resolve(tarPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(info.SourceDir); err != nil {
		t.Fatalf("SourceDir %q does not exist", info.SourceDir)
	}

	// Cleanup.
	os.RemoveAll(filepath.Dir(info.SourceDir))
}

func TestUnwrapSingleDir(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "project-v1")
	os.Mkdir(inner, 0o755)
	os.WriteFile(filepath.Join(inner, "main.go"), []byte("package main"), 0o644)

	got := unwrapSingleDir(dir)
	if got != inner {
		t.Errorf("unwrapSingleDir = %q, want %q", got, inner)
	}
}

func TestUnwrapSingleDirNoUnwrap(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b"), 0o644)

	got := unwrapSingleDir(dir)
	if got != dir {
		t.Errorf("unwrapSingleDir = %q, want %q (no unwrap)", got, dir)
	}
}

// --- test helpers ---

func createTestZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	fw, err := w.Create("main.go")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("package main\nfunc main() {}\n"))
	w.Close()
}

func createTestTarGz(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	content := []byte("package main\nfunc main() {}\n")
	tw.WriteHeader(&tar.Header{
		Name: "main.go",
		Size: int64(len(content)),
		Mode: 0o644,
	})
	tw.Write(content)
	tw.Close()
	gw.Close()
}
