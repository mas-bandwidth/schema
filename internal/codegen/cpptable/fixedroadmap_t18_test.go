package cpptable

// TestFixedRoadmapCppT18FieldEvolution closes the two leaves the overlay kept
// :unknown for this leg: cpp/E8 "Append and deprecate under the backward-read
// contract" and cpp/R19 "a clean NEW-READS-OLD of an appended field, variant,
// arm, flag or keyed slot moves no counter at all" (docs/roadmap.sexp's node
// field-evolution/cpp). The laws are docs/SPEC-TABLES.md §3.4 (the fixed form:
// a deprecated field keeps its slot forever; an appended field is a slot the
// reader fills from its declared default) and docs/FIXED-FORM-ALGORITHM.md §5.4
// (the load's counters: an append the reader knows is not an event, so every
// one of the seven stays zero).
//
// THE C LEG CLOSED THE SAME TWO LEAVES WITHOUT THE CORPUS
// (internal/codegen/ctable/fixedroadmap_t05_test.go): cT05OldFile builds the
// OLD file in GO from the same IR the emitter lays down, and the NEW reader
// reads it under an explicit lock. This card ports that approach: the OLD file
// here is a Go-built port (t18OldFile, below), so a cross-version probe reads
// an old writer's file without the C++ reference's byte oracle
// (build/fixedform-corpus) and without a corpus read of any kind.
//
// A SAME-HASH PAIR NEEDS THE OLDER SIDE HELD EXPLICITLY. `field_deprecate`
// moves no layout byte and no digest byte, so the two hashes are equal and the
// nil-lock sibling convention collapses to the identity entry alone; the
// generator is handed a lock (t18Lock) that names the older side instead.
// cppRunVersionProbe cannot express that (its signature is unchanged, and
// cppVersionRows with it), so its sibling cppRunVersionProbeLock, below, is the
// explicit-lock probe the deprecate half compiles and runs.
//
// THIS IS A TEST-AND-EVIDENCE CARD: no emitter and no runtime changes. The
// functional subtests cpp/E8 and cpp/R19 compile and run the generated C++
// through cppRunVersionProbe (fixedversioning_test.go lines 199-204), so they
// skip by name when the compiler is absent and SCHEMA_REQUIRE_CORPUS is unset,
// and fail by name when it is set. The unit tier TestFixedCppT18OldFiles runs
// in process only (loadUnit and the generator; no compiler, no clock, no
// socket, no network).

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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t18Rows is the six VOLD_/VNEW_ pairs this card covers: the deprecate pair
// (same hash) and the five append kinds the cpp/R19 title names — field,
// variant, arm, flag and keyed slot.
var t18Rows = []string{
	"field_append",
	"field_deprecate",
	"enum_append",
	"flags_append",
	"keyed_array_enum_append",
	"union_append",
}

// t18Compiles bounds concurrent C++ compilations this file starts. A probe
// spawns the compiler's own children, and the sandbox's process ceiling refuses
// a fork with EAGAIN ("posix_spawn: Resource temporarily unavailable") when the
// deprecate probe builds beside the five R19 probes at once. Two at a time
// keeps the functional subtests deterministic; t.Parallel() still orders and
// reports them.
var t18Compiles = make(chan struct{}, 2)

// t18RunVersionProbe is cppRunVersionProbe under the compile bound.
func t18RunVersionProbe(t *testing.T, row, file, body, forge string) ([]byte, error) {
	t.Helper()
	t18Compiles <- struct{}{}
	defer func() { <-t18Compiles }()
	return cppRunVersionProbe(t, row, file, body, forge)
}

// t18RequireCompiler is the functional subtests' gate: they compile and run C++,
// so with no compiler on PATH they SKIP BY NAME — and under SCHEMA_REQUIRE_CORPUS
// that skip is a FAILURE, because a suite that skips itself there would report
// green over a row that never ran.
func t18RequireCompiler(t *testing.T) {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no C++ compiler (%s) is on PATH: %v", cxx, err)
		}
		t.Skipf("no C++ compiler (%s) is on PATH: %v", cxx, err)
	}
}

