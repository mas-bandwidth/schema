package cpptable

// THE C++ LEG'S APPENDS-AND-DEPRECATIONS CARD (docs/roadmap.sexp's
// field-evolution/cpp node): cpp/E8 "Append and deprecate under the backward-read
// contract" and cpp/R19 "a clean NEW-READS-OLD of an appended field, variant,
// arm, flag or keyed slot moves no counter at all". The laws are
// docs/SPEC-TABLES.md §3.4 (the fixed form; a DEPRECATED FIELD KEEPS ITS SLOT
// FOREVER) and docs/FIXED-FORM-ALGORITHM.md §5 (the load; §5.3's steps, §5.4's
// counters).
//
// THE OLD FILE IS BUILT HERE IN GO, the way the C leg's ctable/t05 does
// (fixedroadmap_t05_test.go: cT05OldFile): header, layout length, layout, one
// record (its per-record hash then the body), laid down from the VOLD_ unit's
// own IR. No build/fixedform-corpus read, no C++ reference byte oracle.
//
// TWO TIERS. TestFixedCppT18OldFiles is the unit tier: in process only — no
// compiler, no clock, no socket, no network — it proves the Go-built old file
// carries the hash and layout the generator emits, and that the NEW reader
// generated with an explicit lock names the OLD hash in its lineage. Its
// negative control is reversible and in memory. TestFixedRoadmapCppT18FieldEvolution
// is the functional tier: its cpp/E8 and cpp/R19 subtests compile and RUN the
// generated C++ (cppRunVersionProbe's probe), skip by name when no compiler is on
// PATH and SCHEMA_REQUIRE_CORPUS is unset, and fail by name when it is set.

