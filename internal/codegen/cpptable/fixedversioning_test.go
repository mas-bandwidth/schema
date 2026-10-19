package cpptable

// THE VERSIONING HARNESS ON THE CPP LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). ctable and gotable each
// carry one, generate source, compile it and run a probe against the corpus;
// cpptable has none — this file is the place, proven by exactly one row, §5.8
// row 4 `writer_bound_count`.
//
// THE LINEAGE COMES FROM THE SIBLING-FILENAME CONVENTION, not handed-in
// entries: GenerateLineage takes a nil lock and reads the VOLD_ peer off the
// sibling path — §5.8 row 1's INTERIM. A row is probed by copying
// VOLD_/VNEW_<row>.schema into one temp dir and loading the NEW one.
//
// The bytes are the C++ reference's corpus (build/fixedform-corpus), the same
// byte oracle the C leg reads.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the harness ------------------------------------------------------------

func cppFixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_array_bounded_grow.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; under SCHEMA_REQUIRE_CORPUS a missing file means the
		// build did not do what it was told — and a suite that skips itself there
		// would report green over the row having never run.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

// cppFixedRoot is §5.9 #9's answer for a corpus row, copied from the C leg's
// cFixedRootName (a different package, so it cannot be called): the file's
// root is the OUTER table, the fixed table no other fixed table of the unit
// names by value. A row whose change is NESTED declares two fixed tables — the
// nested type and the root that reaches it — and the root is the one no other
// fixed table names by value.
func cppFixedRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st
		}
	}
	return roots[len(roots)-1]
}

func cppFixedRootName(t *testing.T, u *ir.Unit) string {
	t.Helper()
	return cppFixedRoot(t, u).Name
}

// cppProbeOption switches the lineage harness's refusal proof. `poison` fills
// the destination with 0x5A after the Reset loop and before the load, then
// asserts REFUSE left every byte at 0x5A — the same proof the identity row's
// check lambda runs in fixedbound_test.go. `plant` is the negative control: a
// scratch copy of the generated reader that writes the destination on a named
// refusal, so a row whose poison assertion is missing goes green under it.
// Both are off by default; the clean rows that share this harness are not
// affected.
type cppProbeOption struct {
	poison bool
	plant  bool
}

var (
	cppProbePoison = cppProbeOption{poison: true}
	cppProbePlant  = cppProbeOption{plant: true}
)

// cppRunVersionProbe generates the READER's unit — the VNEW_ file copied
// beside its VOLD_ peer, which is how a nil lock finds the lineage — compiles
// a probe and runs it against `file`. `forge` is C++ that damages the bytes
// before the load (the forged count word), and `body` is the assertions after
// it. The type-wire headers come from cpp.Generate and the table headers from
// GenerateLineage with a nil lock, exactly the way the compiler driver merges
// the two.
func cppRunVersionProbe(t *testing.T, row, file, body, forge string, opts ...cppProbeOption) ([]byte, error) {
	t.Helper()
	var poison, plant bool
	for _, o := range opts {
		poison = poison || o.poison
		plant = plant || o.plant
	}
	dir := t.TempDir()
	oldData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	newData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VNEW_"+row+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "VOLD_"+row+".schema")
	newPath := filepath.Join(dir, "VNEW_"+row+".schema")
	if err := os.WriteFile(oldPath, oldData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, newData, 0o644); err != nil {
		t.Fatal(err)
	}

	u := loadUnit(t, newPath)

	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	var header string
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}

	// THE PLANT (negative control, switched on only by a test): a scratch copy
	// of the generated reader whose step-11 named refusal also writes the
	// destination's first byte. `values` and `capacity` are the FixedLoad
	// parameters in scope there. Nothing is edited on disk in the repo; this is
	// the header file the probe below compiles.
	if plant {
		const refusal = "report->refused = true;\n                report->reason = why;"
		const planted = "report->refused = true;\n                if ( values != NULL && capacity > 0 ) { ((uint8_t *) values)[0] = 0x00; } /* THE PLANT writes on a refusal */\n                report->reason = why;"
		text := string(files[header])
		if !strings.Contains(text, refusal) {
			t.Fatalf("the plant found no step-11 refusal in the generated reader:\n%s", text)
		}
		files[header] = []byte(strings.ReplaceAll(text, refusal, planted))
	}

	outDir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// THE INTERIM MUST HAVE FIRED before the probe is trusted: the header has
	// to carry a FixedLineage[] with more than the identity — the VOLD_ peer
	// picked up by the sibling-filename convention. Identity-only means the
	// convention missed the peer and the row cannot be probed; inventing a lock
	// to work around it is not allowed.
	text := string(files[header])
	if !strings.Contains(text, "FixedLineage[]") {
		t.Fatalf("the generated header carries no FixedLineage[]; the nil-lock convention did not fire:\n%s", text)
	}
	if n := strings.Count(text, "ull, //"); n <= 1 {
		t.Fatalf("the nil-lock convention picked up no VOLD_ peer (identity only, %d note); the row cannot be probed:\n%s", n, text)
	}

	root := cppFixedRootName(t, u)
	pkg := u.Package

	// THE POISON GOES AFTER THE RESET LOOP, BEFORE THE LOAD: before the Reset
	// and the Reset erases it, leaving the row the vacuity this proof repairs.
	// The check is the identity row's, byte for byte: a named refusal wrote no
	// destination byte, so every byte is still the poison.
	poisonCode := ""
	checkCode := ""
	if poison {
		poisonCode = `    memset( back, 0x5A, sizeof( back ) );`
		checkCode = `
    if ( r.refused )
    {
        const uint8_t * poisoned = (const uint8_t *) back;
        for ( int64_t i = 0; i < (int64_t) sizeof( back ); ++i )
            if ( poisoned[i] != 0x5A ) { printf( "REFUSE wrote destination byte %lld: 0x%02x\n", (long long) i, poisoned[i] ); return 1; }
    }`
	}

	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

