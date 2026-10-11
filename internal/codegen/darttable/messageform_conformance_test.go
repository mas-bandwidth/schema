package darttable

// THE DART LEG'S MESSAGE-FORM CONFORMANCE (test/conformance/README.md, the
// `message` surface; docs/SPEC-TABLES.md §3.3).
//
// The driver at test/conformance/dart is the surface's AUTHOR, and this test
// holds the driver's claim to the manifest's own rows. The card's two clauses:
//
//   2.1 the driver answers `message` for every manifest row instead of ABSENT
//   2.2 the driver's row list is the manifest's, joined by the connection key
//
// This is the RED side. The Dart table backend here emits the FIXED form
// (form byte 3, §3.4) and the block and cook accelerators; the ID-TABLE wire
// (form 1) is schema#514 and the message form (form 2, §3.3) is its follow-on,
// so this backend carries none of AnnounceRead/LoadMessages/SaveMessages and
// the driver declares no `message` surface at all. Clause 2.1 fails at this
// commit for the reason it names; the port that lands the codec and wires the
// surface turns it green. Clause 2.2 runs and holds from the manifest alone.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dartMsgDriver and dartMsgManifest are repo-relative from this package's own
// directory, the way every other probe in this package reaches the tree.
const (
	dartMsgDriver   = "../../../test/conformance/dart/main.dart"
	dartMsgManifest = "../../../testdata/conformance/tables/MANIFEST.txt"
)

// TestMessageFormConformanceDart is STEP 2.1's one test, one subtest per
// clause. It is absent at the card's start commit.
func TestMessageFormConformanceDart(t *testing.T) {
	t.Parallel()

	driver := dartMsgReadFile(t, dartMsgDriver)
	manifest := dartMsgReadFile(t, dartMsgManifest)
	rows, connections := dartMsgManifestRows(t, manifest)
	byKey := map[string]bool{}
	for _, c := range connections {
		byKey[c[1]] = true
	}

	// "`message` | `message` | AnnounceRead the connection's announcement,
	// LoadMessages the message-form batch against it, SaveMessages, the bytes"
	// (test/conformance/README.md, the surfaces table), and "THE DRIVER ... its
	// `message` surface (this card)". The driver answers ABSENT by leaving the
	// surface out of `list`, which the harness prints for every message row.
	t.Run("2.1_driver_answers_message", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(driver, "'message'") {
			t.Fatalf("the Dart conformance driver declares no `message` surface, so it answers ABSENT (exit 2) for every one of the manifest's %d message rows: AnnounceRead/LoadMessages/SaveMessages of docs/SPEC-TABLES.md §3.3 are not wired", len(rows))
		}
	})

	// "The manifest's `connection` line names the announcement and the
	// `message` line names the pair of wires, so the driver joins the two by
	// key and never derives a vocabulary of its own." (test/conformance/
	// README.md, the `message` surface). This pins the driver's row list to the
	// manifest's: every message row resolves to a declared connection.
	t.Run("2.2_row_list_is_the_manifests", func(t *testing.T) {
		t.Parallel()
		if len(rows) == 0 {
			t.Fatal("the manifest names no message row, so this leg has nothing to answer")
		}
		for _, f := range rows {
			if !byKey[f[2]] {
				t.Errorf("message row %s names connection %s, and the manifest declares no `connection %s` line", f[1], f[2], f[2])
			}
		}
	})
}

// dartMsgReadFile reads one repo-relative file, run from this package's dir.
func dartMsgReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// dartMsgManifestRows splits the manifest's `message` and `connection` lines
// into fields. It skips blanks and comments the way every manifest reader does.
func dartMsgManifestRows(t *testing.T, manifest string) (messages, connections [][]string) {
	t.Helper()
	for _, raw := range strings.Split(manifest, "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		f := strings.Fields(text)
		switch f[0] {
		case "message":
			messages = append(messages, f)
		case "connection":
			connections = append(connections, f)
		}
	}
	return messages, connections
}
