package gotable

// A HASH THE LINEAGE DOES NOT HOLD IS layout_unknown; layout_newer IS RESERVED
// (docs/SPEC-TABLES.md §21.3, docs/FIXED-FORM-ALGORITHM.md §5.3 step 5).
//
// The refusal used to say layout_newer, "ship the reader", for every header
// hash outside the lineage. A flipped hash byte, an older layout nobody ever
// locked and a newer writer's layout all read that way, and the first two sent
// the operator after a newer reader that does not exist. A build holds one
// ORDERED fact about layouts, its lineage (oldest first, its own layout last,
// a retired mark meaning older), and the wire carries a digest with no ordinal,
// so nothing proves a hash NEWER and the rule is: a hash in the lineage reads
// (or is layout_unsupported below the floor), a hash outside it is
// layout_unknown, and layout_newer is never emitted by the Go fixed load.
//
// THE CASES (each is a generated unit compiled and run in a t.TempDir(): no
// socket, no clock, no outside service):
//
//   - a record with one hash byte flipped, one case per byte of the eight;
//   - an image of an OLDER layout that was never locked (the reader holds no
//     lineage), and of a newer writer's layout the reader's lock never saw;
//   - the same strangers read by a build that DOES hold a lineage: a hash the
//     lineage holds still reads, a retired one is layout_unsupported, and the
//     strangers are still layout_unknown, so the lineage is the proof and the
//     proof is only ever of "older" or "equal";
//   - the legitimate value (the reader's own file) reads clean, so the refusals
//     are the hash and not the fixture.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const (
	unknownHashOlder  = "package probe\nfixed table Point {\n    x int32 = 1\n}\n"
	unknownHashReader = "package probe\nfixed table Point {\n    x int32 = 1\n    y int32 = 2\n}\n"
	unknownHashNewer  = "package probe\nfixed table Point {\n    x int32 = 1\n    y int32 = 2\n    z int32 = 3\n}\n"
)

// unknownHashImage is a file image of schema `src` holding `records` zero-bodied
// records: the form byte, seven reserved zeros, the layout hash, the layout
// behind its u32 length, then the records. It is what that build's own save
// writes, built from the lineage entry the lock would record for it.
func unknownHashImage(t *testing.T, src string, records int) ([]byte, uint64) {
	t.Helper()
	u := unitOf(t, src)
	e, ok := FixedLineageOf(u, "Point")
	if !ok {
		t.Fatal("no lineage entry for Point")
	}
	img := make([]byte, 0, 20+len(e.Layout)+records*int(e.Record))
	img = append(img, 3, 0, 0, 0, 0, 0, 0, 0)
	for k := 0; k < 8; k++ {
		img = append(img, byte(e.Wire>>(8*k)))
	}
	n := len(e.Layout)
	img = append(img, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	img = append(img, e.Layout...)
	for r := 0; r < records; r++ {
		for k := 0; k < 8; k++ {
			img = append(img, byte(e.Wire>>(8*k)))
		}
		img = append(img, make([]byte, int(e.Record)-8)...)
	}
	return img, e.Wire
}

func unknownHashLiteral(b []byte) string {
	var sb strings.Builder
	sb.WriteString("[]byte{")
	for i, c := range b {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "0x%02x", c)
	}
	sb.WriteString("}")
	return sb.String()
}

