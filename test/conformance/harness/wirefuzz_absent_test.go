package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/ir"
)

// W-ROSTER (docs/SPEC-CONFORMANCE.md §2, docs/SPEC-TABLES.md §4.2): a seed
// the leg has no codec for is a failure by name, never a pass. On main at
// 2d352b7dd8 the per-PR gate printed "30 seeds absent" for C and exited 0.

func TestWireFuzzAbsentRootsRefusedWithoutLedger(t *testing.T) {
	absent := map[string]int{"shapes.FixedBounds": 18, "tblsurf.Chain": 2}
	err := checkAbsentRoots(absent, nil, "", "")
	if err == nil {
		t.Fatal("20 absent seeds passed with no ledger")
	}
	for _, want := range []string{"20 seeds absent over 2 roots", "shapes.FixedBounds (18 seeds)", "tblsurf.Chain (2 seeds)", "W-ROSTER"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	if err := checkAbsentRoots(map[string]int{}, nil, "", ""); err != nil {
		t.Fatalf("a full roster refused: %v", err)
	}
}

// The ledger is matched as a SET: one root coming off while another goes
// absent keeps the count and is still red, naming both.
func TestWireFuzzAbsentRootsExactSet(t *testing.T) {
	ledger := map[string]string{"shapes.Bounds": "owed", "tblsurf.Chain@message": "owed"}
	if err := checkAbsentRoots(map[string]int{"shapes.Bounds": 4, "tblsurf.Chain@message": 2}, ledger, "c-plain.tsv", ""); err != nil {
		t.Fatalf("the exact ledgered set refused: %v", err)
	}
	err := checkAbsentRoots(map[string]int{"shapes.Bounds": 4, "tblsurf.Holder": 2}, ledger, "c-plain.tsv", "")
	if err == nil {
		t.Fatal("a swapped absent root passed on an equal count")
	}
	for _, want := range []string{"tblsurf.Holder (2 seeds)", "not in c-plain.tsv", "now answers", "tblsurf.Chain@message"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	err = checkAbsentRoots(map[string]int{}, ledger, "c-plain.tsv", "")
	if err == nil || !strings.Contains(err.Error(), "remove them") {
		t.Fatalf("stale ledger lines passed: %v", err)
	}
}

func TestWireFuzzAbsentLedgerParse(t *testing.T) {
	text := "# comment\n\nshapes.Bounds\towed by a card\ntblsurf.Chain@message\tanother\n"
	got, err := parseAbsentLedger(strings.NewReader(text), "L")
	if err != nil || len(got) != 2 || got["shapes.Bounds"] != "owed by a card" || got["tblsurf.Chain@message"] != "another" {
		t.Fatalf("got %v, %v", got, err)
	}
	for name, bad := range map[string]string{
		"no reason":    "shapes.Bounds\n",
		"empty reason": "shapes.Bounds\t \n",
		"space":        "shapes.Bounds why\n",
		"not a root":   "Bounds\twhy\n",
		"twice":        "shapes.Bounds\ta\nshapes.Bounds\tb\n",
		"no lines":     "# only a comment\n",
	} {
		if _, err := parseAbsentLedger(strings.NewReader(bad), "L"); err == nil {
			t.Errorf("%s: ledger accepted", name)
		}
	}
	if _, err := readAbsentLedger("wire-absent-no-such-ledger.tsv"); err == nil {
		t.Error("a missing ledger accepted")
	}
}

// Every checked-in ledger parses, and is named <leg>-<arm>.tsv.
func TestWireFuzzAbsentLedgersCheckedIn(t *testing.T) {
	files, err := filepath.Glob("../wire-absent/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("test/conformance/wire-absent holds no ledger: every leg answers every root, so retire this test with the directory")
	}
	for _, path := range files {
		if !strings.HasSuffix(path, ".tsv") || !strings.Contains(filepath.Base(path), "-") {
			t.Errorf("%s: a ledger is named <leg>-<arm>.tsv", path)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseAbsentLedger(strings.NewReader(string(data)), path); err != nil {
			t.Error(err)
		}
	}
}

// fakeWireLeg is the test binary as a wire-fuzz leg: it reads the roster,
// answers `1` for every root except the `<unit>.<Root>[@message]` names in
// args, and leaves. A run that gets past the roster check then fails on the
// leg's closed pipe, which is how a test tells the check's verdict from what
// follows it.
func fakeWireLeg(args []string) int {
	absent := map[string]bool{}
	for _, a := range args {
		absent[a] = true
	}
	in := bufio.NewReader(os.Stdin)
	var n uint32
	if err := binary.Read(in, binary.LittleEndian, &n); err != nil {
		return 2
	}
	str := func() (string, error) {
		var l uint16
		if err := binary.Read(in, binary.LittleEndian, &l); err != nil {
			return "", err
		}
		b := make([]byte, l)
		_, err := io.ReadFull(in, b)
		return string(b), err
	}
	answers := make([]byte, n)
	for i := range answers {
		unit, err := str()
		if err != nil {
			return 2
		}
		root, err := str()
		if err != nil {
			return 2
		}
		var formRetain [2]byte
		if _, err := io.ReadFull(in, formRetain[:]); err != nil {
			return 2
		}
		name := unit + "." + root
		if formRetain[0] == ir.TableWireMessageForm {
			name += "@message"
		}
		if !absent[name] {
			answers[i] = 1
		}
	}
	if _, err := os.Stdout.Write(answers); err != nil {
		return 2
	}
	return 0
}

// wireFuzzFixtureManifest is a small roster for the end-to-end tests: a few
// committed seeds of three units (a fixed and a variable root of shapes, a
// variable root of listdemo, a retained variable root of graphdemo), so a run
// compiles three units and not the whole corpus. The working directory is the
// repository root, where the manifest's paths resolve.
const wireFuzzFixtureManifest = `unit shapes test/tables/shapes
instance shapes_bounds_ends shapes Bounds testdata/wire/tables/shapes/bounds_ends.bin
instance shapes_fixed_ends shapes FixedBounds testdata/wire/tables/shapes/fixed_ends.bin
unit listdemo tables/lists
instance list_shared listdemo Album testdata/wire/tables/list_shared.bin
unit graphdemo tables/pointers
instance graph_tree graphdemo Scene testdata/wire/tables/graph_tree.bin
`

func wireFuzzFixture(t *testing.T) *Manifest {
	t.Helper()
	t.Chdir(conformanceRoot(t))
	path := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(path, []byte(wireFuzzFixtureManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(path, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// runFakeWireFuzz runs wireFuzz end to end over m, with the test binary as a
// leg that leaves `absent` unanswered and the ledgers in ledgerDir. The
// pinned vectors are off: they name units the fixture does not hold.
func runFakeWireFuzz(t *testing.T, m *Manifest, opts wireFuzzOptions, absent ...string) error {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	failed := filepath.Join(t.TempDir(), "failed.bin")
	t.Setenv("SCHEMA_HARNESS_FAKE_WIRE_LEG", "1")
	prog := exe
	if opts.driver != "" {
		prog = opts.driver // a driver the test names, the fake leg under it
	}
	opts.driver = strings.Join(append([]string{prog}, absent...), " ")
	opts.vectors = ""
	opts.seed, opts.n, opts.failed = 1, 1, failed
	return wireFuzz(m, opts)
}

func writeLedger(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// W-ROSTER END TO END: the check is wired into the run. A leg that leaves a
// root unanswered and unlisted is red by name; the same root in the leg's
// ledger gets past the check; a ledger line the leg answers is red; a run
// with no --leg is refused. Disabling the check in wireFuzz turns the first
// and third cases green past the roster, and this test red.
func TestWireFuzzAbsentRootEndToEnd(t *testing.T) {
	m := wireFuzzFixture(t)
	dir := t.TempDir()
	const root = "shapes.Bounds"

	err := runFakeWireFuzz(t, m, wireFuzzOptions{leg: "fake", ledgerDir: dir}, root)
	if err == nil || !strings.Contains(err.Error(), "seeds absent over 1 roots") || !strings.Contains(err.Error(), root+" (") {
		t.Fatalf("an unlisted absent root did not fail the run by name: %v", err)
	}

	writeLedger(t, dir, "fake-plain.tsv", root+"\towed: a test\n")
	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "fake", ledgerDir: dir}, root)
	if err == nil {
		t.Fatal("the fake leg closed its pipe, yet the run passed")
	}
	if strings.Contains(err.Error(), "seeds absent") || strings.Contains(err.Error(), "remove them") {
		t.Fatalf("a ledgered absent root failed the roster check: %v", err)
	}

	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "fake", ledgerDir: dir})
	if err == nil || !strings.Contains(err.Error(), "remove them: "+root) {
		t.Fatalf("a ledger line the leg answers passed: %v", err)
	}

	err = runFakeWireFuzz(t, m, wireFuzzOptions{ledgerDir: dir}, root)
	if err == nil || !strings.Contains(err.Error(), "--leg names the leg") {
		t.Fatalf("a whole-roster run with no --leg was not refused: %v", err)
	}

	// the arm is the run's profile: the retain run reads fake-retain.tsv,
	// which does not exist, so the plain ledger excuses nothing there
	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "fake", ledgerDir: dir, retain: true}, "graphdemo.Scene")
	if err == nil || !strings.Contains(err.Error(), "fake-retain.tsv does not exist") {
		t.Fatalf("the retain arm did not read its own ledger: %v", err)
	}
}

// A SPLIT LINE IS ANSWERED BY THE BUILDER ARM: the builder run of the same
// leg reads the plain ledger and refuses a split root it does not answer.
func TestWireFuzzSplitRootAnsweredByBuilderArm(t *testing.T) {
	m := wireFuzzFixture(t)
	dir := t.TempDir()
	const root = "listdemo.Album" // a variable root: in the builder roster
	writeLedger(t, dir, "fake-plain.tsv", root+"\tsplit: fuzzed by the fake builder arm\n")

	// the builder arm answers it: past both checks
	err := runFakeWireFuzz(t, m, wireFuzzOptions{leg: "fake", ledgerDir: dir, builder: true})
	if err == nil || strings.Contains(err.Error(), "split") || strings.Contains(err.Error(), "seeds absent") {
		t.Fatalf("a split root the builder arm answers: %v", err)
	}

	// the builder arm leaves it absent, and its own ledger holds that: the
	// split is a root no arm fuzzes
	writeLedger(t, dir, "fake-builder.tsv", root+"\towed: a test\n")
	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "fake", ledgerDir: dir, builder: true}, root)
	if err == nil || !strings.Contains(err.Error(), "as split to the builder arm") || !strings.Contains(err.Error(), root) {
		t.Fatalf("a split root the builder arm does not answer passed: %v", err)
	}
}