using namespace %s;

int main( void )
{
    static uint8_t data[1 << 20];
    int64_t len;
    FILE * f = fopen( %q, "rb" );
    %s back[8];
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t n;
    if ( f == NULL ) { printf( "the corpus file is missing\n" ); return 1; }
    len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    if ( len < 20 ) { printf( "the corpus file is short\n" ); return 1; }
%s
    memset( back, 0, sizeof( back ) );
    for ( int k = 0; k < 8; ++k ) { %sReset( back[k] ); }
%s
    memset( &r, 0, sizeof( r ) );
    n = %sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
%s
%s
    return 0;
}
`, header, pkg, file, root, forge, root, poisonCode, root, checkCode, body)

	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + serialize, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

// ---- the rows ---------------------------------------------------------------
//
// THE NEW-READS-OLD ROWS SUITE. This leg landed ten fixed-table tests and no
// rows suite, while every other leg carries the twenty-six §5.7 rows; this is
// that suite. THE VALUE ORACLE is the corpus manifest (§5.9 #32), read per
// FILE and in DECIMAL, not from the bytes under test. The brackets are not
// constant across rows — 30 corpus files bracket lead=2863311530 (0xAAAAAAAA)
// / trail=3149642683 (0xBBBBBBBB) and 8 bracket lead=1 / trail=2 — so a
// constant assertion is wrong for eight of them, and reading them as hex is
// how this branch was dead once. THE GATE IS DERIVED FROM THE IR AND NOT FROM
// A FLAG: the OLD root's Fields say whether the `lead`/`trail` pair exists,
// and a hand-kept list beside the row drifts into a row that silently stops
// asserting a value.
//
// A POISON HOOK NOW EXISTS, and a named residual remains. cppRunVersionProbe
// can write 0x5A after the Reset loop and before the load (cppProbePoison),
// and every refused-read row that reads through a VOLD_ lineage passes it and
// proves REFUSE left every destination byte at 0x5A. TestFixedVersioning-
// LineageRefusalLeavesDestinationUntouched runs those rows under a plant — a
// scratch reader that writes on a refusal — and requires each to go red, so
// the poison check cannot silently disappear. The per-row CHECK bodies are
// still absent — c's array_fixed_grow, field_append, fixed_I_grow_element,
// int_widen and uint_widen each carry one — and a row that asserts an ELEMENT
// DEFAULT still needs the poison threaded into its own body, not just its
// refusal. OLD-REFUSES-NEW is the second column and is not in this suite. ONE
// ROW IS CLOSED ELSEWHERE: fixed_I_grow_element's check body (the four widened
// slots -1, -128, 0, 112, the brackets, the counters) lives in
// fixedroadmap_t12_test.go's
// TestFixedRoadmapCppT12PlansEvolution/cpp/E3/fixed-array-element/new-reads-old,
// and its OLD-REFUSES-NEW column (TableFixedReason::layout_unknown, the file's hash, every counter
// zero, the poisoned destination untouched) in .../old-refuses-new, which
// compiles the OLD generation alone through cppRunCapability. A named residual
// is honest, a silent one is the bug this suite exists to close.

type cppVersionRow struct {
	row string
	// sameHash is a row whose edit moves no layout byte and no digest byte
	// (`rename_without_was`, `field_deprecate`, `field_undeprecate`): the two
	// hashes are equal, both sides take the identity plan and the file READS in
	// BOTH directions — which is the test that the hash, and only the hash, is
	// the version (§5.7).
	sameHash bool
	// widens is a row that grows a WIDTH, so `widened` moves at least once;
	// every other row is an APPEND and owes all six counters at zero (§5.9 #10).
	widens bool
	// unported is a row this leg's harness cannot probe, cited by name (§5.9
	// #44): the nil-lock sibling convention has no DISTINCT VOLD_ peer to
	// compile against, so the row is skipped BY NAME, never green and never
	// red — a green folds an unrun row into a pass, a red blames the row for
	// the harness. A reader may not read either as "this row passed".
	unported string
}

var cppVersionRows = []cppVersionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true},
	{row: "array_fixed_grow"},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append"},
	{row: "field_deprecate", sameHash: true, unported: "sameHash: the two sides carry equal hashes, so the nil-lock sibling convention collapses to the identity entry alone (one lineage note) and cppRunVersionProbe's interim guard refuses to probe a reader with no distinct VOLD_ peer. The row reads in both directions by §5.7, but proving it needs an explicit lock, which is a change to cppRunVersionProbe's signature this card is not"},
	{row: "field_undeprecate", sameHash: true, unported: "sameHash: the two sides carry equal hashes, so the nil-lock sibling convention collapses to the identity entry alone (one lineage note) and cppRunVersionProbe's interim guard refuses to probe a reader with no distinct VOLD_ peer. The row reads in both directions by §5.7, but proving it needs an explicit lock, which is a change to cppRunVersionProbe's signature this card is not"},
	{row: "fixed_I_grow", widens: true},
	{row: "fixed_I_grow_element", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true},
	{row: "keyed_array_enum_append"},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true, unported: "sameHash: the two sides carry equal hashes, so the nil-lock sibling convention collapses to the identity entry alone (one lineage note) and cppRunVersionProbe's interim guard refuses to probe a reader with no distinct VOLD_ peer. The row reads in both directions by §5.7, but proving it needs an explicit lock, which is a change to cppRunVersionProbe's signature this card is not"},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------
//
// The shared body is the C leg's, translated to this leg's spellings: a clean
// read is `r.reason != TableMessageReason::newer_form` (never 0 — see
// fixedversioning_count_clamp_ends_test.go), and the counters r.unknown,
// r.kind_mismatch, r.clamped, r.duplicate must all stay zero. The widened
// counter is zero for an APPEND and nonzero for a WIDEN — an append the reader
// knows is not an event (§5.9 #10). Every row whose ROOT declares the
// `lead`/`trail` pair also asserts the oracle's brackets by name.
func TestFixedVersioningNewReadsOld(t *testing.T) {
	corpus := cppFixedCorpus(t)
	for _, r := range cppVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			if r.unported != "" {
				t.Skipf("UNPORTED (§5.9 #44), not green and not red: %s", r.unported)
			}
			older := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+r.row+".schema"))
			root := cppFixedRoot(t, older)
			lead, trail := cppBrackets(t, corpus, r.row, ir.TableFixedTypeBytes(root))
			bracket := ""
			if cppHasField(root, "lead") && cppHasField(root, "trail") {
				bracket = fmt.Sprintf("\n    if ( back[0].lead != 0x%08Xu || back[0].trail != 0x%08Xu )\n        { printf( \"the row moved a neighbour: lead=%%u trail=%%u\\n\", back[0].lead, back[0].trail ); return 1; }", lead, trail)
			}
			counters := `if ( r.widened != 0 ) { printf( "widened %d, and an append is not an event\n", r.widened ); return 1; }`
			if r.widens {
				counters = `if ( r.widened == 0 ) { printf( "a widening row moved no widened counter\n" ); return 1; }`
			}
			body := fmt.Sprintf(`
    if ( n < 1 ) { printf( "the newer reader refused the older writer's file: n=%%lld reason=%%d\n", (long long) n, (int) r.reason ); return 1; }
    if ( r.reason != TableMessageReason::newer_form || r.malformed || r.refused ) { printf( "a clean NEW-READS-OLD is not a refusal: reason=%%d malformed=%%d\n", (int) r.reason, (int) r.malformed ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "counters moved on a clean backward read: u=%%d km=%%d c=%%d d=%%d\n", r.unknown, r.kind_mismatch, r.clamped, r.duplicate ); return 1; }
    %s
    %s