// unknownHashProbe generates `reader` with the given lineage handed to it (the
// older units' locked entries, oldest first, the first `retire` of them
// retired), compiles it with `testSource` and runs the test.
func unknownHashProbe(t *testing.T, reader string, older []string, retire int, testSource string) {
	t.Helper()
	u := unitOf(t, reader)
	var lineage map[string][]FixedLineageEntry
	if len(older) > 0 {
		lineage = map[string][]FixedLineageEntry{}
		for i, src := range older {
			e, ok := FixedLineageOf(unitOf(t, src), "Point")
			if !ok {
				t.Fatal("no lineage entry for Point")
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage["Point"] = append(lineage["Point"], e)
		}
	}
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../..")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["unknownhash_test.go"] = []byte(testSource)
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}

// unknownHashChecks is the probe body the cases share: `load` reads a file and
// `want` names what it owes. Every refusal carries THE FILE'S hash and nothing
// else, moves no counter, is not malformed, and writes no destination byte.
const unknownHashChecks = `
func refuse(t *testing.T, what string, file []byte, reason string, hash uint64) {
	t.Helper()
	plan := make([]TableFixedEntry, PointFixedPlanMax)
	got := make([]Point, 4)
	for i := range got {
		PointReset(&got[i])
	}
	var r TableReport
	n := PointFixedLoad(got, file, plan, &r)
	if n != -1 || r != (TableReport{Verdict: TableOpenRefused, FixedRaised: true, Reason: reason, LayoutHash: hash}) {
		t.Fatalf("%s: want %s carrying 0x%016x and nothing else; got n=%d report=%+v", what, reason, hash, n, r)
	}
	for i := range got {
		var fresh Point
		PointReset(&fresh)
		if got[i] != fresh {
			t.Fatalf("%s: a refusal wrote destination bytes: %+v", what, got[i])
		}
	}
}

func readsClean(t *testing.T, what string, file []byte, want int64) {
	t.Helper()
	plan := make([]TableFixedEntry, PointFixedPlanMax)
	got := make([]Point, 4)
	var r TableReport
	if n := PointFixedLoad(got, file, plan, &r); n != want || r != (TableReport{}) {
		t.Fatalf("%s: want %d records and a silent report; got n=%d report=%+v", what, want, n, r)
	}
}

// flips refuses the file once per header-hash byte with that byte flipped: one
// damaged byte is layout_unknown, never layout_newer.
func flips(t *testing.T, what string, file []byte, hash uint64) {
	t.Helper()
	for k := 0; k < 8; k++ {
		bad := append([]byte(nil), file...)
		bad[TableFixedHashAt+k] ^= 0x01
		refuse(t, what+" with header hash byte "+string(rune('0'+k))+" flipped", bad, "layout_unknown", hash^(uint64(1)<<(8*uint(k))))
	}
}
`

// TestFixedLoadUnknownHashIsLayoutUnknown is the card's proof. At BASE every
// case below answered layout_newer.
func TestFixedLoadUnknownHashIsLayoutUnknown(t *testing.T) {
	t.Parallel()
	own, ownHash := unknownHashImage(t, unknownHashReader, 2)
	older, olderHash := unknownHashImage(t, unknownHashOlder, 2)
	newer, newerHash := unknownHashImage(t, unknownHashNewer, 2)
	if ownHash == olderHash || ownHash == newerHash || olderHash == newerHash {
		t.Fatal("the three layouts must hash apart for the cases to mean anything")
	}
	header := `package probe
import "testing"
var ownImage = ` + unknownHashLiteral(own) + `
var olderImage = ` + unknownHashLiteral(older) + `
var newerImage = ` + unknownHashLiteral(newer) + fmt.Sprintf(`
const ownHash, olderHash, newerHash = uint64(0x%016x), uint64(0x%016x), uint64(0x%016x)
`, ownHash, olderHash, newerHash) + unknownHashChecks

	// A build with NO lock: its lineage is one entry, its own.
	t.Run("no lock", func(t *testing.T) {
		t.Parallel()
		unknownHashProbe(t, unknownHashReader, nil, 0, header+`
func TestNoLock(t *testing.T) {
	// THE LEGITIMATE VALUE: the reader's own file, so the refusals are the hash.
	readsClean(t, "the reader's own file", ownImage, 2)
	// A flipped hash byte: corruption, not a newer reader.
	flips(t, "own image", ownImage, ownHash)
	// An older layout nobody ever locked.
	refuse(t, "an older layout the reader never locked", olderImage, "layout_unknown", olderHash)
	// A newer writer's layout this build does not hold.
	refuse(t, "a newer writer's locked layout this build does not hold", newerImage, "layout_unknown", newerHash)
}
`)
	})

	// A build whose lock holds the older layout: the lineage proves "older".
	t.Run("lineage holds the older layout", func(t *testing.T) {
		t.Parallel()
		unknownHashProbe(t, unknownHashReader, []string{unknownHashOlder}, 0, header+`
func TestLineage(t *testing.T) {
	// The lineage's own entries are not unknown: equal reads, older reads.
	readsClean(t, "the reader's own file", ownImage, 2)
	plan := make([]TableFixedEntry, PointFixedPlanMax)
	got := make([]Point, 4)
	var r TableReport
	if n := PointFixedLoad(got, olderImage, plan, &r); n != 2 || r.Reason != "" || r.Malformed {
		t.Fatalf("a layout the lineage holds must read: n=%d report=%+v", n, r)
	}
	// Strangers stay unknown: the lineage can only prove older or equal.
	flips(t, "own image", ownImage, ownHash)
	flips(t, "older image", olderImage, olderHash)
	refuse(t, "a newer writer's layout the lock has not recorded", newerImage, "layout_unknown", newerHash)
}
`)
	})

	// A build that retired the older layout: below the floor, proven older.
	t.Run("lineage retired the older layout", func(t *testing.T) {
		t.Parallel()
		unknownHashProbe(t, unknownHashReader, []string{unknownHashOlder}, 1, header+`
func TestRetired(t *testing.T) {
	readsClean(t, "the reader's own file", ownImage, 2)
	// Proven OLDER by the lineage: upgrade the client, never layout_unknown.
	refuse(t, "a retired older layout", olderImage, "layout_unsupported", olderHash)
	refuse(t, "a newer writer's layout the lock has not recorded", newerImage, "layout_unknown", newerHash)
}
`)
	})

	// The name is exported, and the emitted Go fixed load never answers
	// layout_newer: the lineage proves nothing newer today, so the name is
	// reserved. The runtime may still export TableFixedReasonLayoutNewer (an
	// adopter's comparisons keep compiling); only the load's body is checked,
	// so putting the old pick<0 line back is red here.
	t.Run("the name is exported and layout_newer is reserved", func(t *testing.T) {
		t.Parallel()
		files, err := Generate(unitOf(t, unknownHashReader))
		if err != nil {
			t.Fatal(err)
		}
		var all strings.Builder
		for _, data := range files {
			all.Write(data)
			all.WriteByte('\n')
		}
		load := funcSource(all.String(), "func PointFixedLoad(")
		if load == "" {
			t.Fatal("the emitted module has no PointFixedLoad")
		}
		for _, banned := range []string{`"layout_newer"`, "TableFixedReasonLayoutNewer"} {
			if strings.Contains(load, banned) {
				t.Errorf("PointFixedLoad names %s, which is reserved until the lineage proves direction (SPEC-TABLES §21.3)", banned)
			}
		}
		if !strings.Contains(load, "tableFixedRefuseHash(report, TableFixedReasonLayoutUnknown, hash)") {
			t.Error("PointFixedLoad does not answer an unknown hash with TableFixedReasonLayoutUnknown and the file's hash")
		}
		unknownHashProbe(t, unknownHashReader, nil, 0, `package probe
import "testing"
func TestExported(t *testing.T) {
	if TableFixedReasonLayoutUnknown != "layout_unknown" {
		t.Fatalf("TableFixedReasonLayoutUnknown = %q", TableFixedReasonLayoutUnknown)
	}
}
`)
	})
}
