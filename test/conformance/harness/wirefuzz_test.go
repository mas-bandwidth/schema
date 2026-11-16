package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The root name moves to an unknown field; its default replaces only that
// field, while every pointer and blob payload remains part of the value.
func TestFileFuzzUnknownFieldPreservesBlobGraph(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "blobdemo", "Catalog", false, false)
	if err != nil {
		t.Fatal(err)
	}
	pin := wireBytes(t, "testdata/wire/tables/blob_small.bin")
	wire := bytes.Clone(pin)
	frame := frameWire(wire)
	if len(frame.entries) == 0 || len(wire) < 2 || wire[1] != 1 {
		t.Fatal("pinned graph no longer names Catalog.name in the first slot")
	}
	// Keep reference 1 in its original position; changing its entry makes the
	// field unknown without leaving an unused entry or changing first-use order.
	const unknownID uint64 = 0x123456789abcdef0
	for _, field := range root.def.Fields {
		if ir.TableFieldWireId(field) == unknownID {
			t.Fatal("unknown field ID names a Catalog field")
		}
	}
	for _, id := range frame.entries {
		if id == unknownID {
			t.Fatal("unknown field ID already appears in the pinned table")
		}
	}
	// The name ID is also used by nested records. Preserve those occurrences
	// by moving them to a new slot, then number the table in first-use order.
	nameID := frame.entries[0]
	newSlot := uint64(len(frame.entries) + 1)
	if newSlot >= 128 {
		t.Fatal("pinned graph needs a wider reference")
	}
	for _, spot := range frame.spots {
		if spot.off == 1 || spot.value != 1 {
			continue
		}
		switch spot.kind {
		case spotRef, spotArm, spotKey, spotVariant, spotRecordType:
			if spot.width != 1 {
				t.Fatal("pinned name reference is not one byte")
			}
			wire[spot.off] = byte(newSlot)
		}
	}
	binary.LittleEndian.PutUint64(wire[frame.entriesAt:], unknownID)
	var entry [8]byte
	binary.LittleEndian.PutUint64(entry[:], nameID)
	wire = append(wire[:len(wire)-8], entry[:]...)
	var count [8]byte
	binary.LittleEndian.PutUint64(count[:], newSlot)
	wire = append(wire, count[:]...)
	wire, err = firstUseOrder(wire)
	if err != nil {
		t.Fatal(err)
	}
	if fault := framingTableFault(framingVector{}, wire); fault != "" {
		t.Fatalf("unknown-field mutant has non-canonical id table: %s", fault)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	want := root.model.New(root.def)
	if ok, err := tablewire.Decode(root.model, want, pin, &tabletext.Report{}); err != nil || !ok {
		t.Fatalf("pinned graph: %v", err)
	}
	name, ok := want.FieldByKey("name")
	if !ok {
		t.Fatal("Catalog.name missing")
	}
	name.Cell, name.Count = root.model.FieldDefaultCell(name.Def), 0
	encoded, err := tablewire.Encode(root.model, want)
	if err != nil {
		t.Fatal(err)
	}
	if !answer.accepted || !bytes.Equal(answer.encoded, encoded) {
		t.Fatalf("unknown field changed the surviving blob graph: accepted=%t got=%x want=%x", answer.accepted, answer.encoded, encoded)
	}
}

func TestFileFuzzCountCapIsFramingRefusal(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "listdemo", "Save", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join("testdata", "wire", "tables", "fuzz-vectors", "list_count_cap.bin"))
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if answer.accepted || len(answer.encoded) != 0 {
		t.Fatalf("count cap: %+v", answer)
	}
	if verdict := wireVerdict(root, legReply{measure: -1, report: Counts{Malformed: true}}, answer, len(wire)); verdict != "" {
		t.Fatal(verdict)
	}
	if verdict := wireVerdict(root, legReply{loaded: true, measure: 1024}, answer, len(wire)); verdict == "" {
		t.Fatal("accepted loaded count above the cap")
	}
}