// t18OldBytes builds a fixed-form FILE in the OLD writer's layout, in Go, from
// the VOLD_ unit's own IR — the port of ctable's cT05OldFile. The bytes are the
// header (the form byte 3, seven reserved zeros, the LAYOUT HASH at 8), the
// u32 layout length, the layout bytes, then one record: its eight-byte
// per-record hash and the body at every field's bound. `values` is the record
// body, every field at its declared extent, in declared order.
func t18OldBytes(t *testing.T, olderPath string, values []byte) []byte {
	t.Helper()
	u := loadUnit(t, olderPath)
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

// t18OldFile is t18OldBytes written to a temp file the C++ probe can fopen.
func t18OldFile(t *testing.T, olderPath string, values []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "old.bin")
	if err := os.WriteFile(p, t18OldBytes(t, olderPath, values), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// t18Lock is the older side held EXPLICITLY: a lockfile.Unit whose single
// fixed table carries one lineage entry naming the VOLD_ side's wire hash,
// layout bytes, definitions digest and record body. A same-hash pair has no
// distinct VOLD_ peer for the nil-lock convention to find, so the reader is
// compiled with this lock instead.
func t18Lock(t *testing.T, olderPath string) *lockfile.Unit {
	t.Helper()
	u := loadUnit(t, olderPath)
	st := cppFixedRoot(t, u)
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	hash := ir.TableFixedLayoutHash(layout, st)
	return &lockfile.Unit{
		Version: lockfile.Version,
		Package: u.Package,
		Tables: []lockfile.Table{{
			Decl: lockfile.DeclFixedTable,
			Name: st.WireName(),
			Lineage: []lockfile.LineageEntry{{
				Wire:   hash,
				Layout: layout,
				Digest: ir.TableFixedDefinitionsDigest(st),
				Record: ir.TableFixedTypeBytes(st),
			}},
		}},
	}
}

// t18TableHeader generates the unit's tables with the given lock (nil is the
// sibling convention) and returns the <Base>Table.h text.
func t18TableHeader(t *testing.T, u *ir.Unit, lock *lockfile.Unit) string {
	t.Helper()
	files, err := GenerateLineage(u, lock)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") && !strings.Contains(name, "/") {
			return string(data)
		}
	}
	t.Fatalf("no Table.h was emitted for %s", u.Package)
	return ""
}

// t18Emitted reads the generator's own hash and layout bytes for the unit's
// root out of the emitted header: `constexpr uint64_t <Root>FixedHash = 0x...ull`
// and `constexpr uint8_t <Root>FixedLayout[] = { ... }`. The unit tier holds
// the Go-built OLD file to THESE, not to a second walk of the same IR the test
// already made.
func t18Emitted(t *testing.T, u *ir.Unit, root string) (uint64, []byte) {
	t.Helper()
	header := t18TableHeader(t, u, nil)
	hashTok := root + "FixedHash = 0x"
	i := strings.Index(header, hashTok)
	if i < 0 {
		t.Fatalf("the emitted header carries no %q", hashTok)
	}
	rest := header[i+len(hashTok):]
	j := strings.Index(rest, "ull")
	if j < 0 {
		t.Fatalf("the emitted %q has no ull suffix", hashTok)
	}
	h, err := strconv.ParseUint(rest[:j], 16, 64)
	if err != nil {
		t.Fatalf("the emitted hash %q is unreadable: %v", rest[:j], err)
	}
	layTok := root + "FixedLayout[] = {"
	i = strings.Index(header, layTok)
	if i < 0 {
		t.Fatalf("the emitted header carries no %q", layTok)
	}
	rest = header[i+len(layTok):]
	j = strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("the emitted %q has no end", layTok)
	}
	var out []byte
	for _, f := range strings.Split(rest[:j], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("the emitted layout byte %q is unreadable: %v", f, err)
		}
		out = append(out, byte(v))
	}
	return h, out
}

