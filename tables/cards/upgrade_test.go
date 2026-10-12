package cards

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cardsfuture "cardsfuture"
)

// The retained-upgrade proofs (docs/SPEC-TABLES.md §4, §5, §6.6): every
// shipped fixture upgrades successfully and preserves every value; a lossy or
// unsupported input refuses with nil output and byte-unchanged original.

// TestUpgradeKeepsEveryField enumerates every committed SHIPPED case and
// requires a successful, idempotent, value-preserving Upgrade.
func TestUpgradeKeepsEveryField(t *testing.T) {
	t.Parallel()
	for _, caseName := range shippedCases(t) {
		caseName := caseName
		dir := filepath.Join("corpus", caseName)
		wires, err := filepath.Glob(filepath.Join(dir, "*.wire"))
		if err != nil || len(wires) == 0 {
			t.Fatalf("%s: no .wire fixtures", caseName)
		}
		for _, wf := range wires {
			wf := wf
			t.Run(caseName+"/"+filepath.Base(wf), func(t *testing.T) {
				t.Parallel()
				wire, err := os.ReadFile(wf)
				if err != nil {
					t.Fatal(err)
				}
				json, err := os.ReadFile(strings.TrimSuffix(wf, ".wire") + ".json")
				if err != nil {
					t.Fatal(err)
				}
				checkUpgraded(t, caseName, wire, json)
			})
		}
	}
}