`, counters, bracket)
			out, err := cppRunVersionProbe(t, r.row, filepath.Join(corpus, "old_"+r.row+".bin"), body, "")
			if err != nil {
				t.Fatalf("NEW-READS-OLD %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- the value oracle --------------------------------------------------------

// cppBrackets is §5.9 #32's value oracle: per row the `lead` and `trail` the
// corpus brackets it with, so a mislaid size moves a neighbour and the row
// says so by name. THE MANIFEST is read when the tree carries one
// (`build/fixedform-corpus/manifest.txt`), and where it does not the values
// the manifest would carry are taken FROM THE CORPUS BYTES — which is where
// the manifest itself reads them: the lead is the first four bytes of the
// first record's body and the trail the last four.
func cppBrackets(t *testing.T, corpus, row string, bodyBytes int64) (uint32, uint32) {
	t.Helper()
	if lead, trail, ok := cppManifestBrackets(t, corpus, "old_"+row+".bin"); ok {
		return lead, trail
	}
	data, err := os.ReadFile(filepath.Join(corpus, "old_"+row+".bin"))
	if err != nil || len(data) < 20 {
		t.Fatalf("the corpus row %s is not readable: %v", row, err)
	}
	at := 20 + int(binary.LittleEndian.Uint32(data[16:20])) + 8
	if int64(at)+bodyBytes > int64(len(data)) {
		t.Fatalf("the corpus row %s is shorter than its own record", row)
	}
	body := data[at : int64(at)+bodyBytes]
	return binary.LittleEndian.Uint32(body[:4]),
		binary.LittleEndian.Uint32(body[len(body)-4:])
}

// cppManifestBrackets reads ONE FILE'S row out of the corpus manifest, which
// is the VALUE ORACLE and not the bytes under test (§5.9 #32). A line is
// `file=<name> row=<row> side=<old|new|none> root=<R> records=<n>
// values=<k>=<v>,...` — the FILE is the first field and the key, because one
// row has two sides and they carry different values; the brackets live inside
// `values=` spelled `r0.lead` and `r0.trail`; and EVERY NUMBER IS DECIMAL.
// Matching the row against the first field, or reading the brackets as hex,
// is how this branch was dead. The manifest is not tracked, so `ok` false is
// "the tree has no manifest" and the caller falls back to the corpus bytes —
// but a manifest that IS there and does not carry this file is a FAILURE BY
// NAME, never a silent fallback.
// cppManifestFloatBits is the FLOAT half of the corpus oracle, and it exists because this
// file's `...ManifestRow` cannot carry one: that function parses every value as
// a uint64 and SILENTLY SKIPS anything else, so the manifest's own
// `r0.aim=0.300000012|0x3E99999A` was unreadable by the bracket path BY
// CONSTRUCTION — and every leg's compressed-float row asserted a HARDCODED
// literal instead of the reference's answer (schema#1164, measured 2026-09-19
// across all nine legs; Glenn: "do whatever is needed to make sure that a new
// reader can read an old writer").
//
// THIS LEG ALREADY COMPARED BITS, which is the right comparison and is why it
// is one of the cheap three. What it did not do is take the number from the
// REFERENCE: a literal in a test is an oracle written by the same hand as the
// code under test, and it agrees with a requantizing reader the moment somebody
// "fixes" it to match.
//
// SECTION is "values" or "forged"; a forged key carries its byte offset, so a
// key equal to `path` or beginning `path+"@"` is the one asked for. It FAILS
// LOUDLY three ways — no manifest, no row, no such value — because a float
// oracle that can degrade to "not checked" is the vacuity this repairs.
func cppManifestFloatBits(t *testing.T, corpus, file, section, path string) uint32 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is not readable, so this row has no oracle: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		var values string
		named := false
		for field := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				named = v == file
			case section:
				values = v
			}
		}
		if !named {
			continue
		}
		for pair := range strings.SplitSeq(values, ",") {
			k, v, ok := strings.Cut(pair, "=")
			if !ok || (k != path && !strings.HasPrefix(k, path+"@")) {
				continue
			}
			_, hexPart, ok := strings.Cut(v, "|0x")
			if !ok {
				t.Fatalf("the manifest's %s for %s is %q, which carries no |0x<bits> half: a decimal alone is not a bit-exact oracle", path, file, v)
			}
			bits, err := strconv.ParseUint(hexPart, 16, 32)
			if err != nil {
				t.Fatalf("the manifest's %s for %s has unreadable bits %q: %v", path, file, hexPart, err)
			}
			return uint32(bits)
		}
		t.Fatalf("the manifest's %s for %s carries no %s — the oracle and the bytes under test have come apart", section, file, path)
	}
	t.Fatalf("the manifest carries no row for %s — the oracle and the bytes under test have come apart", file)
	return 0
}