// TestFixedCppT18OldFiles is the UNIT TIER: in process only (loadUnit and the
// generator; no compiler, no clock, no socket, no network). For each of the six
// VOLD_ units it holds the Go-built OLD file's header to the hash and the
// layout bytes the generator emits, checks the record carries the same hash, and
// checks the VNEW reader generated with the explicit lock names the VOLD hash in
// its lineage. The negative control is reversible and in memory: the OLD file
// built from the VNEW side of a hash-changing pair (field_append) must not match
// the VOLD hash, so the assertion is shown able to fail.
func TestFixedCppT18OldFiles(t *testing.T) {
	t.Parallel()
	for _, row := range t18Rows {
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			older := filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema")
			newer := filepath.Join("..", "..", "..", "test", "tables", "VNEW_"+row+".schema")
			uOld := loadUnit(t, older)
			stOld := cppFixedRoot(t, uOld)
			data := t18OldBytes(t, older, make([]byte, ir.TableFixedTypeBytes(stOld)))

			emHash, emLayout := t18Emitted(t, uOld, stOld.Name)
			gotHash := binary.LittleEndian.Uint64(data[8:16])
			if gotHash != emHash {
				t.Fatalf("the Go-built OLD file's header hash is 0x%016x, the generator emits 0x%016x for %s", gotHash, emHash, stOld.Name)
			}
			at := 20 + int(binary.LittleEndian.Uint32(data[16:20]))
			gotLayout := data[20:at]
			if !bytes.Equal(gotLayout, emLayout) {
				t.Fatalf("the Go-built OLD file's layout is %d bytes, the generator emits %d for %s", len(gotLayout), len(emLayout), stOld.Name)
			}
			gotRec := binary.LittleEndian.Uint64(data[at : at+8])
			if gotRec != gotHash {
				t.Fatalf("the record hash 0x%016x is not the header hash 0x%016x", gotRec, gotHash)
			}

			uNew := loadUnit(t, newer)
			header := t18TableHeader(t, uNew, t18Lock(t, older))
			want := fmt.Sprintf("0x%016xull", emHash)
			if !strings.Contains(header, want) {
				t.Fatalf("the VNEW reader built from the explicit lock does not name the VOLD hash %s in its lineage", want)
			}
		})
	}

	// THE NEGATIVE CONTROL, reversible and in memory: the same builder run on
	// the VNEW_ side of a hash-changing pair produces that side's own layout hash,
	// which is a different number from the VOLD_ hash. If the two matched, the
	// check above could not tell an old file from a new one.
	t.Run("hash-changed", func(t *testing.T) {
		t.Parallel()
		older := filepath.Join("..", "..", "..", "test", "tables", "VOLD_field_append.schema")
		newer := filepath.Join("..", "..", "..", "test", "tables", "VNEW_field_append.schema")
		uOld := loadUnit(t, older)
		stOld := cppFixedRoot(t, uOld)
		voldHash := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(stOld)), stOld)

		uNew := loadUnit(t, newer)
		stNew := cppFixedRoot(t, uNew)
		data := t18OldBytes(t, newer, make([]byte, ir.TableFixedTypeBytes(stNew)))
		gotHash := binary.LittleEndian.Uint64(data[8:16])
		if gotHash == voldHash {
			t.Fatalf("the negative control is inert: a hash-changing pair produced the VOLD hash 0x%016x", voldHash)
		}
	})
}

// t18CleanRead is the shared clean-read body: the clean-read value is
// `newer_form` (never 0 — see fixedversioning_test.go), and all SEVEN
// TableReport counters — unknown, kind_mismatch, widened, clamped, duplicate,
// retained, retain_lost — are exactly 0 (docs/FIXED-FORM-ALGORITHM.md §5.4).
const t18CleanRead = `
    if ( n != 1 ) { printf( "the newer reader refused the older writer's file: n=%lld reason=%d\n", (long long) n, (int) r.reason ); return 1; }
    if ( r.reason != newer_form || r.malformed || r.refused ) { printf( "a clean NEW-READS-OLD is not a refusal: reason=%d malformed=%d\n", (int) r.reason, (int) r.malformed ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 )
        { printf( "counters moved on a clean backward read: u=%d km=%d w=%d c=%d d=%d ret=%d lost=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, r.retained, r.retain_lost ); return 1; }
`

