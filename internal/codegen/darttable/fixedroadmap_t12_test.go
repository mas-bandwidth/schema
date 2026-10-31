package darttable

// THE DART LEG'S LIVE-EXTENTS, FIXED-IMAGE WRITES AND NESTED-GUARD ROWS
// (docs/roadmap.sexp node `fixed-tables`, work-sets `live-extents/dart`,
// `writing/dart` and `union-guards/dart`). Each subtest is one roadmap task id
// and holds the page sentence its row's :title names: docs/FIXED-FORM-ALGORITHM.md
// §3.1 (the write, template zeros, DEBUG-only checks, unspecified read slack),
// §4.1 (the plan's arg/meta lanes and the bytes(N) row), §4.3 (the prefill)
// and §4.6 (the bounds pass walks live extents only). dart/R27 round-trips
// every record of the C++ reference's corpus and dart/P3 drives the hostile
// byte-flip sweep over the same corpus, both through build/fixedform-corpus.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t12Probe is the probe template the write/extents subtests drive: it saves one
// or more values with the leg's own writer, optionally reaches into the written
// bytes, and loads them back, failing by name on any counter a clean read moves.
const t12Probe = `import 'dart:io';
import 'dart:typed_data';

import './ProbeFixed.dart' as t;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

void main() {
  final values = <t.@TYPE@>[for (var i = 0; i < @RECORDS@; i++) t.@TYPE@()];
  for (final v in values) {
    t.init@TYPE@(v);
  }
  final bytes = Uint8List(t.@LOWER@FixedMeasure(values.length));
@BODY@
  if (exitCode == 0) {
    stdout.writeln('ok');
  }
}
`

// t12Func slices one function body out of generated Dart source by its
// declaration line, for the source-inspection subtests (dart/C14, dart/W5).
func t12Func(src, decl string) string {
	i := strings.Index(src, decl)
	if i < 0 {
		return ""
	}
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i : j+1]
			}
		}
	}
	return src[i:]
}