func cppManifestBrackets(t *testing.T, corpus, file string) (uint32, uint32, bool) {
	t.Helper()
	f, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		values, ok := cppManifestRow(line, file)
		if !ok {
			continue
		}
		lead, haveLead := values["r0.lead"]
		trail, haveTrail := values["r0.trail"]
		if !haveLead || !haveTrail {
			// A ROW WHOSE TABLE DECLARES NO BRACKETS (`enum_append`'s does not)
			// has nothing here to read, and the caller reads the bytes' own
			// first and last four instead. That is not the dead branch: the row
			// WAS found, and a row that is not found still fails by name.
			return 0, 0, false
		}
		return uint32(lead), uint32(trail), true
	}
	t.Fatalf("the manifest carries no row for %s — the oracle and the bytes under test have come apart", file)
	return 0, 0, false
}

// cppManifestRow answers one line's `values=` map when its `file=` is the one
// asked for. The values are `<path>=<number>` pairs separated by commas, and a
// pair whose value is not a number — a quoted string, a bool — is not a
// bracket and is skipped.
func cppManifestRow(line, file string) (map[string]uint64, bool) {
	var values string
	named := false
	for field := range strings.FieldsSeq(line) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch k {
		case "file":
			named = v == file
		case "values":
			values = v
		}
	}
	if !named {
		return nil, false
	}
	out := map[string]uint64{}
	for pair := range strings.SplitSeq(values, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			out[k] = n
		}
	}
	return out, true
}