// TestFixedRoadmapCppT18FieldEvolution is the two functional subtests. cpp/E8
// reads the deprecated pair under the explicit lock (same hash) plus the append
// half under the sibling convention; cpp/R19 reads one Go-built OLD file per
// appended kind. Both compile and RUN the generated C++ through the leg's own
// probe, so an absent compiler SKIPS them by name and SCHEMA_REQUIRE_CORPUS
// turns that skip into a failure.
func TestFixedRoadmapCppT18FieldEvolution(t *testing.T) {
	t.Parallel()

	// cpp/E8 "Append and deprecate under the backward-read contract"
	// (docs/SPEC-TABLES.md §3.4 "A DEPRECATED FIELD KEEPS ITS SLOT FOREVER"; the
	// read's counters are docs/FIXED-FORM-ALGORITHM.md §5.4). A deprecated field
	// is still an entry in the layout and still a slot, so the NEW reader reads
	// the OLD file's a/b/c exactly — the deprecated b included — and moves no
	// counter; the append half lands the tail field's declared default.
	t.Run("cpp/E8", func(t *testing.T) {
		t.Parallel()
		t18RequireCompiler(t)
		deprecate := filepath.Join("..", "..", "..", "test", "tables", "VOLD_field_deprecate.schema")
		dvals := make([]byte, 12)
		binary.LittleEndian.PutUint32(dvals[0:], 1) // a
		binary.LittleEndian.PutUint32(dvals[4:], 2) // b, the deprecated slot
		binary.LittleEndian.PutUint32(dvals[8:], 3) // c
		dout, derr := cppRunVersionProbeLock(t, "field_deprecate", deprecate, t18OldFile(t, deprecate, dvals), t18CleanRead+`
    if ( back[0].a != 1 || back[0].b != 2 || back[0].c != 3 )
        { printf( "the deprecated field or a neighbour does not land exactly: a=%d b=%d c=%d\n", back[0].a, back[0].b, back[0].c ); return 1; }
`, "")
		if derr != nil {
			t.Fatalf("cpp/E8 field_deprecate: %v\n%s", derr, dout)
		}

		appendOld := filepath.Join("..", "..", "..", "test", "tables", "VOLD_field_append.schema")
		avals := make([]byte, 12)
		binary.LittleEndian.PutUint32(avals[0:], 10) // x
		binary.LittleEndian.PutUint32(avals[4:], 11) // y
		binary.LittleEndian.PutUint32(avals[8:], 12) // z
		aout, aerr := t18RunVersionProbe(t, "field_append", t18OldFile(t, appendOld, avals), t18CleanRead+`
    if ( back[0].x != 10 || back[0].y != 11 || back[0].z != 12 )
        { printf( "the old writer's values did not land: %d %d %d\n", back[0].x, back[0].y, back[0].z ); return 1; }
    if ( back[0].w != 77 )
        { printf( "the appended field is not its declared default 77: %d\n", back[0].w ); return 1; }
`, "")
		if aerr != nil {
			t.Fatalf("cpp/E8 field_append: %v\n%s", aerr, aout)
		}
	})

	// cpp/R19 "a clean NEW-READS-OLD of an appended field, variant, arm, flag or
	// keyed slot moves no counter at all" (docs/FIXED-FORM-ALGORITHM.md §5.4;
	// SPEC-TABLES §3.4). One Go-built OLD file per kind, built from the VOLD_
	// unit's own IR; the old values land exactly and all seven counters stay
	// zero. The appended field and the appended keyed slot also land the
	// DECLARED default the newer side prefills.
	t.Run("cpp/R19", func(t *testing.T) {
		t.Parallel()
		t18RequireCompiler(t)

		t.Run("field", func(t *testing.T) {
			t.Parallel()
			old := filepath.Join("..", "..", "..", "test", "tables", "VOLD_field_append.schema")
			vals := make([]byte, 12)
			binary.LittleEndian.PutUint32(vals[0:], 10)
			binary.LittleEndian.PutUint32(vals[4:], 11)
			binary.LittleEndian.PutUint32(vals[8:], 12)
			out, err := t18RunVersionProbe(t, "field_append", t18OldFile(t, old, vals), t18CleanRead+`
    if ( back[0].x != 10 || back[0].y != 11 || back[0].z != 12 )
        { printf( "the old fields did not land: %d %d %d\n", back[0].x, back[0].y, back[0].z ); return 1; }
    if ( back[0].w != 77 )
        { printf( "the appended field is not its declared default 77: %d\n", back[0].w ); return 1; }
`, "")
			if err != nil {
				t.Fatalf("cpp/R19 field: %v\n%s", err, out)
			}
		})

		t.Run("variant", func(t *testing.T) {
			t.Parallel()
			old := filepath.Join("..", "..", "..", "test", "tables", "VOLD_enum_append.schema")
			vals := make([]byte, 5)
			vals[0] = 3 // Gold, the last variant the OLD side declares
			binary.LittleEndian.PutUint32(vals[1:], 5)
			out, err := t18RunVersionProbe(t, "enum_append", t18OldFile(t, old, vals), t18CleanRead+`
    if ( back[0].tier != Tier::Gold )
        { printf( "the old variant did not land: %d\n", (int) back[0].tier ); return 1; }
    if ( back[0].seq != 5 )
        { printf( "the scalar after the variant did not land: %d\n", back[0].seq ); return 1; }
`, "")
			if err != nil {
				t.Fatalf("cpp/R19 variant: %v\n%s", err, out)
			}
		})

		t.Run("arm", func(t *testing.T) {
			t.Parallel()
			old := filepath.Join("..", "..", "..", "test", "tables", "VOLD_union_append.schema")
			vals := make([]byte, 9)
			vals[0] = 1 // PickType::Alpha
			binary.LittleEndian.PutUint32(vals[1:], 9)
			binary.LittleEndian.PutUint32(vals[5:], 5)
			out, err := t18RunVersionProbe(t, "union_append", t18OldFile(t, old, vals), t18CleanRead+`
    if ( back[0].pick.type != PickType::Alpha )
        { printf( "the old arm did not land: %d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].pick.alpha.m != 9 )
        { printf( "the old arm payload did not land: %d\n", back[0].pick.alpha.m ); return 1; }
    if ( back[0].seq != 5 )
        { printf( "the scalar after the arm did not land: %d\n", back[0].seq ); return 1; }
`, "")
			if err != nil {
				t.Fatalf("cpp/R19 arm: %v\n%s", err, out)
			}
		})

		t.Run("flag", func(t *testing.T) {
			t.Parallel()
			old := filepath.Join("..", "..", "..", "test", "tables", "VOLD_flags_append.schema")
			vals := make([]byte, 12)
			binary.LittleEndian.PutUint64(vals[0:], 3) // Jump | Crouch
			binary.LittleEndian.PutUint32(vals[8:], 5)
			out, err := t18RunVersionProbe(t, "flags_append", t18OldFile(t, old, vals), t18CleanRead+`
    if ( back[0].caps != ( Caps_Jump | Caps_Crouch ) )
        { printf( "the old flags did not land: %llu\n", (unsigned long long) back[0].caps ); return 1; }
    if ( back[0].seq != 5 )
        { printf( "the scalar after the flags did not land: %d\n", back[0].seq ); return 1; }
`, "")
			if err != nil {
				t.Fatalf("cpp/R19 flag: %v\n%s", err, out)
			}
		})

		t.Run("keyed-slot", func(t *testing.T) {
			t.Parallel()
			old := filepath.Join("..", "..", "..", "test", "tables", "VOLD_keyed_array_enum_append.schema")
			vals := make([]byte, 16)
			binary.LittleEndian.PutUint32(vals[0:], 4)  // [Bronze].n
			binary.LittleEndian.PutUint32(vals[4:], 5)  // [Silver].n
			binary.LittleEndian.PutUint32(vals[8:], 6)  // [Gold].n
			binary.LittleEndian.PutUint32(vals[12:], 5) // seq
			out, err := t18RunVersionProbe(t, "keyed_array_enum_append", t18OldFile(t, old, vals), t18CleanRead+`
    if ( back[0].slots[Tier::Bronze].n != 4 || back[0].slots[Tier::Silver].n != 5 || back[0].slots[Tier::Gold].n != 6 )
        { printf( "the old keyed slot values did not land: %d %d %d\n", back[0].slots[Tier::Bronze].n, back[0].slots[Tier::Silver].n, back[0].slots[Tier::Gold].n ); return 1; }
    if ( back[0].slots[Tier::Platinum].n != 7 )
        { printf( "the appended key's slot is not Qty's declared default 7: %d\n", back[0].slots[Tier::Platinum].n ); return 1; }
    if ( back[0].seq != 5 )
        { printf( "the scalar after the keyed array did not land: %d\n", back[0].seq ); return 1; }
`, "")
			if err != nil {
				t.Fatalf("cpp/R19 keyed-slot: %v\n%s", err, out)
			}
		})
	})
}