// t12Capability generates one inline schema with this leg's own emitter, writes
// the generated libraries beside a GENERATED probe, runs the probe with the
// leg's own toolchain, and returns its combined output. A red probe is a
// finding: the caller's subtest fails and the card names it.
func t12Capability(t *testing.T, src, body string, records int) string {
	t.Helper()
	u := unitFrom(t, src)
	st := versionRoot(t, u)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate fixed: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lower := lowerFirst(st.Name)
	probe := strings.NewReplacer(
		"@TYPE@", st.Name,
		"@LOWER@", lower,
		"@RECORDS@", fmt.Sprintf("%d", records),
		"@BODY@", body,
	).Replace(t12Probe)
	if err := os.WriteFile(filepath.Join(dir, "t12_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "t12_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the t12 probe did not run green: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("the t12 probe printed no ok: %s", out)
	}
	return string(out)
}

func TestFixedRoadmapDartT12ExtentsWritesGuards(t *testing.T) {
	t.Parallel()

	// dart/C14 "bounds pass walks live only" — docs/FIXED-FORM-ALGORITHM.md
	// §4.6: "It walks only what a read can have written — a counted array's
	// LIVE elements and never its slack". The emitted decode clamps the counted
	// array over its own live count word, never the declared bound.
	t.Run("dart/C14", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe

fixed table Root
{
    marks [..4]int32 | min = 0, max = 10
}
`)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate fixed: %v", err)
		}
		src := string(files["ProbeFixed.dart"])
		decode := t12Func(src, "void rootFixedDecode(")
		if decode == "" {
			t.Fatal("C14: rootFixedDecode is not emitted")
		}
		if !strings.Contains(decode, "i < value.marksCount") {
			t.Error("C14: the bounds pass must walk the counted array's LIVE count (§4.6)")
		}
		if strings.Contains(decode, "i < 4") {
			t.Error("C14: the bounds pass must not walk the declared bound, its slack (§4.6)")
		}
		if !strings.Contains(decode, "report.clamped++") {
			t.Error("C14: a live out-of-range element must clamp and count (§4.6)")
		}
	})

	// dart/W4 "read slack unspecified" — §3.1: "Slack is UNSPECIFIED on read
	// and not a refusal: a reader validates the USED UNITS only, and non-zero
	// slack is not `malformed`, not a refusal, and moves no counter." Poison
	// the unused tail of a text(8) field past its used length; the read still
	// succeeds cleanly.
	t.Run("dart/W4", func(t *testing.T) {
		t.Parallel()
		t12Capability(t, `package probe

fixed table Root
{
    a int32
    t string(8)
}
`, `
  values[0].a = 7;
  values[0].tLength = 2;
  values[0].t[0] = 0x41;
  values[0].t[1] = 0x42;
  final wrote = t.rootFixedSave(values, 1, bytes);
  check(wrote == bytes.length, 'the writer wrote $wrote of ${bytes.length} bytes');
  final body = t.rootFixedHeaderBytes + 8;
  for (var i = body + 10; i < body + 16; i++) {
    bytes[i] = 0xA5;
  }
  final plan = t.rootFixedNewPlan();
  final rep = t.TableFixedReport();
  final back = [t.Root()];
  t.initRoot(back[0]);
  final n = t.rootFixedLoad(back, 1, bytes, bytes.length, plan, rep);
  check(n == 1, 'the dirty-slack file must read: n=$n ${rep.refused}');
  check(!rep.malformed && rep.refused == 0 && rep.clamped == 0,
      'non-zero slack is not malformed, not a refusal, and moves no counter: '
      '${rep.malformed} ${rep.refused} ${rep.clamped}');
  check(back[0].a == 7 && back[0].tLength == 2 && back[0].t[0] == 0x41 &&
      back[0].t[1] == 0x42,
      'the used units landed: a=${back[0].a} len=${back[0].tLength}');
`, 1)
	})

	// dart/W9 "prefill unwritten ranges only" — §4.3: "Prefill the bytes the
	// plan does not write … only those take the declared defaults before the
	// loop." Reading an OLDER writer into a NEWER reader: the appended field is
	// a range the new plan does not write from the old file, so it takes its
	// declared default even when the destination was poisoned with 0x5A.
	t.Run("dart/W9", func(t *testing.T) {
		t.Parallel()
		corpus := fixedCorpus(t)
		body := `  final data = File(FILE).readAsBytesSync();
  plan.image.fillRange(0, plan.image.length, 0x5A);
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n >= 1, 'the newer reader refused the older writer: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a clean backward read: ${why(report)}');
  check(values[0].w == 77,
      'the appended field is the prefill declared default, not caller storage: '
      '${values[0].w}');
  check(values[0].x == 11 && values[0].y == 22 && values[0].z == 33,
      'the old writer values did not land: ${values[0].x} ${values[0].y} ${values[0].z}');
`
		out, err := runVersionProbe(t, dartBinary(t), "VNEW_field_append",
			[]string{"VOLD_field_append"}, 0,
			filepath.Join(corpus, "old_field_append.bin"), body)
		if err != nil {
			t.Fatalf("W9: %v\n%s", err, out)
		}
	})

	// dart/W1 "write slack is template zeros" — §3.1: "the hash, then zero
	// everywhere a value lands — which zero-fills every byte of declared slack
	// without the writer touching it." Writing a text(8) field with a used
	// length of 2 leaves the six unused payload bytes zero.
	t.Run("dart/W1", func(t *testing.T) {
		t.Parallel()
		t12Capability(t, `package probe

fixed table Root
{
    a int32
    t string(8)
}
`, `
  values[0].a = 7;
  values[0].tLength = 2;
  values[0].t[0] = 0x41;
  values[0].t[1] = 0x42;
  final wrote = t.rootFixedSave(values, 1, bytes);
  check(wrote == bytes.length, 'the writer wrote $wrote of ${bytes.length} bytes');
  final body = t.rootFixedHeaderBytes + 8;
  for (var i = body + 10; i < body + 16; i++) {
    check(bytes[i] == 0, 'slack byte $i is not template zero: ${bytes[i]}');
  }
`, 1)
	})

	// dart/W3 "zero behind a narrower arm" — §3.1: "A union writes the tag and
	// the taken arm only, zero behind a narrower one." Writing the int32 arm of
	// a union whose widest arm is int64 leaves the four bytes past the int32
	// zero.
	t.Run("dart/W3", func(t *testing.T) {
		t.Parallel()
		t12Capability(t, `package probe

union Pick
{
    a int32
    b int64
}

fixed table Root
{
    lead int32
    pick Pick
    trail int32
}
`, `
  values[0].lead = 1;
  values[0].trail = 2;
  values[0].pick.type = 1;
  values[0].pick.a = 0x11223344;
  final wrote = t.rootFixedSave(values, 1, bytes);
  check(wrote == bytes.length, 'the writer wrote $wrote of ${bytes.length} bytes');
  final body = t.rootFixedHeaderBytes + 8;
  for (var i = body + 9; i < body + 13; i++) {
    check(bytes[i] == 0, 'byte $i behind the narrower arm is not zero: ${bytes[i]}');
  }
  final plan = t.rootFixedNewPlan();
  final rep = t.TableFixedReport();
  final back = [t.Root()];
  t.initRoot(back[0]);
  final n = t.rootFixedLoad(back, 1, bytes, bytes.length, plan, rep);
  check(n == 1 && rep.refused == 0 && !rep.malformed,
      'the narrow-arm record must read: n=$n ${rep.refused} ${rep.malformed}');
  check(back[0].pick.type == 1 && back[0].pick.a == 0x11223344,
      'the narrow arm landed: type=${back[0].pick.type} a=${back[0].pick.a}');
`, 1)
	})

	// dart/W5 "write checks DEBUG only" — §3.1: "Write-side bound checks are
	// DEBUG ONLY, by rule. `count <= Max` and `length <= N` are a caller
	// contract; a release build removes them exactly as it removes `assert` …
	// and clamps nothing." The emitted writer holds its bounds with Dart's
	// `assert` and never clamps, refuses or reports a write-side bound.
	t.Run("dart/W5", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe

union Pick
{
    a int32
    b int64
}

fixed table Root
{
    note string(12)
    marks [..4]int32
    pick Pick
}
`)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate fixed: %v", err)
		}
		src := string(files["ProbeFixed.dart"])
		writeFn := t12Func(src, "void rootFixedWriteBody(")
		if writeFn == "" {
			t.Fatal("W5: rootFixedWriteBody is not emitted")
		}
		// The writer trusts the caller: its bound checks are `assert`s (removed
		// by a release build) and it never clamps, refuses or counts.
		if !strings.Contains(writeFn, "assert(") {
			t.Error("W5: the write side's bound checks are `assert`s, which release removes (§3.1)")
		}
		if strings.Contains(writeFn, "clamped") {
			t.Error("W5: the write side must clamp nothing (DEBUG-only checks, §3.1)")
		}
	})

	// dart/W8 "bytes(N) takes the array row" — §4.1 fix 10: "A `bytes(N)`
	// takes the ARRAY's row, not a text field's — the count to `aux` and the
	// elements to `dst`". The identity plan's bytes entry is a `text` op whose
	// `meta` lane is the bytes flavour and whose destination is the payload
	// buffer, the length word at `src`.
	t.Run("dart/W8", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe

fixed table Root
{
    blob bytes(6)
}
`)
		st := findTable(t, u, "Root")
		plan, split := fixedIdentityPlan(st)
		if split != len(plan) {
			t.Fatalf("W8: a bytes-only table has no guarded half (split=%d of %d)", split, len(plan))
		}
		var found bool
		for _, e := range plan {
			if e.op != fixedOpText {
				continue
			}
			found = true
			if e.meta != fixedTextBytes {
				t.Fatalf("W8: the bytes(N) row's flavour is %d, want bytes (3)", e.meta)
			}
			if e.aux != e.src+fixedCountBytes {
				t.Fatalf("W8: the bytes(N) row lands its length at src and its elements at the buffer (aux=%d src=%d)", e.aux, e.src)
			}
		}
		if !found {
			t.Fatal("W8: the plan carries no bytes(N) text row")
		}
	})

	// dart/W7 "arg and meta are two lanes" — §4.1 fix 12: "`arg` and `meta`
	// ARE TWO LANES BECAUSE THEY ARE TWO FACTS, and must never share one."
	// A `string(N)` under a union arm needs both: `arg` is the guard's ordinal
	// and `meta` is the op's own flavour. Put the string arm second so the arm
	// ordinal (2) and the utf8 flavour (1) are visibly distinct lanes.
	t.Run("dart/W7", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe

union Pick
{
    n int32
    s string(8)
}

fixed table Root
{
    pick Pick
}
`)
		st := findTable(t, u, "Root")
		plan, split := fixedIdentityPlan(st)
		if split == len(plan) {
			t.Fatal("W7: a union plan must have a guarded half")
		}
		foundText := false
		for _, e := range plan {
			if e.guard == fixedNoGuard || e.op != fixedOpText {
				continue
			}
			foundText = true
			if e.arg == 0 {
				t.Fatalf("W7: a guarded text entry carries no arg ordinal")
			}
			if e.meta == 0 || e.meta > 3 {
				t.Fatalf("W7: a guarded text entry carries no flavour on the meta lane (meta=%d)", e.meta)
			}
			if e.arg == e.meta {
				t.Fatalf("W7: arg and meta share one lane (arg=%d meta=%d)", e.arg, e.meta)
			}
		}
		if !foundText {
			t.Fatal("W7: no guarded string arm entry in the plan")
		}
	})

	// dart/R27 "the corpus round trip" — the C++ reference's byte oracle is
	// build/fixedform-corpus/manifest.txt; every record of every file the
	// manifest names must survive a load and a byte-identical rewrite through
	// this leg's own reader and writer. The four files whose bytes the
	// standard says a rewrite does not restore (a clamp, an older writer's
	// layout) assert the cited bytes instead: fixedcorpus_roundtrip_test.go,
	// fixedCorpusContracts, holds each with its section.
	t.Run("dart/R27", func(t *testing.T) {
		t.Parallel()
		corpus := fixedCorpus(t)
		for file, records := range t12ManifestRows(t, corpus) {
			t.Run(file, func(t *testing.T) {
				t.Parallel()
				fixedCorpusRoundTrip(t, corpus, file, records)
			})
		}
	})

	// dart/P3 "the hostile sweep" — flip every byte of every corpus file the
	// manifest names and drive the reader over it. A graceful refusal or clamp
	// is fine; a Dart exception escaping the reader is a hostile-input finding.
	t.Run("dart/P3", func(t *testing.T) {
		t.Parallel()
		corpus := fixedCorpus(t)
		for file := range t12ManifestRows(t, corpus) {
			base := t12SchemaBase(file)
			if base == "" {
				continue
			}
			t.Run(file, func(t *testing.T) {
				t.Parallel()
				u := t12Schema(t, base)
				out := t12Sweep(t, u, filepath.Join(corpus, file))
				if !strings.Contains(out, "ok") {
					t.Fatalf("P3 sweep of %s printed no ok: %s", file, out)
				}
			})
		}
	})
}