// fakeGooseLeg is the test binary under the Goose leg's name, wirefuzz.
func fakeGooseLeg(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	leg := filepath.Join(t.TempDir(), "wirefuzz")
	if err := os.Symlink(exe, leg); err != nil {
		t.Fatal(err)
	}
	return leg
}

// A ONE-UNIT RUN (--unit without --replay) asks about its unit alone and
// holds only that unit's ledger lines.
func TestWireFuzzUnitScopedRoster(t *testing.T) {
	m := wireFuzzFixture(t)
	seeds, roots, _, _, err := buildWireFuzzRoster(m, wireFuzzOptions{unit: "listdemo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) == 0 {
		t.Fatal("the listdemo roster is empty")
	}
	for _, r := range roots {
		if r.unit != "listdemo" {
			t.Fatalf("a listdemo-scoped roster names %s.%s", r.unit, r.root)
		}
	}
	ledger := map[string]string{"listdemo.Album": "owed", "shapes.Bounds": "owed"}
	if err := checkAbsentRoots(map[string]int{"listdemo.Album": 2}, ledger, "L", "listdemo"); err != nil {
		t.Fatalf("a scoped run held another unit's line: %v", err)
	}
	if err := checkAbsentRoots(map[string]int{"listdemo.Album": 2}, ledger, "L", ""); err == nil {
		t.Fatal("an unscoped run passed a ledger line the roster answers")
	}
}

// --UNIT IS FOR ONE-UNIT LEGS ALONE (#4880 round 2): a whole-roster leg that
// takes --unit leaves every other unit unasked, so its absent roots there are
// never counted. The harness refuses --unit on any leg oneUnitLegs does not
// name, and a one-unit leg without --unit. Letting --unit through on any leg
// turns the first case green past the roster, and this test red.
func TestWireFuzzUnitOnlyOnOneUnitLegs(t *testing.T) {
	m := wireFuzzFixture(t)
	dir := t.TempDir()

	// shapes.Bounds is unanswered and in no ledger: a whole-roster run scoped
	// to listdemo would never see it
	err := runFakeWireFuzz(t, m, wireFuzzOptions{leg: "fake", ledgerDir: dir, unit: "listdemo"}, "shapes.Bounds")
	if err == nil || !strings.Contains(err.Error(), "only a one-unit leg (goose-unit) takes it, and --leg fake is not one") {
		t.Fatalf("--unit on a whole-roster leg was not refused: %v", err)
	}

	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "goose-unit", ledgerDir: dir}, "shapes.Bounds")
	if err == nil || !strings.Contains(err.Error(), "--leg goose-unit is a one-unit leg (its whole-roster run is --leg goose) and needs --unit") {
		t.Fatalf("a one-unit leg without --unit was not refused: %v", err)
	}

	// goose-unit is the Goose leg's name, tied to its driver: the reader's
	// plant, the Go driver under --leg goose-unit --unit tblp1, is refused,
	// and so is the Goose leg over another unit than --unit
	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "goose-unit", ledgerDir: dir, unit: "tblp1", driver: "build/conformance-go wire-fuzz"})
	if err == nil || !strings.Contains(err.Error(), "--leg goose-unit runs the Goose unit leg") {
		t.Fatalf("the Go driver ran under --leg goose-unit: %v", err)
	}
	goose := fakeGooseLeg(t)
	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "goose-unit", ledgerDir: dir, unit: "listdemo", driver: goose + " tblp1"})
	if err == nil || !strings.Contains(err.Error(), "--leg goose-unit runs the Goose unit leg") {
		t.Fatalf("the Goose leg of another unit ran under --unit listdemo: %v", err)
	}

	// the one-unit leg with its unit and its driver: past the leg checks and
	// the roster check (its unit answers every root), stopped by the fake
	// leg's pipe
	err = runFakeWireFuzz(t, m, wireFuzzOptions{leg: "goose-unit", ledgerDir: dir, unit: "listdemo", driver: goose + " listdemo"}, "shapes.Bounds")
	if err == nil {
		t.Fatal("the fake leg closed its pipe, yet the run passed")
	}
	if strings.Contains(err.Error(), "one-unit leg") || strings.Contains(err.Error(), "seeds absent") {
		t.Fatalf("a one-unit leg run over its unit was refused: %v", err)
	}
}
