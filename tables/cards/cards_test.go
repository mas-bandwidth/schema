package cards

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// setStr places a Go string into a generated string(N) buffer and its used
// length companion (docs/SPEC-TABLES.md §6.1).
func setStr(dst []byte, n *int32, s string) {
	copy(dst, s)
	*n = int32(len(s))
}

// makeBody builds a 20 KiB body of well-formed UTF-8 with no NUL, carrying
// multibyte characters, escapes, tabs and newlines (docs/SPEC-TABLES.md §2.5).
func makeBody(t *testing.T) string {
	t.Helper()
	const target = 20 * 1024
	unit := []byte("résumé→\t\"quote\"\\backslash\nline🚀—")
	var buf bytes.Buffer
	for buf.Len()+len(unit) <= target {
		buf.Write(unit)
	}
	for buf.Len() < target {
		buf.WriteByte('x')
	}
	b := buf.Bytes()
	if len(b) != target || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		t.Fatalf("body construction is not %d bytes of NUL-free UTF-8", target)
	}
	return string(b)
}

// buildPopulated fills every field of a card through the generated builder and
// saves it to wire (docs/SPEC-TABLES.md §6.2, §16).
func buildPopulated(t *testing.T) []byte {
	t.Helper()
	var b CardBuilder
	if !b.Init() {
		t.Fatal("CardBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStr(root.Id[:], &root.IdLength, "card-1234")
	setStr(root.Sha[:], &root.ShaLength, strings.Repeat("a", 40))
	root.Tier = Tierheavy
	setStr(root.Repo[:], &root.RepoLength, "mas-bandwidth/schema")
	setStr(root.Base[:], &root.BaseLength, "sprint/schema-finish")
	setStr(root.Kind[:], &root.KindLength, "feature")
	root.Priority = Priorityhigh
	setStr(root.Stream[:], &root.StreamLength, "schema-cards")
	for _, p := range []string{"tables/cards/Card.schema", "make/go.mk", "test/conformance/go/cards_codec.go"} {
		e := CardPathsAdd(&b.Main, &root.Paths)
		if e == nil {
			t.Fatal("CardPathsAdd returned nil")
		}
		setStr(e.S[:], &e.SLength, p)
	}
	for _, p := range []string{"tables/cards/go.mod", "tables/cards/cards.go"} {
		e := CardNewAdd(&b.Main, &root.New)
		if e == nil {
			t.Fatal("CardNewAdd returned nil")
		}
		setStr(e.S[:], &e.SLength, p)
	}
	if e := CardSharedAdd(&b.Main, &root.Shared); e == nil {
		t.Fatal("CardSharedAdd returned nil")
	} else {
		setStr(e.S[:], &e.SLength, "tables/cards/cards_test.go")
	}
	for _, n := range []string{"dep-0", "dep-1"} {
		e := CardNeedsAdd(&b.Main, &root.Needs)
		if e == nil {
			t.Fatal("CardNeedsAdd returned nil")
		}
		setStr(e.S[:], &e.SLength, n)
	}
	setStr(root.Test[:], &root.TestLength, "TestCardDeclarationRoundTrip")
	TableStringEmplace(&b.Main, &root.Done, []byte("make cards-test"))
	root.DeadlineMinutes = 90
	TableStringEmplace(&b.Main, &root.Source, []byte("docs/CONTRIBUTING.md résumé→世界"))
	root.Epoch = 17
	root.Column = Columnworking
	setStr(root.Holder[:], &root.HolderLength, "bench")
	root.Attempt = 3
	setStr(root.Head[:], &root.HeadLength, strings.Repeat("9", 40))
	setStr(root.Branch[:], &root.BranchLength, "sprint/schema-cards-c01-declaration-and-roundtrip")
	TableStringEmplace(&b.Main, &root.Body, []byte(makeBody(t)))
	if !b.Lock() {
		t.Fatal("CardBuilder.Lock failed")
	}
	packed := b.AsConst()
	size := CardMeasure(packed)
	if size < 0 {
		t.Fatal("CardMeasure refused the built card")
	}
	wire := make([]byte, size)
	if CardSave(packed, wire) != size {
		t.Fatal("CardSave did not fill the measured wire")
	}
	return wire
}

// TestCardDeclarationRoundTrip builds every field through the generated
// builder, serializes, loads, and compares every logical field; a generated
// JSON round trip compares again and the body bytes are exact.
func TestCardDeclarationRoundTrip(t *testing.T) {
	t.Parallel()
	wire := buildPopulated(t)
	rec, rep, err := Load(wire)
	if err != nil {
		t.Fatalf("Load: %v (report %+v)", err, rep)
	}
	if rec == nil {
		t.Fatal("Load returned a nil record")
	}
	if !bytes.Equal(rec.Wire(), wire) {
		t.Fatal("Wire() does not reproduce the original wire")
	}
	body := stringData(&rec.root.Body, nil)
	want := makeBody(t)
	if !bytes.Equal(body, []byte(want)) {
		t.Fatalf("body bytes differ: got %d bytes, want %d", len(body), len(want))
	}
	text, _, err := ToJSON(rec)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	rec2, err := FromJSON(text)
	if err != nil {
		t.Fatalf("FromJSON round trip: %v", err)
	}
	text2, _, err := ToJSON(rec2)
	if err != nil {
		t.Fatalf("ToJSON (second): %v", err)
	}
	if !bytes.Equal(text, text2) {
		t.Fatalf("JSON round trip is not stable:\n%s\n--- vs ---\n%s", text, text2)
	}
	if !bytes.Equal(rec2.Wire(), rec.Wire()) {
		t.Fatal("JSON round trip changed the wire")
	}
	if got := stringData(&rec2.root.Body, nil); !bytes.Equal(got, body) {
		t.Fatal("JSON round trip changed the body bytes")
	}
}

// TestCardRecordOwnsWire mutates the caller's input and a Wire() return
// independently and proves the stored bytes and value are unchanged
// (docs/SPEC-TABLES.md §6.2: the record owns its storage).
func TestCardRecordOwnsWire(t *testing.T) {
	t.Parallel()
	wire := buildPopulated(t)
	rec, _, err := Load(wire)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	snapshot := append([]byte(nil), wire...)
	for i := range wire {
		wire[i] = 0xff
	}
	if !bytes.Equal(rec.Wire(), snapshot) {
		t.Fatal("mutating the caller's input changed the record's wire")
	}
	returned := rec.Wire()
	for i := range returned {
		returned[i] = 0
	}
	if !bytes.Equal(rec.Wire(), snapshot) {
		t.Fatal("mutating a Wire() return changed the record's wire")
	}
	if text, _, err := ToJSON(rec); err != nil {
		t.Fatalf("ToJSON after mutation: %v", err)
	} else if len(text) == 0 {
		t.Fatal("ToJSON produced empty text")
	}
}

// TestCardJSONRefusesLoss covers unknown property, out-of-range enum/value,
// over-bound element string, NUL in the body, and the generated report's loss
// events: each is an error, never an accepted truncation (docs/SPEC-TABLES.md §4, §16).
func TestCardJSONRefusesLoss(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		json string
	}{
		{"unknown property", `{"id":"x","bogus":1}`},
		{"out-of-range enum", `{"tier":"not-a-tier"}`},
		{"out-of-range value", `{"deadline_minutes":100001}`},
		{"over-bound element string", `{"paths":[{"s":"` + strings.Repeat("p", 193) + `"}]}`},
		{"NUL in body", `{"body":"a\u0000b"}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := FromJSON([]byte(tc.json)); err == nil {
				t.Fatalf("FromJSON(%q) accepted a lossy input", tc.json)
			}
		})
	}
}

// TestCardBodyBoundRefused proves the 64 KiB body ceiling: exactly 64 KiB
// succeeds and 64 KiB + 1 is refused naming body and 65536 (docs/SPEC-TABLES.md §2.5).
func TestCardBodyBoundRefused(t *testing.T) {
	t.Parallel()
	build := func(bodyLen int) string {
		var b strings.Builder
		b.WriteString(`{"id":"x","body":"`)
		for i := 0; i < bodyLen; i++ {
			b.WriteByte('y')
		}
		b.WriteString(`"}`)
		return b.String()
	}
	if _, err := FromJSON([]byte(build(MaxBodyBytes))); err != nil {
		t.Fatalf("exactly 64 KiB body was refused: %v", err)
	}
	_, err := FromJSON([]byte(build(MaxBodyBytes + 1)))
	if err == nil {
		t.Fatal("64 KiB + 1 body was accepted")
	}
	if !strings.Contains(err.Error(), "body") || !strings.Contains(err.Error(), "65536") {
		t.Fatalf("refusal did not name body and 65536: %v", err)
	}
}

// TestCardListBoundRefused proves the 512-entry ceiling for each collection,
// as table cases (docs/SPEC-TABLES.md §2.9).
func TestCardListBoundRefused(t *testing.T) {
	t.Parallel()
	mk := func(field string, entries int, elem string) string {
		var b strings.Builder
		b.WriteString(`{"id":"x","` + field + `":[`)
		for i := 0; i < entries; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"s":"` + elem + `"}`)
		}
		b.WriteString(`]}`)
		return b.String()
	}
	cases := []struct {
		field string
		elem  string
	}{
		{"paths", "p"},
		{"_new", "n"},
		{"shared", "s"},
		{"needs", "d"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			if _, err := FromJSON([]byte(mk(tc.field, MaxListEntries, tc.elem))); err != nil {
				t.Fatalf("exactly %d entries in %s was refused: %v", MaxListEntries, tc.field, err)
			}
			_, err := FromJSON([]byte(mk(tc.field, MaxListEntries+1, tc.elem)))
			if err == nil {
				t.Fatalf("%d entries in %s was accepted", MaxListEntries+1, tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("refusal did not name %s: %v", tc.field, err)
			}
		})
	}
}

// fixtureCard builds the small populated card the wire-fuzz manifest pins: all
// field families, multiple list entries and a UTF-8 body. It returns the wire
// and the matching JSON text.
func fixtureCard(t *testing.T) (wire, json []byte) {
	t.Helper()
	var b CardBuilder
	if !b.Init() {
		t.Fatal("CardBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStr(root.Id[:], &root.IdLength, "populated")
	setStr(root.Sha[:], &root.ShaLength, "0123456789012345678901234567890123456789")
	root.Tier = Tierflash
	setStr(root.Repo[:], &root.RepoLength, "repo")
	setStr(root.Base[:], &root.BaseLength, "base")
	setStr(root.Kind[:], &root.KindLength, "kind")
	root.Priority = Prioritycritical
	setStr(root.Stream[:], &root.StreamLength, "stream")
	for _, s := range []string{"path/one", "path/two"} {
		e := CardPathsAdd(&b.Main, &root.Paths)
		if e == nil {
			t.Fatal("CardPathsAdd returned nil")
		}
		setStr(e.S[:], &e.SLength, s)
	}
	if e := CardNewAdd(&b.Main, &root.New); e == nil {
		t.Fatal("CardNewAdd returned nil")
	} else {
		setStr(e.S[:], &e.SLength, "new/path")
	}
	if e := CardSharedAdd(&b.Main, &root.Shared); e == nil {
		t.Fatal("CardSharedAdd returned nil")
	} else {
		setStr(e.S[:], &e.SLength, "shared/path")
	}
	for _, s := range []string{"need-one", "need-two"} {
		e := CardNeedsAdd(&b.Main, &root.Needs)
		if e == nil {
			t.Fatal("CardNeedsAdd returned nil")
		}
		setStr(e.S[:], &e.SLength, s)
	}
	setStr(root.Test[:], &root.TestLength, "fixture test")
	TableStringEmplace(&b.Main, &root.Done, []byte("done when"))
	root.DeadlineMinutes = 45
	TableStringEmplace(&b.Main, &root.Source, []byte("source résumé→世界"))
	root.Epoch = 7
	root.Column = Columnreview
	setStr(root.Holder[:], &root.HolderLength, "holder")
	root.Attempt = 1
	setStr(root.Head[:], &root.HeadLength, "0123456789012345678901234567890123456789")
	setStr(root.Branch[:], &root.BranchLength, "branch/name")
	TableStringEmplace(&b.Main, &root.Body, []byte("body with multibyte →世界 and \"escapes\"\nand tabs\t"))
	if !b.Lock() {
		t.Fatal("CardBuilder.Lock failed")
	}
	packed := b.AsConst()
	size := CardMeasure(packed)
	if size < 0 {
		t.Fatal("CardMeasure refused the fixture card")
	}
	wire = make([]byte, size)
	if CardSave(packed, wire) != size {
		t.Fatal("CardSave did not fill the measured wire")
	}
	rec, _, err := Load(wire)
	if err != nil {
		t.Fatalf("Load fixture card: %v", err)
	}
	json, _, err = ToJSON(rec)
	if err != nil {
		t.Fatalf("ToJSON fixture card: %v", err)
	}
	return wire, json
}

// TestCardFixtureRead reads the committed populated wire and its matching JSON
// and proves the decoded values reproduce the JSON (docs/SPEC-TABLES.md §16).
func TestCardFixtureRead(t *testing.T) {
	t.Parallel()
	wire, err := os.ReadFile(filepath.Join("fuzz", "card-populated.wire"))
	if err != nil {
		t.Fatalf("committed wire fixture is missing: %v", err)
	}
	json, err := os.ReadFile(filepath.Join("fuzz", "card-populated.json"))
	if err != nil {
		t.Fatalf("committed JSON fixture is missing: %v", err)
	}
	rec, _, err := Load(wire)
	if err != nil {
		t.Fatalf("Load fixture: %v", err)
	}
	got, _, err := ToJSON(rec)
	if err != nil {
		t.Fatalf("ToJSON fixture: %v", err)
	}
	if !bytes.Equal(got, json) {
		t.Fatalf("decoded wire does not match card-populated.json:\n%s\n--- vs ---\n%s", got, json)
	}
}

// TestCardFixtureWrite writes the populated wire and JSON into the named source
// fuzz directory. It runs only under CARDS_UPDATE_FIXTURE=1, so ordinary tests
// read the committed evidence and never generate it.
func TestCardFixtureWrite(t *testing.T) {
	if os.Getenv("CARDS_UPDATE_FIXTURE") != "1" {
		t.Skip("fixture update requires CARDS_UPDATE_FIXTURE=1")
	}
	wire, json := fixtureCard(t)
	dir := filepath.Join("..", "..", "..", "tables", "cards", "fuzz")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card-populated.wire"), wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card-populated.json"), json, 0o644); err != nil {
		t.Fatal(err)
	}
}