// t12Schema loads a schema by the base name the corpus build used: version rows
// live in test/tables, the rich corpus in tables/examples.
func t12Schema(t *testing.T, base string) *ir.Unit {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "test", "tables")
	if strings.Contains(base, "/") {
		dir = filepath.Join("..", "..", "..", "tables", "examples")
		base = strings.TrimPrefix(base, "examples/")
	}
	path := filepath.Join(dir, base+".schema")
	return versionSchemaPath(t, path, base)
}

func versionSchemaPath(t *testing.T, path, base string) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", base, err)
	}
	ast, perrs := parser.Parse(base+".schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", base, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: base + ".schema", Base: base, Bytes: src, AST: ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", base, cerrs[0])
	}
	return u
}

func t12SchemaBase(file string) string {
	switch {
	case strings.HasPrefix(file, "old_"):
		return "VOLD_" + strings.TrimSuffix(file[len("old_"):], ".bin")
	case strings.HasPrefix(file, "new_"):
		return "VNEW_" + strings.TrimSuffix(file[len("new_"):], ".bin")
	case strings.HasPrefix(file, "mid_"):
		return "VMID_" + strings.TrimSuffix(file[len("mid_"):], ".bin")
	case strings.HasPrefix(file, "a_"):
		return "VBRA_" + strings.TrimSuffix(file[len("a_"):], ".bin")
	case strings.HasPrefix(file, "b_"):
		return "VBRB_" + strings.TrimSuffix(file[len("b_"):], ".bin")
	}
	return map[string]string{
		"fx1.bin":   "FX1",
		"fx2.bin":   "FX2",
		"p1.bin":    "P1",
		"p3.bin":    "P3",
		"fu1.bin":   "FU1",
		"fu2.bin":   "FU2",
		"keyed.bin": "examples/Keyed",
		"pack.bin":  "examples/Pack",

		"many_unknown_census.bin":        "VOLD_unknown_census",
		"past_cfloat_range_widen.bin":    "VNEW_cfloat_range_widen",
		"hostile_cfloat_range_widen.bin": "VNEW_cfloat_range_widen",
	}[file]
}