// cppRunVersionProbeLock is cppRunVersionProbe's sibling for a SAME-HASH pair:
// it generates the READER's unit and hands GenerateLineage the explicit lock
// naming the older side (t18Lock), so a reader with no distinct VOLD_ peer —
// the deprecate row — still carries that peer's hash in its lineage. Its
// interim guard is the lock's own: the generated header MUST name the old hash.
// cppRunVersionProbe's signature is untouched; this is a second entry point.
func cppRunVersionProbeLock(t *testing.T, row, olderPath, file, body, forge string) ([]byte, error) {
	t.Helper()
	t18Compiles <- struct{}{}
	defer func() { <-t18Compiles }()
	dir := t.TempDir()
	newData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VNEW_"+row+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(dir, "VNEW_"+row+".schema")
	if err := os.WriteFile(newPath, newData, 0o644); err != nil {
		t.Fatal(err)
	}

	u := loadUnit(t, newPath)
	lock := t18Lock(t, olderPath)

	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lock)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	// THE LOCK MUST HAVE FIRED before the probe is trusted: the reader's header
	// has to name the older side's hash. Identity-only means the lock missed and
	// the row cannot be probed.
	oldU := loadUnit(t, olderPath)
	oldSt := cppFixedRoot(t, oldU)
	oldHash := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(oldSt)), oldSt)
	want := fmt.Sprintf("0x%016xull", oldHash)
	headerText := ""
	for name, data := range tables {
		if strings.HasSuffix(name, "Table.h") && !strings.Contains(name, "/") {
			headerText = string(data)
		}
	}
	if headerText == "" {
		t.Fatal("no Table.h was generated")
	}
	if !strings.Contains(headerText, want) {
		t.Fatalf("the explicit lock did not fire: the reader's lineage does not name %s:\n%s", want, headerText)
	}

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

	root := cppFixedRootName(t, u)
	pkg := u.Package

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
    memset( &r, 0, sizeof( r ) );
    n = %sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