// cppHasField answers whether a row's ROOT declares a member by that name. It
// is how the bracket check knows which rows carry §5.9 #32's `lead`/`trail`
// pair, and it is read off the IR on purpose: a flag kept by hand beside each
// row would drift the first time a schema gained or lost the pair, and the
// failure mode of that drift is a row that silently stops asserting a landed
// value — which is exactly the vacuity being repaired here.
func cppHasField(st *ir.Struct, name string) bool {
	for _, f := range st.Fields {
		if f != nil && f.Name == name {
			return true
		}
	}
	return false
}

// ---- the row ----------------------------------------------------------------

// TestFixedVersioningWriterBoundCount is §5.8 row 4: VOLD_/VNEW_
// array_bounded_grow — vals [..4]int32 on the OLD side, [..8]int32 on the NEW
// — plus old_array_bounded_grow.bin with the count word forged to 7, BETWEEN
// the writer's 4 and the reader's 8. The NEW build reads the forged OLD file
// through its lineage, so the plan is COMPILED and the count's bound is the
// WRITER's (4). A count past the PLAN's bound refuses `count_over_bound` by
// name, the Go oracle's lineage-count row (internal/codegen/gotable/
// fixedversioning_test.go:217-245). Nothing is decoded, no counter moves, and
// the destination is poisoned 0x5A and proved still 0x5A byte for byte.
func TestFixedVersioningWriterBoundCount(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := `
    if ( n != -1 ) { printf( "writer_bound_count: a forged count past the PLAN's bound did not refuse: n=%lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "writer_bound_count: refused is false\n" ); return 1; }
    if ( r.reason != count_over_bound ) { printf( "writer_bound_count: reason=%d, not count_over_bound\n", (int) r.reason ); return 1; }
    if ( r.malformed ) { printf( "writer_bound_count: malformed set — a refusal by name is not malformed\n" ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "writer_bound_count: a counter moved: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
`
	forge := `
    /* THE FORGE (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8 row 4): the OLD
       record is lead 0xAAAAAAAA then vals_count 4, both little-endian uint32,
       so the eight-byte needle is AA AA AA AA 04 00 00 00. Find it, assert it
       occurs EXACTLY once — a locator that matches twice is not a locator —
       and write the little-endian 7 over the count word, a value BETWEEN the
       writer's 4 and the reader's 8. */
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator matched twice: %lld and %lld\n", (long long) found, (long long) i ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator matched nowhere\n" ); return 1; }
        TableFixedPut32( data + found + 4, 7 ); /* the forge */
    }
`
	out, err := cppRunVersionProbe(t, "array_bounded_grow",
		filepath.Join(corpus, "old_array_bounded_grow.bin"), body, forge, cppProbePoison)
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
	}
}