// t12ManifestRows reads every `file=` and `records=` pair out of the corpus
// manifest, which is the single value oracle.
func t12ManifestRows(t *testing.T, corpus string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is not readable: %v", err)
	}
	out := map[string]int{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		var file string
		var records int
		for field := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				file = v
			case "records":
				fmt.Sscanf(v, "%d", &records)
			}
		}
		if file != "" {
			out[file] = records
		}
	}
	return out
}

// t12Emit generates the fixed codec and the packet library for a unit into a
// fresh temp dir and returns the dir, the codec file name, the root type name
// and its lower name.
func t12Emit(t *testing.T, u *ir.Unit) (string, string, string, string, string) {
	t.Helper()
	st := versionRoot(t, u)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate fixed: %v", err)
	}
	maps.Copy(files, tables)
	lower := lowerFirst(st.Name)
	codec := ""
	typefile := ""
	typ := st.Name
	for name, data := range files {
		if strings.HasSuffix(name, "Fixed.dart") && bytes.Contains(data, []byte(lower+"FixedLoad")) {
			codec = name
		}
		if strings.HasSuffix(name, "Fixed.dart") && bytes.Contains(data, []byte("class "+typ)) {
			typefile = name
		}
	}
	if codec == "" || typefile == "" {
		t.Fatalf("no codec/type file generated (codec=%q type=%q)", codec, typefile)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, codec, typefile, typ, lower
}

