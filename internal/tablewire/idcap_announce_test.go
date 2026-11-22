package tablewire

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// AnnounceSchema(n) has exactly n announced vocabulary entries, the arithmetic every
// port's announcement test leans on.
func TestAnnounceSchemaVocabularyCount(t *testing.T) {
	t.Parallel()
	for _, n := range []int{ir.TableVocabularyMaxEntries, ir.TableVocabularyMaxEntries + 1} {
		u := covModel(t, AnnounceSchema(n)).Unit
		if got := len(MessageVocabulary(u)); got != n {
			t.Fatalf("AnnounceSchema(%d) has %d announced vocabulary entries", n, got)
		}
	}
}

// THE LAWFUL TWIN'S GOLDEN (docs/SPEC-CONFORMANCE.md §7). The Go reference's
// Announce bytes for AnnounceSchema(4096), the largest vocabulary a writer may
// announce, are pinned by their length and SHA-256 in
// testdata/conformance/announce-cap/announce-4096.txt. A port's announcement
// test generates the same schema and must write exactly these bytes (and its
// reader must accept them); the 4097 schema must be refused. Regenerate with
// SCHEMA_UPDATE_ANNOUNCE_GOLDEN=1 go test ./internal/tablewire -run AnnounceGolden.
func TestAnnounceGoldenAtTheBound(t *testing.T) {
	t.Parallel()
	u := covModel(t, AnnounceSchema(ir.TableVocabularyMaxEntries)).Unit
	ann := Announce(u)
	sum := sha256.Sum256(ann)
	got := fmt.Sprintf("entries %d\nbytes %d\nsha256 %s\n", ir.TableVocabularyMaxEntries, len(ann), hex.EncodeToString(sum[:]))
	path := filepath.Join("..", "..", "testdata", "conformance", "announce-cap", "announce-4096.txt")
	if os.Getenv("SCHEMA_UPDATE_ANNOUNCE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("the 4096-entry announcement moved from its golden:\nwant %sgot  %s", want, got)
	}
}