%s
    return 0;
}
`, header, pkg, file, root, forge, root, root, body)

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

// ---- cpp/R16, the exact counters (main's 9d2cb5bc6; the t18 number was taken on both sides) ----

// fixedroadmap_t18 — cpp/R16 "the counters exactly" (docs/FIXED-FORM-ALGORITHM.md
// §5.4, docs/roadmap.sexp node `fixed-tables`). One fixture per row of the §5.4
// table, each WRITTEN and READ by this leg's own C++ toolchain — the OLD
// writer's `...FixedSave` writes the records, the NEW reader's `...FixedLoad`
// reads them through the lineage, and the report's exact counters are asserted.
// This is the same generate-compile-run unit cppRunVersionProbe uses, but the
// bytes are written here (never read from build/fixedform-corpus), so the row
// runs wherever g++ does.

// cppT18Pair is the OLD and NEW generation of one row, generated side by side.
type cppT18Pair struct {
	outDir               string
	oldHeader, newHeader string
	oldPkg, newPkg       string
	root                 string
}

// cppT18Generate stages VOLD_/VNEW_<row>.schema in one temp dir, generates the
// OLD unit (the writer) and the NEW unit (the reader, whose lineage is the OLD
// peer via the nil-lock sibling convention), and answers the two headers and
// their namespaces.
func cppT18Generate(t *testing.T, row string) cppT18Pair {
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

	oldUnit := loadUnit(t, oldPath)
	newUnit := loadUnit(t, newPath)

	oldFiles, err := cpp.Generate(oldUnit)
	if err != nil {
		t.Fatal(err)
	}
	oldTables, err := GenerateLineage(oldUnit, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(oldFiles, oldTables)

	newFiles, err := cpp.Generate(newUnit)
	if err != nil {
		t.Fatal(err)
	}
	newTables, err := GenerateLineage(newUnit, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(newFiles, newTables)

	outDir := t.TempDir()
	var pair cppT18Pair
	pair.outDir = outDir
	for name, data := range oldFiles {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			pair.oldHeader = name
		}
	}
	for name, data := range newFiles {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			pair.newHeader = name
		}
	}
	if pair.oldHeader == "" || pair.newHeader == "" || pair.oldHeader == pair.newHeader {
		t.Fatalf("t18: the two generations did not yield two distinct Table.h headers (old=%q new=%q)", pair.oldHeader, pair.newHeader)
	}
	pair.oldPkg = oldUnit.Package
	pair.newPkg = newUnit.Package
	pair.root = cppFixedRootName(t, newUnit)
	return pair
}

// cppT18Run compiles and runs one probe against the pair. The probe includes
// both headers and opens the OLD namespace, so the OLD writer, OLD reader and
// OLD report/plan types are unqualified, while the NEW reader's types are
// `%[2]s::`-qualified.
func cppT18Run(t *testing.T, pair cppT18Pair, body string) string {
	t.Helper()
	dir := t.TempDir()
	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include "%s"
#include "%s"

using namespace %s;

int main( void )
{
%s
    return 0;
}
`, pair.oldHeader, pair.newHeader, pair.oldPkg, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
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
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + pair.outDir, "-I" + serialize, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("t18: the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("t18: the probe failed: %v\n%s", err, out)
	}
	return string(out)
}