// Builder and region readers refuse the same oversized or truncated count.
func TestBuilderFuzzChecksCountRefusalWithoutRegionPreflight(t *testing.T) {
	_, _, units := corpus(t)
	for _, tc := range []struct{ name, table string }{
		{"list_count_cap", "Save"}, {"list_count_cross_length", "Album"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := newWireRoot(units, "listdemo", tc.table, false, false)
			if err != nil {
				t.Fatal(err)
			}
			root.builder = true
			wire, err := os.ReadFile(filepath.Join("testdata", "wire", "tables", "fuzz-vectors", tc.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			answer, err := root.oracle(wire)
			if err != nil {
				t.Fatal(err)
			}
			if answer.accepted || len(answer.encoded) != 0 {
				t.Fatalf("builder count cap: %+v", answer)
			}
			reply := legReply{loaded: false, measure: -1, report: answer.report, saveFail: true}
			if verdict := wireVerdict(root, reply, answer, len(wire)); verdict != "" {
				t.Fatal(verdict)
			}
			reply.loaded = true
			reply.report = Counts{}
			if verdict := wireVerdict(root, reply, answer, len(wire)); verdict == "" {
				t.Fatal("builder gate accepted an oversized or truncated count")
			}
		})
	}
}

// TestWireFuzzZeroSeedRosterRefusesByName drives the wire-fuzz roster build
// over an empty roster — a manifest that names no seed — and asserts the
// harness refuses by name with no panic (docs/SPEC-TABLES.md §4.2, the wire
// fuzzer; test/conformance/README.md). This is the zero-roster defect: before,
// the harness's empty-roster path was a panic rather than a named refusal.
func TestWireFuzzZeroSeedRosterRefusesByName(t *testing.T) {
	t.Parallel()
	manifest := filepath.Join(t.TempDir(), "empty.MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte("unit cards tables/cards\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(manifest, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := wireFuzzOptions{vectors: ""}
	seeds, roots, _, _, err := buildWireFuzzRoster(m, opts)
	if err == nil {
		t.Fatalf("empty roster was not refused: seeds=%d roots=%d", len(seeds), len(roots))
	}
	if got := err.Error(); !strings.Contains(got, "0 seeds over 0 roots: the roster is empty") {
		t.Fatalf("refusal is not the named empty-roster one: %s", got)
	}
}

// fakeClock is an injected timer for the driver-reply deadline, firing the
// After channel when the test signals the deadline passed. It never uses real
// time.
type fakeClock struct {
	afterCh chan time.Time
}

func (f *fakeClock) After(time.Duration) <-chan time.Time { return f.afterCh }

// TestWireFuzzDriverStall pins the hang's cause at the layer this card fixed
// it: the harness's reply read had no bound, so a driver that stopped
// answering hung the run forever. Over a fake driver on io.Pipe (no process,
// no socket, no real time), the injected clock passing the deadline must turn
// the stall into a named failure (test/conformance/README.md, "The wire-fuzz
// driver").
func TestWireFuzzDriverStall(t *testing.T) {
	t.Parallel()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	defer stdinR.Close()
	defer stdoutW.Close()
	leg := &wireLeg{
		stdin:  stdinW,
		stdout: bufio.NewReader(stdoutR),
		known:  []bool{true},
	}
	// a fake driver that drains its stdin but never answers a reply
	go func() {
		defer stdinW.Close()
		_, _ = io.Copy(io.Discard, stdinR)
	}()
	clk := &fakeClock{afterCh: make(chan time.Time)}
	result := make(chan error, 1)
	go func() {
		_, err := leg.receiveWithDeadline(clk)
		result <- err
	}()
	// the reply never comes; the injected clock passes the deadline
	close(clk.afterCh)
	err := <-result
	if err == nil {
		t.Fatal("a stalled driver returned no error")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("the stall is not named: %v", err)
	}
}

// A NON-FINITE FLOAT IS REFUSED BY EVERY READ (docs/SPEC-TABLES.md §3.3): on a
// mutant the oracle refuses as non_finite_float, a leg that keeps the rule
// answers with the refusal verdict and nothing else is compared; a leg that
// lands the pattern is a divergence.
func TestWireVerdictNonFiniteIsARefusal(t *testing.T) {
	root := &wireRoot{unit: "probe", root: "Root"}
	answer := oracleAnswer{}
	if verdict := wireVerdict(root, legReply{loaded: true, report: Counts{Refused: true}, saveFail: true}, answer, 16); verdict != "" {
		t.Fatalf("a leg that refused the non-finite float diverged: %s", verdict)
	}
	if verdict := wireVerdict(root, legReply{loaded: true, report: Counts{Unknown: 1}, saved: []byte{1, 0}}, answer, 16); verdict == "" {
		t.Fatal("a leg that landed a non-finite float did not diverge")
	}
}

// Refusal diagnostics and detection order do not change the one verdict.
func TestWireVerdictRefusalDiagnosticsDoNotMatter(t *testing.T) {
	answer := oracleAnswer{report: Counts{Malformed: true}}
	for _, report := range []Counts{{Malformed: true}, {Refused: true}, {Malformed: true, Refused: true}} {
		if verdict := wireVerdict(nil, legReply{loaded: true, report: report}, answer, 16); verdict != "" {
			t.Fatalf("refusal diagnostics changed the verdict: %s", verdict)
		}
	}
}

// The pinned repeated-key mutant must refuse the whole map.
func TestFileFuzzRepeatedKeyWidenRefuses(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "mapdemo", "Pairs", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile("testdata/wire/tables/fuzz-vectors/map_repeated_key_widen.bin")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if answer.accepted || len(answer.encoded) != 0 {
		t.Fatalf("final widened key: %+v", answer)
	}
}

func TestWireFuzzFiniteExemptionIsRetired(t *testing.T) {
	if err := wireFuzz(nil, wireFuzzOptions{finiteOwed: "legacy"}); err == nil || !strings.Contains(err.Error(), "every mutant must be compared") {
		t.Fatalf("non-finite comparison exemption survived: %v", err)
	}
}