// t12RunProbe writes a probe into dir and runs it with the leg's toolchain.
func t12RunProbe(t *testing.T, dir, name, probe string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", name)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe %s failed: %v\n%s", name, err, out)
	}
	return string(out)
}

// t12RoundTrip loads every record of a reference file and rewrites it, holding
// the rewrite byte-identical to the reference bytes.
func t12RoundTrip(t *testing.T, u *ir.Unit, records int, path string) string {
	t.Helper()
	dir, codec, typefile, typ, lower := t12Emit(t, u)
	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import %q as c;
import %q as t;
void check(bool ok, String what) {
  if (!ok) { stderr.writeln('FAIL: $what'); exitCode = 1; }
}
void main() {
  final data = File(%q).readAsBytesSync();
  final plan = c.%sFixedNewPlan();
  final report = t.TableFixedReport();
  final values = <t.%s>[for (var i = 0; i < %d; i++) t.%s()];
  for (final v in values) { t.init%s(v); }
  final n = c.%sFixedLoad(values, values.length, data, data.length, plan, report);
  check(n == %d, 'load returned $n, want %d (refused=${report.refused})');
  check(report.refused == 0 && !report.malformed,
      'a clean reference read: refused=${report.refused} malformed=${report.malformed}');
  final back = Uint8List(c.%sFixedMeasure(%d));
  final wrote = c.%sFixedSave(values, %d, back);
  check(wrote == back.length && back.length == data.length,
      'rewrite length $wrote/${back.length} vs ${data.length}');
  for (var i = 0; i < data.length; i++) {
    check(back[i] == data[i], 'byte $i differs: ${back[i]} vs ${data[i]}');
  }
  if (exitCode == 0) { stdout.writeln('ok'); }
}
`, "./"+codec, "./"+typefile, path, lower, typ, records, typ, typ, lower, records, records, lower, records, lower, records)
	return t12RunProbe(t, dir, "t12_roundtrip.dart", probe)
}

// t12MustRefuse reads a reference file that carries a NaN or an infinity and
// holds the read to the finite rule (docs/SPEC-TABLES.md §3.3): refused as
// non_finite_float, the leaf field named, and every caller value back at its
// defaults.
func t12MustRefuse(t *testing.T, u *ir.Unit, path, leaf string) string {
	t.Helper()
	dir, codec, typefile, typ, lower := t12Emit(t, u)
	probe := fmt.Sprintf(`import 'dart:io';
import %q as c;
import %q as t;
void check(bool ok, String what) {
  if (!ok) { stderr.writeln('FAIL: $what'); exitCode = 1; }
}
void main() {
  final data = File(%q).readAsBytesSync();
  final report = t.TableFixedReport();
  final values = <t.%s>[for (var i = 0; i < 8; i++) t.%s()];
  final n = c.%sFixedLoad(values, values.length, data, data.length, c.%sFixedNewPlan(), report);
  check(n == -1 && report.refused == t.TableFixedRefusal.nonFiniteFloat,
      'a non-finite reference file was not refused as non_finite_float: n=$n refused=${t.TableFixedRefusal.name(report.refused)}');
  check(report.nonFiniteFloat.endsWith(%q),
      'the refusal names "${report.nonFiniteFloat}", want the %s field');
  check(report.unknown == 0 && report.clamped == 0 && report.widened == 0,
      'a refused read moved a counter');
  if (exitCode == 0) { stdout.writeln('ok'); }
}
`, "./"+codec, "./"+typefile, path, typ, typ, lower, lower, "."+leaf, leaf)
	return t12RunProbe(t, dir, "t12_mustrefuse.dart", probe)
}

// t12Sweep flips every byte of a reference file and reads it, failing only if
// an exception escapes the reader.
func t12Sweep(t *testing.T, u *ir.Unit, path string) string {
	t.Helper()
	dir, codec, typefile, typ, lower := t12Emit(t, u)
	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import %q as c;
import %q as t;
void main() {
  final data = File(%q).readAsBytesSync();
  final plan = c.%sFixedNewPlan();
  for (var b = 0; b < data.length; b++) {
    final buf = Uint8List.fromList(data);
    buf[b] ^= 0xFF;
    final report = t.TableFixedReport();
    final values = <t.%s>[for (var i = 0; i < 8; i++) t.%s()];
    for (final v in values) { t.init%s(v); }
    try {
      c.%sFixedLoad(values, values.length, buf, buf.length, plan, report);
    } catch (e) {
      stderr.writeln('FLIP $b threw: $e');
      exitCode = 1;
    }
  }
  if (exitCode == 0) { stdout.writeln('ok'); }
}
`, "./"+codec, "./"+typefile, path, lower, typ, typ, typ, lower)
	return t12RunProbe(t, dir, "t12_sweep.dart", probe)
}