// t18Read is the NEW reader's compiled-plan read, qualified into the NEW
// namespace: `%[1]s` is the shared root name, `%[2]s` the NEW package.
const t18Read = `
        %[2]s::%[1]s back[%[3]d];
        static %[2]s::TableFixedEntry plan[4096];
        %[2]s::TableReport r; memset( &r, 0, sizeof( r ) );
        const int64_t n = %[2]s::%[1]sFixedLoad( back, %[3]d, buffer, written, plan, 4096, NULL, &r );
`

// TestFixedRoadmapCppT18ExactCounters is cpp/R16: the §5.4 counters, exactly.
func TestFixedRoadmapCppT18ExactCounters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c++ compiler (the leg's own toolchain)")
	t.Run("cpp/R16", func(t *testing.T) {
		t.Parallel()

		// widened: int16 -> int32, one widen entry per record. N=4 -> widened=4.
		pair := cppT18Generate(t, "int_widen")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want[4];
        for ( int k = 0; k < 4; ++k ) { want[k].lead = 1u; want[k].v = (int16_t) ( k + 1 ); want[k].trail = 2u; }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "widen: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 4 ) { printf( "widen: n=%%lld\n", (long long) n ); return 1; }
        if ( r.widened != 4 ) { printf( "widen: widened=%%d want 4\n", r.widened ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 ) { printf( "widen: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 4))

		// folded widened element run: [..4]int16 -> [..4]int32, one widen entry per
		// element, never folded: min(their_n, my_n) = 4 per record.
		pair = cppT18Generate(t, "array_elem_widen")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.lead = 1u; want.trail = 2u; want.vals_count = 4;
        for ( int k = 0; k < 4; ++k ) { want.vals[k] = (int16_t) ( 100 + k ); }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "elem-widen: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 1 ) { printf( "elem-widen: n=%%lld\n", (long long) n ); return 1; }
        if ( r.widened != 4 ) { printf( "elem-widen: widened=%%d want 4\n", r.widened ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 ) { printf( "elem-widen: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 1))

		// clamped count: [..4]int32 -> [..8]int32, the count forged to 7 (past the
		// writer's 4). One clamp per record; N=3 -> clamped=3.
		pair = cppT18Generate(t, "array_bounded_grow")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want[3];
        for ( int k = 0; k < 3; ++k )
        {
            want[k].lead = 1u; want[k].trail = 2u; want[k].vals_count = 4;
            for ( int j = 0; j < 4; ++j ) { want[k].vals[j] = 1000 + j; }
        }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( want, 3, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "count: save failed\n" ); return 1; }
        {
            static const uint8_t needle[8] = { 0x01, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00 };
            int found = 0;
            for ( int64_t i = 0; i + 8 <= written; ++i )
                if ( memcmp( buffer + i, needle, 8 ) == 0 ) { TableFixedPut32( buffer + i + 4, 7 ); found++; }
            if ( found != 3 ) { printf( "count: locator matched %%d times, want 3\n", found ); return 1; }
        }
`+t18Read+`
        if ( n != 3 ) { printf( "count: n=%%lld\n", (long long) n ); return 1; }
        if ( r.clamped != 3 ) { printf( "count: clamped=%%d want 3\n", r.clamped ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 ) { printf( "count: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 3))

		// forged ordinal past the writer's variant count: tier=Gold(3) forged to 4,
		// one past the writer's three variants. clamped == 1 on the COMPILED plan
		// (NEW reader) exactly as on the IDENTITY plan (OLD reader), never 2.
		pair = cppT18Generate(t, "enum_append")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.tier = Tier::Gold; want.seq = 9;
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "ordinal: save failed\n" ); return 1; }
        {
            static const uint8_t needle[5] = { 0x03, 0x09, 0x00, 0x00, 0x00 };
            int found = 0;
            for ( int64_t i = 0; i + 5 <= written; ++i )
                if ( memcmp( buffer + i, needle, 5 ) == 0 ) { buffer[i] = 4; found++; }
            if ( found != 1 ) { printf( "ordinal: locator matched %%d times, want 1\n", found ); return 1; }
        }
`+t18Read+`
        if ( n != 1 ) { printf( "ordinal compiled: n=%%lld\n", (long long) n ); return 1; }
        if ( back[0].tier != %[2]s::Tier::None ) { printf( "ordinal compiled: lands %%d, not None\n", (int) back[0].tier ); return 1; }
        if ( r.clamped != 1 ) { printf( "ordinal compiled: clamped=%%d want 1\n", r.clamped ); return 1; }
        {
            %[1]s back0;
            static TableFixedEntry plan[4096];
            TableReport rr; memset( &rr, 0, sizeof( rr ) );
            const int64_t m = %[1]sFixedLoad( &back0, 1, buffer, written, plan, 4096, NULL, &rr );
            if ( m != 1 ) { printf( "ordinal identity: n=%%lld\n", (long long) m ); return 1; }
            if ( back0.tier != Tier::None ) { printf( "ordinal identity: lands %%d, not None\n", (int) back0.tier ); return 1; }
            if ( rr.clamped != 1 ) { printf( "ordinal identity: clamped=%%d want 1\n", rr.clamped ); return 1; }
        }
    }
`, pair.root, pair.newPkg, 1))

		// clean appended field: every counter 0, the reader's tail is its default 77.
		pair = cppT18Generate(t, "field_append")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.x = 11; want.y = 22; want.z = 33;
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "append: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 1 ) { printf( "append: n=%%lld\n", (long long) n ); return 1; }
        if ( back[0].x != 11 || back[0].y != 22 || back[0].z != 33 || back[0].w != 77 ) { printf( "append: values did not land\n" ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 ) { printf( "append: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 1))

		// unknown: Item.drop removed, read across the four-element array of tables.
		// Once per field per peer, never per record nor per element: unknown == 1.
		pair = cppT18Generate(t, "unknown_census")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.lead = 1u; want.trail = 2u;
        for ( int k = 0; k < 4; ++k ) { want.items[k].a = 10 + k; want.items[k].drop = 50 + k; }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "unknown: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 1 ) { printf( "unknown: n=%%lld\n", (long long) n ); return 1; }
        if ( r.unknown != 1 ) { printf( "unknown: unknown=%%d want 1\n", r.unknown ); return 1; }
        if ( r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 ) { printf( "unknown: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 1))
	})
}