// ---- the lineage refusal rows and their plant -----------------------------
//
// THE LINEAGE REFUSAL ROWS are the rows that read an OLD writer's file through
// the VOLD_ lineage and expect a named refusal: the writer-bound count
// (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8 row 4), the two count-clamp ends,
// and the count/length rows transcribed from the Go oracle
// (internal/codegen/gotable/fixedversioning_test.go:217-245 and
// gotable/fixedbound_test.go). Each poisons the destination 0x5A after the
// Reset loop and before the load, and the shared harness asserts REFUSE left
// every byte at 0x5A. The rows here WRITE the OLD bytes with the OLD writer in
// the probe (the t18 pair's unit), so the plant check runs on a tree without
// the reference's byte oracle.
//
// THE PLANT is a scratch copy of the generated reader whose step-11 named
// refusal also writes values[0]: a row that forgot the poison assertion stays
// green under it. TestFixedVersioningLineageRefusalLeavesDestinationUntouched
// runs every row above against the plant and requires each to go red, so the
// plant check is a test rather than a hand step. The same rows are green with
// the plant off.

type cppLineageRefusalRow struct {
	name   string
	row    string
	writer string
	forge  string
	body   string
}

const (
	// The OLD writer's record for array_bounded_grow: lead 0xAAAAAAAA,
	// vals_count 4, then four values. Its count/length word is the forge site.
	cppArrayBoundedWriter = `
    want.lead = 0xAAAAAAAAu;
    want.trail = 0xBBBBBBBBu;
    want.vals_count = 4;
    for ( int k = 0; k < 4; ++k ) { want.vals[k] = 100 + k; }
`
	// The OLD writer's record for string_grow: lead 0xAAAAAAAA, text_length 8,
	// then eight characters.
	cppStringGrowWriter = `
    want.lead = 0xAAAAAAAAu;
    want.trail = 0xBBBBBBBBu;
    want.text_length = 8;
    for ( int k = 0; k < 8; ++k ) { want.text[k] = 'a'; }
`
)

// cppLineageCountForge finds the OLD record's lead 0xAAAAAAAA followed by the
// count 4, asserts it occurs exactly once, and writes value over the count.
func cppLineageCountForge(value uint32, slack bool) string {
	s := fmt.Sprintf(`
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator matched twice: %%lld and %%lld\n", (long long) found, (long long) i ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator matched nowhere\n" ); return 1; }
        TableFixedPut32( data + found + 4, %du );
`, value)
	if slack {
		s += "        for ( int64_t i = found + 8; i < len && i < found + 8 + 4LL * 4; ++i ) data[i] = 0xFF;\n"
	}
	return s + "    }\n"
}

// cppLineageLengthForge is the same locator over string_grow's text_length 8.
func cppLineageLengthForge(value uint32, slack bool) string {
	s := fmt.Sprintf(`
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x08, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator matched twice: %%lld and %%lld\n", (long long) found, (long long) i ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator matched nowhere\n" ); return 1; }
        TableFixedPut32( data + found + 4, %du );
`, value)
	if slack {
		s += "        for ( int64_t i = found + 8; i < len && i < found + 8 + 8; ++i ) data[i] = 0xFF;\n"
	}
	return s + "    }\n"
}

func cppLineageRefusalBody(reason string) string {
	return fmt.Sprintf(`
    if ( n != -1 ) { printf( "lineage refusal: n=%%lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "lineage refusal: refused is false\n" ); return 1; }
    if ( r.reason != %s ) { printf( "lineage refusal: reason=%%d, want %s\n", (int) r.reason ); return 1; }
    if ( r.malformed ) { printf( "lineage refusal: malformed set — a refusal by name is not malformed\n" ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "lineage refusal: a counter moved: clamped=%%d unknown=%%d kind_mismatch=%%d widened=%%d duplicate=%%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
`, reason, reason)
}

