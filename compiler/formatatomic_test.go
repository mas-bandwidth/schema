// `schema fmt` REWRITES AN AUTHOR'S FILE THROUGH A TEMPORARY AND A RENAME
// (FRONTEND-11): a full disk or a killed process must leave the original file
// whole, never truncated. Writing in place cannot promise that; the witness
// here is a directory where no temporary can be created — the in-place write
// would still succeed there, the atomic one refuses and leaves the file as it
// was.
package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

const unformattedSource = "package p\ntype T {\n  a int32\n}\n"

func TestFormatFileWritesThroughATemporary(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes through a read-only directory: the atomic rewrite cannot be refused")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "T.schema")
	if err := os.WriteFile(path, []byte(unformattedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	for name, rewrite := range map[string]func(string) ([]byte, bool, error){"FormatFile": FormatFile, "MigrateFile": MigrateFile} {
		if _, _, err := rewrite(path); err == nil {
			t.Errorf("%s rewrote the file in place in a directory that holds no temporary", name)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != unformattedSource {
			t.Errorf("%s changed the file without a temporary:\n%s", name, got)
		}
	}
}

func TestFormatFileKeepsModeAndLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "T.schema")
	if err := os.WriteFile(path, []byte(unformattedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := FormatFile(path); err != nil || !rewrote {
		t.Fatalf("FormatFile: rewrote=%v err=%v", rewrote, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries after a rewrite, want the file alone", len(entries))
	}
}