import (
	"bytes"
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
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppT18VoldUnits are the six VOLD_ units this card reads: the five appended
// kinds cpp/R19 names (field, variant, arm, flag, keyed slot) plus the
// deprecated-field pair cpp/E8 needs.
var cppT18VoldUnits = []string{
	"field_append",
	"field_deprecate",
	"enum_append",
	"union_append",
	"flags_append",
	"keyed_array_enum_append",
}

// cppT18R19Kinds are the five appended kinds, each the VOLD_/VNEW_ pair the
// title names: field (field_append), variant (enum_append), arm (union_append),
// flag (flags_append) and keyed slot (keyed_array_enum_append).
var cppT18R19Kinds = []string{
	"field",
	"variant",
	"arm",
	"flag",
	"keyed-slot",
}

// cppT18Pair maps an R19 kind to the test/tables row it reads.
var cppT18Pair = map[string]string{
	"field":      "field_append",
	"variant":    "enum_append",
	"arm":        "union_append",
	"flag":       "flags_append",
	"keyed-slot": "keyed_array_enum_append",
}

// cppT18OldBytes builds a fixed-form FILE in the OLD writer's layout, in Go,
// from the same IR the emitter lays down — header, layout length, layout, one
// record (per-record hash then the body) — a port of the C leg's cT05OldFile.
// `values` is the record body, every field at its bound, in declared order.
func cppT18OldBytes(t *testing.T, u *ir.Unit, values []byte) []byte {
	t.Helper()
	st := cppFixedRoot(t, u)
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	hash := ir.TableFixedLayoutHash(layout, st)
	body := ir.TableFixedTypeBytes(st)
	if int64(len(values)) != body {
		t.Fatalf("the old record body is %d bytes, the values are %d", body, len(values))
	}
	var b bytes.Buffer
	hdr := make([]byte, 16)
	hdr[0] = 3 // the fixed form byte
	binary.LittleEndian.PutUint64(hdr[8:], hash)
	b.Write(hdr)
	var ln [4]byte
	binary.LittleEndian.PutUint32(ln[:], uint32(len(layout)))
	b.Write(ln[:])
	b.Write(layout)
	var rh [8]byte
	binary.LittleEndian.PutUint64(rh[:], hash)
	b.Write(rh[:])
	b.Write(values)
	return b.Bytes()
}

// cppT18OldFile is cppT18OldBytes written to a temp file, which is what the
// compiled probe opens.
func cppT18OldFile(t *testing.T, row string, values []byte) string {
	t.Helper()
	u := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema"))
	p := filepath.Join(t.TempDir(), "old_"+row+".bin")
	if err := os.WriteFile(p, cppT18OldBytes(t, u, values), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// cppT18Body is a zeroed record body of the OLD root's declared size.
func cppT18Body(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	return make([]byte, ir.TableFixedTypeBytes(cppFixedRoot(t, u)))
}

// cppT18WireOffset is a leaf's byte offset in the WIRE record body, read off the
// SAME plan the emitter compiles (ir.TableFixedBuildPlanNormalisedWithPool) and
// keyed by the layout entry's Note, which is the field name. The in-memory
// RecordLayout offset is not this: the fixed body is the packed canonical
// layout, so only the plan's Src names a body byte.
func cppT18WireOffset(t *testing.T, u *ir.Unit, name string) int {
	t.Helper()
	plan, _, _ := ir.TableFixedBuildPlanNormalisedWithPool(u, cppFixedRoot(t, u))
	for _, e := range plan {
		if e.Note == name {
			return int(e.Src)
		}
	}
	var notes []string
	for _, e := range plan {
		notes = append(notes, fmt.Sprintf("%s@%d+%d", e.Note, e.Src, e.Size))
	}
	t.Fatalf("no leaf %q in the fixed plan of %s: %s", name, u.Package, strings.Join(notes, " "))
	return 0
}

// cppT18FirstRun is the first coalesced plan run: its Note, wire offset and
// size. Scalars the coalescer merged (x,y,z int32 is one twelve-byte run) have
// no per-field plan entry, so a test that wants a field inside such a run takes
// the run and adds the field's declared offset.
func cppT18FirstRun(t *testing.T, u *ir.Unit) (int, int) {
	t.Helper()
	plan, _, _ := ir.TableFixedBuildPlanNormalisedWithPool(u, cppFixedRoot(t, u))
	if len(plan) == 0 {
		t.Fatalf("the fixed plan of %s is empty", u.Package)
	}
	return int(plan[0].Src), int(plan[0].Size)
}

// cppT18Set puts an int32 at a named leaf's wire offset.
func cppT18Set(t *testing.T, u *ir.Unit, body []byte, name string, v uint32) {
	t.Helper()
	binary.LittleEndian.PutUint32(body[cppT18WireOffset(t, u, name):], v)
}

// cppT18Lock is the EXPLICIT lock this card's same-hash pair (field_deprecate)
// needs: cppRunVersionProbe reads the VOLD_ peer off the sibling-filename
// convention with a nil lock, and for a pair whose two sides carry EQUAL hashes
// that convention collapses to the identity entry alone (lineage.go's `add`
// de-duplicates by hash), so the interim guard refuses to probe the reader at
// all. Here the OLD unit's own live lineage entry is prepended to the NEW
// unit's lock, so the reader holds the older side explicitly. Oldest first, the
// reader's own layout last — the shape lockfile.Lineage expects.
func cppT18Lock(t *testing.T, newU, oldU *ir.Unit) *lockfile.Unit {
	t.Helper()
	lock := lockfile.Render(newU)
	oldLock := lockfile.Render(oldU)
	for _, st := range ir.TableFixedRoots(oldU) {
		tbl := lock.Table(st.WireName())
		if tbl == nil {
			t.Fatalf("the NEW lock has no table %s", st.WireName())
		}
		oldTbl := oldLock.Table(st.WireName())
		if oldTbl == nil || len(oldTbl.Lineage) == 0 {
			t.Fatalf("the OLD render has no lineage for %s", st.WireName())
		}
		e := oldTbl.Lineage[len(oldTbl.Lineage)-1]
		tbl.Lineage = append([]lockfile.LineageEntry{e}, tbl.Lineage...)
	}
	return lock
}

// cppT18RequireCompiler is the functional tier's gate: no compiler on PATH with
// SCHEMA_REQUIRE_CORPUS unset is a skip BY NAME; under SCHEMA_REQUIRE_CORPUS it
// is a FAILURE BY NAME, never a silent green.
func cppT18RequireCompiler(t *testing.T) {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set but the C++ compiler %q is not on PATH: %v", cxx, err)
		}
		t.Skipf("no C++ compiler %q on PATH: this functional row is not run (set SCHEMA_REQUIRE_CORPUS to fail instead)", cxx)
	}
}

// cppT18Probe is cppRunVersionProbe's sibling for the explicit-lock rows: it
// generates the NEW unit's headers with the lock cppT18Lock builds, compiles the
// same probe cppRunVersionProbe compiles and runs it against `file`. The
// signature of cppRunVersionProbe and the contents of cppVersionRows are left
// unchanged.
func cppT18Probe(t *testing.T, row, file, body string) ([]byte, error) {
	t.Helper()
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

	newU := loadUnit(t, newPath)
	oldU := loadUnit(t, oldPath)
	lock := cppT18Lock(t, newU, oldU)

	files, err := cpp.Generate(newU)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(newU, lock)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	outDir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	// THE EXPLICIT LOCK MUST HAVE LANDED: the header has to name the OLD wire
	// hash in its lineage array, whichever side of the pair that hash came from.
	text := string(files[header])
	if !strings.Contains(text, "FixedLineage[]") {
		t.Fatalf("the generated header carries no FixedLineage[]; the explicit lock did not land:\n%s", text)
	}
	oldHash := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(cppFixedRoot(t, oldU))), cppFixedRoot(t, oldU))
	if !strings.Contains(text, fmt.Sprintf("0x%016x", oldHash)) {
		t.Fatalf("the generated header does not name the OLD hash 0x%016x in its lineage:\n%s", oldHash, text)
	}

	root := cppFixedRootName(t, newU)
	pkg := newU.Package

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
    memset( back, 0, sizeof( back ) );
    for ( int k = 0; k < 8; ++k ) { %sReset( back[k] ); }
    memset( &r, 0, sizeof( r ) );
    n = %sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
