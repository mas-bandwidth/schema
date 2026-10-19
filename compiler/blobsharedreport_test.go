// A shared-blob count reaches the PUBLIC report (docs/SPEC-TABLES.md §2.5, §4,
// §17.1). The engine's tabletext.Report counts BlobShared, but the public
// TableReport beside it and both Silent() answers used to drop it, so a wire
// naming one blob from two slots read as a SILENT, clean read.
//
// Reverting any one of the three edits turns a named test red:
//   - internal/tabletext/value.go's Report.Silent() → TestBlobSharedReachesPublicReport/tabletext
//   - compiler/pack.go's TableReport.BlobShared/publicReport/Silent
//     → TestBlobSharedReachesPublicReport (Cook's report) and .../publicreport
//   - cmd/schema/main.go's printed counter → TestCookBlobSharedIsNotSilent.
package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestBlobSharedReachesPublicReport(t *testing.T) {
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
	wire, err := os.ReadFile(filepath.Join("..", "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}

	// Cook is the public path that reads a wire and hands back a TableReport:
	// the shared blob is packed as one node, so the read succeeds and the
	// extra naming has to reach the caller.
	_, _, report, err := c.Cook(u, "Catalog", wire, CookOptions{})
	if err != nil {
		t.Fatalf("Cook of the shared-blob wire: %v", err)
	}
	if report.BlobShared != 1 {
		t.Fatalf("TableReport.BlobShared = %d, want 1 (report %+v)", report.BlobShared, report)
	}
	if report.Silent() {
		t.Fatalf("a read that counted a shared blob is silent: %+v", report)
	}

	t.Run("publicreport", func(t *testing.T) {
		t.Parallel()

		// publicReport carries the engine's count to the public surface.
		pub := publicReport(tabletext.Report{BlobShared: 1})
		if pub.BlobShared != 1 {
			t.Fatalf("publicReport dropped BlobShared: %+v", pub)
		}
		if pub.Silent() {
			t.Fatalf("TableReport{BlobShared: 1} is silent: %+v", pub)
		}

		// Both Silent() answers see the engine's count.
		if (tabletext.Report{BlobShared: 1}).Silent() {
			t.Fatal("tabletext.Report{BlobShared: 1} is silent")
		}
		if (TableReport{BlobShared: 1}).Silent() {
			t.Fatal("TableReport{BlobShared: 1} is silent")
		}

		// A zero value of each is still silent.
		if !(tabletext.Report{}).Silent() {
			t.Fatal("a zero tabletext.Report is not silent")
		}
		if !(TableReport{}).Silent() {
			t.Fatal("a zero TableReport is not silent")
		}
	})
}