// buildFutureCard builds a card through the FUTURE declaration's generated
// reader: the current card plus a nonempty future_note scalar and, optionally,
// the archived Column variant the current reader cannot name (docs/SPEC-TABLES.md §6.6).
func buildFutureCard(t *testing.T, archived bool) []byte {
	t.Helper()
	var b cardsfuture.CardBuilder
	if !b.Init() {
		t.Fatal("cardsfuture.CardBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStr(root.Id[:], &root.IdLength, "future-card")
	setStr(root.Sha[:], &root.ShaLength, strings.Repeat("e", 40))
	root.Tier = cardsfuture.Tierpro
	setStr(root.Repo[:], &root.RepoLength, "mas-bandwidth/schema")
	setStr(root.Kind[:], &root.KindLength, "feature")
	setStr(root.Test[:], &root.TestLength, "TestUpgradeKeepsEveryField")
	if archived {
		root.Column = cardsfuture.Columnarchived
	} else {
		root.Column = cardsfuture.Columnreview
	}
	root.Attempt = 1
	setStr(root.FutureNote[:], &root.FutureNoteLength, "future-value")
	if !b.Lock() {
		t.Fatal("CardBuilder.Lock failed")
	}
	packed := b.AsConst()
	size := cardsfuture.CardMeasure(packed)
	if size < 0 {
		t.Fatal("CardMeasure refused")
	}
	wire := make([]byte, size)
	if cardsfuture.CardSave(packed, wire) != size {
		t.Fatal("CardSave did not fill")
	}
	return wire
}

// TestUpgradePreservesRemovedField proves the retained region read/save keeps a
// field the reduced current declaration cannot name, and the historical
// declaration rereads it (docs/SPEC-TABLES.md §6.6).
func TestUpgradePreservesRemovedField(t *testing.T) {
	t.Parallel()
	wire := buildFutureCard(t, false)
	out, report, err := Upgrade(wire)
	if err != nil {
		t.Fatalf("Upgrade refused a removable scalar: %v (report %+v)", err, report)
	}
	// Reread with the FUTURE reader: future_note must survive.
	n := cardsfuture.CardLoadMeasure(out)
	if n < 0 {
		t.Fatal("future measure refused")
	}
	region := alignedRegion(n)
	var fr cardsfuture.TableReport
	froot := cardsfuture.CardLoad(region, out, &fr)
	if froot == nil || fr.Malformed {
		t.Fatalf("future reread failed: %+v", fr)
	}
	if got := rowStr(froot.FutureNote[:], froot.FutureNoteLength); got != "future-value" {
		t.Fatalf("future_note not preserved: %q", got)
	}
	// The current reader reports the future scalar as unknown, and it is retained.
	if report.Unknown == 0 || report.Retained == 0 {
		t.Fatalf("retained read should count Unknown and Retained: %+v", report)
	}
}

// TestReadStrictRejectsFutureScalar proves ReadStrict refuses a future
// nonempty field while a compatible declaration with only default/elided
// additions is accepted (docs/SPEC-TABLES.md §4, §6).
func TestReadStrictRejectsFutureScalar(t *testing.T) {
	t.Parallel()
	wire := buildFutureCard(t, false)
	if _, err := ReadStrict(wire); err == nil {
		t.Fatal("ReadStrict accepted a future nonempty scalar")
	}
	// A compatible empty-consumers card (only default/elided additions) is accepted.
	ew, _ := buildEmptyConsumersCard(t)
	if _, err := ReadStrict(ew); err != nil {
		t.Fatalf("ReadStrict refused a compatible card: %v", err)
	}
}

// TestUpgradeRefusesUnknownColumnVariant proves an unknown enum variant's loss
// blocks replacement: nil output, byte-unchanged original (docs/SPEC-TABLES.md §6.6).
func TestUpgradeRefusesUnknownColumnVariant(t *testing.T) {
	t.Parallel()
	wire := buildFutureCard(t, true)
	snapshot := append([]byte(nil), wire...)
	out, report, err := Upgrade(wire)
	if err == nil {
		t.Fatalf("Upgrade accepted an unknown Column variant (report %+v)", report)
	}
	if out != nil {
		t.Fatal("refused Upgrade returned output")
	}
	if report.RetainLost == 0 {
		t.Fatalf("refusal did not name RetainLost: %+v", report)
	}
	if !bytes.Equal(wire, snapshot) {
		t.Fatal("refused Upgrade mutated the input wire")
	}
}

// TestUpgradeRefusesMalformed proves damaged or non-card wire is refused with
// nil output and byte-unchanged original (docs/SPEC-TABLES.md §4.2).
func TestUpgradeRefusesMalformed(t *testing.T) {
	t.Parallel()
	base, _ := buildEmptyConsumersCard(t)
	cases := [][]byte{
		{0xff, 0xff, 0xff},
		base[:len(base)/2], // truncated
	}
	for _, wire := range cases {
		snapshot := append([]byte(nil), wire...)
		out, _, err := Upgrade(wire)
		if err == nil {
			t.Fatal("Upgrade accepted malformed wire")
		}
		if out != nil {
			t.Fatal("refused Upgrade returned output")
		}
		if !bytes.Equal(wire, snapshot) {
			t.Fatal("refused Upgrade mutated the input wire")
		}
	}
}

// TestUpgradeRefusesOversize proves the customer ceilings hold through Upgrade:
// a 64 KiB+1 body, 513 paths and 513 consumers each refuse with nil output and
// byte-unchanged original, while boundary-valid sizes succeed (docs/SPEC-TABLES.md §2.5, §2.9).
func TestUpgradeRefusesOversize(t *testing.T) {
	t.Parallel()
	build := func(bodyLen, paths, consumers int) []byte {
		var b CardBuilder
		if !b.Init() {
			t.Fatal("Init failed")
		}
		defer b.Shutdown()
		root := b.GetRoot()
		setStr(root.Id[:], &root.IdLength, "oversize")
		setStr(root.Sha[:], &root.ShaLength, strings.Repeat("f", 40))
		for i := 0; i < paths; i++ {
			e := CardPathsAdd(&b.Main, &root.Paths)
			setStr(e.S[:], &e.SLength, "p")
		}
		for i := 0; i < consumers; i++ {
			c := CardConsumersAdd(&b.Main, &root.Consumers)
			setStr(c.Kind[:], &c.KindLength, "k")
		}
		if bodyLen > 0 {
			TableStringEmplace(&b.Main, &root.Body, bytes.Repeat([]byte("x"), bodyLen))
		}
		if !b.Lock() {
			t.Fatal("Lock failed")
		}
		packed := b.AsConst()
		size := CardMeasure(packed)
		wire := make([]byte, size)
		if CardSave(packed, wire) != size {
			t.Fatal("Save did not fill")
		}
		return wire
	}
	refuse := func(name string, wire []byte) {
		t.Helper()
		snapshot := append([]byte(nil), wire...)
		out, _, err := Upgrade(wire)
		if err == nil {
			t.Fatalf("%s: Upgrade accepted an over-ceiling card", name)
		}
		if out != nil {
			t.Fatalf("%s: refused Upgrade returned output", name)
		}
		if !bytes.Equal(wire, snapshot) {
			t.Fatalf("%s: refused Upgrade mutated the input", name)
		}
	}
	refuse("body", build(MaxBodyBytes+1, 0, 0))
	refuse("paths", build(0, MaxListEntries+1, 0))
	refuse("consumers", build(0, 0, MaxListEntries+1))
	// Boundary-valid counterparts succeed.
	for name, wire := range map[string][]byte{
		"body":      build(MaxBodyBytes, 0, 0),
		"paths":     build(0, MaxListEntries, 0),
		"consumers": build(0, 0, MaxListEntries),
	} {
		if _, _, err := Upgrade(wire); err != nil {
			t.Fatalf("boundary-valid %s was refused: %v", name, err)
		}
	}
}

// TestUpgradeTinyCapacityRetainLost drives the package-private retention seam
// with a tiny caller-owned store: it must name RetainLost, return nil and
// preserve the input, while the fixed production limits still succeed
// (docs/SPEC-TABLES.md §6.6).
func TestUpgradeTinyCapacityRetainLost(t *testing.T) {
	t.Parallel()
	wire := buildFutureCard(t, false)
	snapshot := append([]byte(nil), wire...)
	out, report, err := upgrade(wire, 8, 4)
	if err == nil {
		t.Fatal("tiny capacity was accepted")
	}
	if out != nil {
		t.Fatal("tiny capacity returned output")
	}
	if report.RetainLost == 0 {
		t.Fatalf("tiny capacity did not name RetainLost: %+v", report)
	}
	if !bytes.Equal(wire, snapshot) {
		t.Fatal("tiny capacity mutated the input")
	}
	if _, _, err := Upgrade(wire); err != nil {
		t.Fatalf("production limits should succeed: %v", err)
	}
}