%s
    return 0;
}
`, header, pkg, file, root, root, root, body)

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

// cppT18EmittedLayout pulls the byte list of <root>FixedLayout[] out of an
// emitted header, so the unit tier can hold the Go-built file to the same bytes
// the generator writes down.
func cppT18EmittedLayout(t *testing.T, header, root string) []byte {
	t.Helper()
	marker := "constexpr uint8_t " + root + "FixedLayout[] = {"
	at := strings.Index(header, marker)
	if at < 0 {
		t.Fatalf("the emitted header carries no %sFixedLayout[]", root)
	}
	body := header[at+len(marker):]
	end := strings.Index(body, "};")
	if end < 0 {
		t.Fatalf("the emitted %sFixedLayout[] is unterminated", root)
	}
	var out []byte
	for tok := range strings.SplitSeq(body[:end], ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("the emitted layout token %q is not a byte: %v", tok, err)
		}
		out = append(out, byte(v))
	}
	return out
}

// cppT18GeneratedHeader renders one unit's table headers with the lock handed
// in and returns the Table.h text, keyed by root name.
func cppT18GeneratedHeader(t *testing.T, u *ir.Unit, lock *lockfile.Unit) string {
	t.Helper()
	files, err := GenerateLineage(u, lock)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if strings.HasSuffix(name, "Table.h") {
			return string(data)
		}
	}
	t.Fatalf("no Table.h was generated for %s", u.Package)
	return ""
}

// TestFixedCppT18OldFiles is the unit tier. In process only — loadUnit and the
// generator, no compiler, no clock, no socket, no network. For each of the six
// VOLD_ units it holds the Go-built OLD file's header to the hash and the layout
// bytes the generator emits for that unit's root, checks the record carries the
// same hash, and checks the VNEW_ reader generated with the explicit lock names
// the VOLD_ hash in its lineage. The negative control is reversible and in
// memory: the OLD file built from the VNEW_ side of a hash-changing pair
// (field_append) must NOT match the VOLD_ hash.
func TestFixedCppT18OldFiles(t *testing.T) {
	t.Parallel()
	for _, row := range cppT18VoldUnits {
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			oldU := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema"))
			newU := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VNEW_"+row+".schema"))
			root := cppFixedRoot(t, oldU)
			layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(root))
			hash := ir.TableFixedLayoutHash(layout, root)
			values := cppT18Body(t, oldU)
			file := cppT18OldBytes(t, oldU, values)

			// the header the generator emits for this unit's root
			voldHeader := cppT18GeneratedHeader(t, oldU, nil)
			if !strings.Contains(voldHeader, fmt.Sprintf("0x%016xull", hash)) {
				t.Fatalf("%s: the emitted VOLD_ header does not carry the hash 0x%016x", row, hash)
			}
			if got := cppT18EmittedLayout(t, voldHeader, root.Name); !bytes.Equal(got, layout) {
				t.Fatalf("%s: the Go-built layout is not the generator's own layout (%d vs %d bytes)", row, len(layout), len(got))
			}

			// the Go-built file's header carries that hash, its layout length and
			// its layout, and the record carries the same hash
			if file[0] != 3 {
				t.Fatalf("%s: the old file's form byte is %d, not 3", row, file[0])
			}
			if got := binary.LittleEndian.Uint64(file[8:16]); got != hash {
				t.Fatalf("%s: the old file's header hash is 0x%016x, not 0x%016x", row, got, hash)
			}
			if got := binary.LittleEndian.Uint32(file[16:20]); int(got) != len(layout) {
				t.Fatalf("%s: the old file's layout length is %d, not %d", row, got, len(layout))
			}
			if !bytes.Equal(file[20:20+len(layout)], layout) {
				t.Fatalf("%s: the old file's layout bytes are not the generator's", row)
			}
			recAt := 20 + len(layout)
			if got := binary.LittleEndian.Uint64(file[recAt : recAt+8]); got != hash {
				t.Fatalf("%s: the record hash is 0x%016x, not 0x%016x", row, got, hash)
			}

			// the NEW reader generated with the explicit lock names the VOLD_ hash
			// in its lineage
			newHeader := cppT18GeneratedHeader(t, newU, cppT18Lock(t, newU, oldU))
			if !strings.Contains(newHeader, fmt.Sprintf("0x%016x", hash)) {
				t.Fatalf("%s: the VNEW_ reader's lineage does not name the VOLD_ hash 0x%016x", row, hash)
			}
		})
	}

	// THE NEGATIVE CONTROL, reversible and in memory: field_append is a
	// hash-CHANGING pair, so the file built from the VNEW_ side must not carry
	// the VOLD_ hash. If it did, every assertion above would be vacuous.
	t.Run("negative-control", func(t *testing.T) {
		t.Parallel()
		vold := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VOLD_field_append.schema"))
		vnew := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VNEW_field_append.schema"))
		oldHash := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(cppFixedRoot(t, vold))), cppFixedRoot(t, vold))
		crossed := cppT18OldBytes(t, vnew, cppT18Body(t, vnew))
		if got := binary.LittleEndian.Uint64(crossed[8:16]); got == oldHash {
			t.Fatalf("field_append's two sides share the hash 0x%016x, so this control cannot fail and the assertion above proves nothing", oldHash)
		}
	})
}

// TestFixedRoadmapCppT18FieldEvolution is the card's leaf suite, one subtest per
// leaf. cpp/E8's deprecate half is a SAME-HASH pair (the deprecated field keeps
// its slot, SPEC-TABLES §3.4), so it is probed through cppT18Probe's explicit
// lock; its append half and every cpp/R19 kind go through the same helper.
func TestFixedRoadmapCppT18FieldEvolution(t *testing.T) {
	t.Parallel()

	// cpp/E8 "Append and deprecate under the backward-read contract"
	// (docs/SPEC-TABLES.md §3.4 "A DEPRECATED FIELD KEEPS ITS SLOT FOREVER";
	// docs/FIXED-FORM-ALGORITHM.md §5.3, §5.4). A deprecated field is still an
	// entry in the layout and still a slot, so the NEW reader reads the OLD
	// file's a/b/c exactly — the deprecated b included — on the identity lane,
	// and no counter moves. The append half lands the tail default.
	t.Run("cpp/E8", func(t *testing.T) {
		t.Parallel()
		cppT18RequireCompiler(t)

		older := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VOLD_field_deprecate.schema"))
		// the coalescer merges a,b,c int32 into one twelve-byte run at 0, so the
		// fields are declared ints at 0, 4 and 8
		vals := cppT18Body(t, older)
		binary.LittleEndian.PutUint32(vals[0:], 1) // a
		binary.LittleEndian.PutUint32(vals[4:], 2) // b, the deprecated slot
		binary.LittleEndian.PutUint32(vals[8:], 3) // c
		file := cppT18OldFile(t, "field_deprecate", vals)
		out, err := cppT18Probe(t, "field_deprecate", file, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "deprecate read: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( r.reason != newer_form ) { printf( "a clean deprecation read is not a refusal: reason=%d\n", (int) r.reason ); return 1; }
    if ( back[0].a != 1 || back[0].b != 2 || back[0].c != 3 ) { printf( "the deprecated field or a neighbour does not land exactly: a=%d b=%d c=%d\n", (int) back[0].a, (int) back[0].b, (int) back[0].c ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 ) { printf( "a deprecation moves a counter: u=%d km=%d w=%d c=%d d=%d ret=%d lost=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, r.retained, r.retain_lost ); return 1; }
`)
		if err != nil {
			t.Fatalf("cpp/E8 deprecate: %v\n%s", err, out)
		}

		appendOld := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VOLD_field_append.schema"))
		// x,y,z int32 coalesce into one twelve-byte run at 0
		avals := cppT18Body(t, appendOld)
		binary.LittleEndian.PutUint32(avals[0:], 10) // x
		binary.LittleEndian.PutUint32(avals[4:], 11) // y
		binary.LittleEndian.PutUint32(avals[8:], 12) // z
		afile := cppT18OldFile(t, "field_append", avals)
		aout, err := cppT18Probe(t, "field_append", afile, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "append read: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( r.reason != newer_form ) { printf( "a clean append read is not a refusal: reason=%d\n", (int) r.reason ); return 1; }
    if ( back[0].x != 10 || back[0].y != 11 || back[0].z != 12 ) { printf( "the old fields do not land exactly: x=%d y=%d z=%d\n", (int) back[0].x, (int) back[0].y, (int) back[0].z ); return 1; }
    if ( back[0].w != 77 ) { printf( "the appended field must land its declared default 77, not %d\n", (int) back[0].w ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 ) { printf( "an append moves a counter: u=%d km=%d w=%d c=%d d=%d ret=%d lost=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, r.retained, r.retain_lost ); return 1; }
`)
		if err != nil {
			t.Fatalf("cpp/E8 append: %v\n%s", err, aout)
		}
	})

	// cpp/R19 "a clean NEW-READS-OLD of an appended field, variant, arm, flag or
	// keyed slot moves no counter at all" (docs/FIXED-FORM-ALGORITHM.md §5.4,
	// docs/SPEC-TABLES.md §3.4). One clean read per kind, each from its
	// VOLD_/VNEW_ pair; the old values land exactly and all seven TableReport
	// counters are exactly 0. The reason is the clean-read value
	// TestFixedVersioningNewReadsOld uses: `r.reason != newer_form` is a refusal.
	t.Run("cpp/R19", func(t *testing.T) {
		t.Parallel()
		cppT18RequireCompiler(t)
		for _, kind := range cppT18R19Kinds {
			t.Run(kind, func(t *testing.T) {
				t.Parallel()
				row := cppT18Pair[kind]
				older := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema"))
				vals := cppT18Body(t, older)
				// the plan's first run is where the leading field (or the whole
				// leading record) begins; the value assertions below read it back
				base, _ := cppT18FirstRun(t, older)
				var valueC string
				switch kind {
				case "field":
					binary.LittleEndian.PutUint32(vals[base+0:], 10) // x
					binary.LittleEndian.PutUint32(vals[base+4:], 11) // y
					binary.LittleEndian.PutUint32(vals[base+8:], 12) // z
					valueC = `    if ( back[0].x != 10 || back[0].y != 11 || back[0].z != 12 ) { printf( "field_append: the old fields do not land exactly: x=%d y=%d z=%d\n", (int) back[0].x, (int) back[0].y, (int) back[0].z ); return 1; }
    if ( back[0].w != 77 ) { printf( "field_append: the appended field must land its declared default 77, not %d\n", (int) back[0].w ); return 1; }
`
				case "variant", "arm":
					// the enum/union is not coalesced, so the trailing scalar has
					// its own plan entry and lands a distinctive value
					cppT18Set(t, older, vals, "seq", 40)
					valueC = `    if ( back[0].seq != 40 ) { printf( "` + row + `: the trailing scalar did not land: seq=%d\n", (int) back[0].seq ); return 1; }
`
				case "flag":
					// caps and seq coalesce into one run at 0; Jump is bit 0, so a
					// one in the flags byte is the old writer's value
					vals[base] = 1
					valueC = `    if ( (int) back[0].caps != 1 ) { printf( "flags_append: the old flags did not land: caps=%d\n", (int) back[0].caps ); return 1; }
`
				case "keyed-slot":
					// the keyed array and the scalar coalesce into one run at 0;
					// slot 0's Qty.n is the first four bytes
					binary.LittleEndian.PutUint32(vals[base:], 9) // slots[Tier::Bronze].n
					valueC = `    if ( back[0].slots[Tier::Bronze].n != 9 ) { printf( "keyed_array_enum_append: the old slot did not land: n=%d\n", (int) back[0].slots[Tier::Bronze].n ); return 1; }
`
				}
				file := cppT18OldFile(t, row, vals)
				body := `
    if ( n != 1 || r.refused || r.malformed ) { printf( "` + row + ` clean append: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( r.reason != newer_form ) { printf( "` + row + ` clean append is not a refusal: reason=%d\n", (int) r.reason ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 ) { printf( "` + row + ` appended kind moves a counter: u=%d km=%d w=%d c=%d d=%d ret=%d lost=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, r.retained, r.retain_lost ); return 1; }
` + valueC
				out, err := cppT18Probe(t, row, file, body)
				if err != nil {
					t.Fatalf("cpp/R19 %s: %v\n%s", kind, err, out)
				}
			})
		}
	})
}
