package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.md")

	for _, content := range []string{"first", "second"} {
		if err := Write(path, []byte(content)); err != nil {
			t.Fatal(err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != content {
			t.Fatalf("content = %q, want %q", got, content)
		}
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
