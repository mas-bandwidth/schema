// A shared blob refuses the read (docs/SPEC-TABLES.md §2.5, §6.5): one blob
// record named from two slots is input no writer produces, and the public
// path that reads a wire (Cook) answers the refusal, never a cook of it. The
// accepted twin is the corpus's blob_small, every blob named once.
package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestBlobSharedRefusesOnThePublicPath(t *testing.T) {
	t.Parallel()

	// The unit MANIFEST.txt's `unit blobdemo` line names, reached from the
	// compiler package by walking up one directory.
	paths, err := filepath.Glob(filepath.Join("..", "tables", "blobs", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schema under ../tables/blobs: %v", err)
	}
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("load tables/blobs: %v", err)
	}
	read := func(name string) []byte {
		t.Helper()
		wire, err := os.ReadFile(filepath.Join("..", "testdata", "wire", "tables", name))
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}

	out, _, report, err := c.Cook(u, "Catalog", read("blob_shared.bin"), CookOptions{})
	if err == nil || !tablewire.ReadRefusal(err) || tablewire.RefusalReason(err) != tablewire.ReasonBlobShared {
		t.Fatalf("Cook of the shared-blob wire: %v, want a blob_shared refusal", err)
	}
	if len(out) != 0 || !report.Refused || report.Unknown != 0 || report.Widened != 0 {
		t.Fatalf("a refused read cooked %d bytes with report %+v", len(out), report)
	}

	out, _, report, err = c.Cook(u, "Catalog", read("blob_small.bin"), CookOptions{})
	if err != nil || len(out) == 0 || !report.Silent() {
		t.Fatalf("Cook of blob_small: %d bytes, %+v, %v", len(out), report, err)
	}
}