var cppLineageRefusalRows = []cppLineageRefusalRow{
	{
		name:   "writer_bound_count",
		row:    "array_bounded_grow",
		writer: cppArrayBoundedWriter,
		forge:  cppLineageCountForge(7, false),
		body:   cppLineageRefusalBody("count_over_bound"),
	},
	{
		name:   "count_clamp_end_negative",
		row:    "array_bounded_grow",
		writer: cppArrayBoundedWriter,
		forge:  cppLineageCountForge(0xFFFFFFFF, false),
		body:   cppLineageRefusalBody("count_over_bound"),
	},
	{
		name:   "count_clamp_end_past_reader",
		row:    "array_bounded_grow",
		writer: cppArrayBoundedWriter,
		forge:  cppLineageCountForge(9, false),
		body:   cppLineageRefusalBody("count_over_bound"),
	},
	{
		name:   "lineage_count_over_bound",
		row:    "array_bounded_grow",
		writer: cppArrayBoundedWriter,
		forge:  cppLineageCountForge(200, false),
		body:   cppLineageRefusalBody("count_over_bound"),
	},
	{
		name:   "lineage_count_over_bound_with_slack",
		row:    "array_bounded_grow",
		writer: cppArrayBoundedWriter,
		forge:  cppLineageCountForge(200, true),
		body:   cppLineageRefusalBody("count_over_bound"),
	},
	{
		name:   "lineage_length_over_bound",
		row:    "string_grow",
		writer: cppStringGrowWriter,
		forge:  cppLineageLengthForge(99, false),
		body:   cppLineageRefusalBody("length_over_bound"),
	},
	{
		name:   "lineage_length_over_bound_with_slack",
		row:    "string_grow",
		writer: cppStringGrowWriter,
		forge:  cppLineageLengthForge(99, true),
		body:   cppLineageRefusalBody("length_over_bound"),
	},
}

// cppLineageOldFile writes one lineage row's OLD file with the OLD generation's
// own writer: values set by hand so the forge site is known, saved through
// <root>FixedSave and written where cppRunVersionProbe can read it. This is the
// t18 pair's writer unit, so the plant row does not need the corpus.
func cppLineageOldFile(t *testing.T, row, writer string) string {
	t.Helper()
	pair := cppT18Generate(t, row)
	path := filepath.Join(t.TempDir(), "old_"+row+".bin")
	cppT18Run(t, pair, fmt.Sprintf(`
    static uint8_t buffer[1 << 16];
    %[1]s want;
    memset( &want, 0, sizeof( want ) );
%[2]s
    const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
    if ( written < 0 ) { printf( "the lineage row's OLD writer failed\n" ); return 1; }
    FILE * f = fopen( %[3]q, "wb" );
    if ( f == NULL ) { printf( "the lineage row's file did not open\n" ); return 1; }
    if ( fwrite( buffer, 1, (size_t) written, f ) != (size_t) written ) { printf( "the lineage row's file was short written\n" ); return 1; }
    fclose( f );
`, pair.root, writer, path))
	return path
}

func TestFixedVersioningLineageRefusalLeavesDestinationUntouched(t *testing.T) {
	t.Parallel()
	for _, r := range cppLineageRefusalRows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			file := cppLineageOldFile(t, r.row, r.writer)

			// WITHOUT THE PLANT the row is green: the refusal is the named one
			// and the poisoned destination keeps every byte.
			out, err := cppRunVersionProbe(t, r.row, file, r.body, r.forge, cppProbePoison)
			if err != nil {
				t.Fatalf("the lineage row is red without the plant: %v\n%s", err, out)
			}

			// WITH THE PLANT the reader writes values[0] on the refusal, so the
			// row must go red naming the destination byte. A row that stays green
			// here has no poison assertion, which is the vacuity this pins.
			out, err = cppRunVersionProbe(t, r.row, file, r.body, r.forge, cppProbePoison, cppProbePlant)
			if err == nil || !strings.Contains(string(out), "REFUSE wrote destination byte") {
				t.Fatalf("the plant wrote the destination on a refusal and the row stayed green: err=%v\n%s", err, out)
			}
		})
	}
}
