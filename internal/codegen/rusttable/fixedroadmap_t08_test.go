package rusttable

// THE ROADMAP'S WRITES-CONFORMANCE TASKS ON THE RUST LEG (docs/roadmap.sexp
// node `fixed-tables`, work-sets `union-guards/rust`, `plan-execution/rust`,
// `interoperability/rust`, `hostile-input/rust`; docs/FIXED-FORM-ALGORITHM.md
// §4.1 "The plan" and §4.4 "The run"; docs/SPEC-TABLES.md §3.4). One subtest
// per task id this card proves, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs — the
// same shape fixedroadmap_t02_test.go and fixedroadmap_t04_test.go state for
// the same reason. A clause that needs the emitted code to RUN against the C++
// reference's own bytes is NOT asserted here, because that oracle is
// `build/fixedform-corpus`, which the C++ reference writes and this tree cannot
// build without a sibling serialize checkout. Those assertions live in
// test/rust-fixedform/src/main.rs (the_write, the_compiled_plan,
// the_negative_controls, the_hostile_sweep) and in the versioning harness, and
// they RUN only under `make tables-rust-fixedform` / `make tables-rust-versioning`,
// which this job names as not run. Each such task is reported `unknown` in the
// verdict with the clause and the reason, and is not given a subtest here.
//
// rust/W7  [owed] "arg and meta are two lanes" — asserted below.
// rust/W16 [owed] "nested union answers outer tag" — asserted below.
// rust/R28 [owed] "the guard chain ..." — asserted below: the runtime carries an
//   offset+count guard chain into a pool of (guard, arg, argw) links, outermost
//   first, bounded by the layout's 64, and a rustc-built probe RUNS a nested
//   union's active and inactive arms to prove every link participates (§4.1).
// rust/W6  [owed] "plan partition / split" — unknown, not asserted:
//   table_fixed_compile coalesces but never partitions the plan
//   unguarded-first, and no split index is stated (§4.1, §4.4).
// rust/P1  [verify] "identity == compiled" — asserted below.
// rust/W13 [verify] "layout+hash == C++ reference" — unknown: the clause is the
//   harness's whole-file comparison (round_trip!'s `out == golden`,
//   test/rust-fixedform/src/main.rs), which RUNS only against the C++ reference
//   corpus this tree cannot build.
// rust/P2  [verify] "write-read-write byte-identical" — unknown: the same oracle.
// rust/R27 [verify] "the manifest is the wire ..." — unknown: the manifest is
//   build/fixedform-corpus/manifest.txt, written by the C++ reference build this
//   tree cannot make.
// rust/P3  [owed] "hostile bytes, sweep + sanitizer" — unknown: the sweep is the
//   harness's the_hostile_sweep, which RUNS only against the corpus.
// rust/P4  [verify] "wrong plan goes red" — unknown: the negative control is the
//   harness's the_negative_controls, which RUNS only against the corpus.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- readers of this leg's output ------------------------------------------

// t08Runtime is the shared fixed runtime body, the one module every fixed unit
// carries and where the plan entry, the one read loop and the plan compiler
// live.
func t08Runtime(t *testing.T) string {
	t.Helper()
	s := string(fixedRuntimeBody)
	if s == "" {
		t.Fatal("the fixed runtime body is empty")
	}
	return s
}

// t08Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t08Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t08Has is a required substring, named by rule.
func t08Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// ---- rust/W7 -----------------------------------------------------------------

// t08W7 holds rust/W7 to §4.1's two-facts sentence: "`arg` and `meta` ARE TWO
// LANES BECAUSE THEY ARE TWO FACTS, and must never share one." (fix 12). On
// this leg the guard's ordinal rides in `arg`, at FULL WIDTH, and the op's own
// argument rides in `aux` (a second destination, a side-table offset, or a
// constant) — two fields, never one — and the text flavour has no runtime lane
// at all (the compile-time kind that selected the op says utf8 or wide), so a
// text field under a union arm is guarded and read with the right flavour
// without one lane being stamped over the other. The bite is the two
// declarations standing apart: a lane shared between the ordinal and the op's
// argument would read a byte string under arm 2 as WIDE, or run an entry only
// when the tag equalled the flavour.
func t08W7(t *testing.T) {
	rt := t08Runtime(t)
	// the guard's ordinal is full width, and it is its own lane
	t08Has(t, rt, "pub arg: u64,", "W7 the guard's ordinal is a full-width lane")
	t08Has(t, rt, "/// THE GUARD'S TAG and nothing else", "W7 arg is the guard's ordinal and nothing else")
	// the op's own argument is a SEPARATE lane, named meta in §4.1 and aux here
	t08Has(t, rt, "pub aux: u32,", "W7 the op's argument is a separate lane")
	if strings.Contains(rt, "pub arg_aux") || strings.Contains(rt, "pub argmeta") {
		t.Error("W7: the ordinal and the op argument still share one lane")
	}
}

// ---- rust/W16 ----------------------------------------------------------------

// t08W16 holds rust/W16 to §4.1's stamp sentence: "A union's guard is stamped
// over everything its arm produced — an arm inside an arm answers to the OUTER
// tag — and a nested union's own arm selection must survive that stamp." On
// this leg the plan compiler's union case lays the None const down with the
// INHERITED guard chain (the OUTER tag's links, the `goff`/`gcount` the caller
// threaded down) and each arm's tag const with THIS union's own link CONJOINED
// on top — the two are spelled differently in the emitted runtime, and that
// difference is the clause. A nested union's None therefore answers the outer
// tag, never the inner one.
func t08W16(t *testing.T) {
	rt := t08Runtime(t)
	// the None const carries the INHERITED chain, at the outer tag, and the
	// comment states the rule it implements
	t08Has(t, rt, "None answers to the OUTER", "W16 the nested union's None answers the outer tag")
	none := strings.Index(rt, "None answers to the OUTER")
	if none < 0 {
		return
	}
	rest := rt[none:]
	if end := strings.Index(rest, "op: TableFixedOp::Const,"); end < 0 {
		t.Fatal("W16: the None const's shape moved")
	} else {
		rest = rest[:end]
	}
	// the None const's guard chain is the INHERITED chain (the bare `goff` /
	// `gcount` of the parameters), not this union's own conjoined arm link
	for _, inherited := range []string{"guard_off: goff as u32,", "guard_count: gcount as u8,"} {
		if !strings.Contains(rest, inherited) {
			t.Errorf("W16: the None const does not answer the OUTER tag (missing %q):\n%s", inherited, rest)
		}
	}
	if strings.Contains(rest, "n_off") || strings.Contains(rest, "n_count") {
		t.Error("W16: the None const is stamped with this union's own tag, not the outer one")
	}
}

// ---- rust/P1 -----------------------------------------------------------------

// t08P1 holds rust/P1 to §4.1's identity sentence — "An identity plan carries
// only `copy`, `count` and `text`" — and to §4.4's one loop: "READ( plan, split,
// record, out )" runs the SAME loop over the identity plan and over a plan
// compiled from a stranger's layout. On this leg the identity plan is ONE
// whole-body Copy (the image-domain coalescing rule taken to its end), and the
// emitted load has a SINGLE `table_fixed_run` call site reached by both the
// identity and the compiled branch — there is no second reader path.
func t08P1(t *testing.T) {
	u := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("P1: Generate: %v", err)
	}
	text := t08Fixed(t, files)
	// the identity plan is ONE entry, a whole-body copy — only Copy, no count
	// or text because a plain scalar record has none
	t08Has(t, text, "THE IDENTITY PLAN, and it is ONE ENTRY", "P1 the identity plan is one entry")
	t08Has(t, text, "pub const LINEAGE_FIXED_PLAN: [TableFixedEntry; 1] = [TableFixedEntry {", "P1 the identity plan is a single-entry array")
	t08Has(t, text, "op: TableFixedOp::Copy,", "P1 the identity plan carries only Copy")
	// the SAME loop runs over either plan: ONE run call site, shared by the
	// identity branch and the compiled branch
	load := text
	if at := strings.Index(text, "pub fn lineage_fixed_load("); at >= 0 {
		load = text[at:]
	}
	if n := strings.Count(load, "table_fixed_run(entries, guards, table, &record[8..], &mut image, report);"); n != 1 {
		t.Errorf("P1: the read loop's run call appears %d times, want exactly one shared by both plans", n)
	}
	t08Has(t, text, "SAME LOOP either way", "P1 one reader path, identity and compiled alike")
}

// ---- rust/R28 ----------------------------------------------------------------

// t08R28Schema is one fixed table whose field is a nested union — a union arm
// that is itself a union — so a plan compiled for it carries a TWO-LINK guard
// chain (the OUTER tag first, the inner tag second) and the run loop must check
// every link (§4.1's follow-on).
const t08R28Schema = `package probe

union Inner
{
    a int32
    b int32
}

union Outer
{
    inner Inner
    plain int32
}

// the checker admits a scalar arm only in a union some table's field names, so
// Inner is held here as well as through Outer's arm (docs/SPEC-TABLES.md §2.6)
table Twig
{
    inner Inner
}

fixed table Probe
{
    outer Outer
}
`

// t08R28Probe is the Rust binary's crate root: the generated fixed modules, one
// compile of the unit's own block against itself (which takes the union-arm
// path, so every nested arm's entry grows a guard chain), an assertion on the
// chain's shape, and three runs of the plan over hand-built bodies that select
// the outer and inner arms together and apart. A task's line is `R28 <id> ok`
// or `R28 <id> BAD <why>`.
func t08R28Probe() string {
	return `mod build_version;
mod fixed_runtime;
mod probe_fixed;
mod probe_records;

pub use build_version::*;
pub use fixed_runtime::*;
pub use probe_fixed::*;
pub use probe_records::*;

fn say(id: &str, ok: bool, why: &str) {
    if ok {
        println!("R28 {id} ok");
    } else {
        println!("R28 {id} BAD {why}");
    }
}

fn run(body: &[u8], plan: &[TableFixedEntry], guards: &[TableFixedGuard], remap: &[u16]) -> Vec<u8> {
    let mut image = vec![0u8; PROBE_FIXED_BODY_BYTES];
    let mut report = TableFixedReport::default();
    table_fixed_run(plan, guards, remap, body, &mut image, &mut report);
    image
}

fn main() {
    let mine = TableFixedBlock::parse(&PROBE_FIXED_BLOCK).expect("this build's own block parses");
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut guards = vec![TableFixedGuard::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = table_fixed_compile(&mine, &mine, &PROBE_FIXED_COUNTED, &mut plan, &mut guards, &mut remap, &mut report)
        .expect("the own-against-own plan compiles");

    // A nested union's inner-arm entries carry a TWO-LINK chain, OUTERMOST
    // FIRST: link 0 is the OUTER tag (this union's own tag at body offset 0)
    // and link 1 the inner tag, so an arm inside an arm answers the OUTER tag
    // too and never the inner tag alone.
    let mut two = None;
    for i in 0..n {
        if plan[i].guard_count == 2 {
            two = Some(i);
            break;
        }
    }
    let Some(i) = two else {
        say("chain", false, "no entry carried a two-link guard chain");
        return;
    };
    let off = plan[i].guard_off as usize;
    let outer = guards[off];
    let inner = guards[off + 1];
    let shape = outer.guard == 0 && inner.guard > outer.guard && outer.arg == 1 && inner.arg == 1;
    if !shape {
        say("chain", false, &format!("chain is not outermost-first: [{outer:?}, {inner:?}]"));
        return;
    }
    say("chain", true, "");

    // BODY: byte 0 is the outer tag, byte 1 the inner tag when the inner arm
    // rode, bytes 2..6 the arm payload. The inner arm's payload lands only in
    // bytes 2..6, so byte 5 is its own: the plain arm (4 bytes at 1..5) never
    // writes it.
    // outer active + inner active: the inner arm's payload lands.
    let a = run(&[1, 1, 0, 0, 0, 42], &plan[..n], &guards, &remap);
    say("active-active", a[5] == 42, "an active inner arm did not land its payload");
    // outer inactive + inner tag present: the inner arm must NOT land, because
    // it answers to the OUTER tag too.
    let b = run(&[2, 1, 0, 0, 0, 42], &plan[..n], &guards, &remap);
    say("outer-inactive", b[5] == 0, "an inner arm landed under an inactive outer arm");
    // outer active + inner None: the inner arm must NOT land, because the
    // inner tag is its own condition.
    let c = run(&[1, 0, 0, 0, 0, 42], &plan[..n], &guards, &remap);
    say("inner-none", c[5] == 0, "an inner arm landed under a None inner tag");
}
`
}

// t08R28Run generates the unit (with a two-entry lineage, so the plans struct,
// the guard pool and the load path's compile and run call sites are all
// emitted and compiled too), drops the probe crate, and runs it once with rustc
// (the fixed runtime is self-contained, so no sibling checkout is needed).
func t08R28Run(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc is not on PATH: %v", err)
	}
	u := unitFrom(t, t08R28Schema)
	own, ok := FixedLineageOf(u, "Probe")
	if !ok {
		t.Fatalf("the nested-union schema grew no fixed root")
	}
	old := own
	old.Wire ^= 0xffffffffffffffff // a different hash, the same layout bytes
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Probe": {old}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	if _, ok := files["probe_fixed.rs"]; !ok {
		t.Fatalf("the nested-union schema grew no fixed form; got %v", keysOf(files))
	}
	root := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(t08R28Probe()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "t08r28probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = root
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	return string(out)
}

// t08R28 holds rust/R28 to §4.1's follow-on: "the guard chain: the plan entry
// carries offset+count into a pool of (guard, arg, argw) links, outermost
// first, the only bound the layout's 64; an arm inside an arm answers to the
// OUTER tag". The runtime carries the offset+count pool (TableFixedGuard), and
// the rustc-built probe proves the chain is laid outermost-first and that
// EVERY link participates: the inner arm lands only when BOTH tags hold their
// ordinal, not when the outer tag fails or the inner tag is None.
func t08R28(t *testing.T) {
	rt := t08Runtime(t)
	// the pool and the entry's offset+count lanes exist, and the bound is 64
	t08Has(t, rt, "pub struct TableFixedGuard {", "R28 the pool of (guard, arg, argw) links exists")
	t08Has(t, rt, "pub guard_off: u32,", "R28 the entry carries the pool offset")
	t08Has(t, rt, "pub guard_count: u8,", "R28 the entry carries the pool count")
	t08Has(t, rt, "count + 1 > MAX_DEPTH as usize", "R28 the chain is bounded by the layout's 64")
	out := t08R28Run(t)
	for _, id := range []string{"chain", "active-active", "outer-inactive", "inner-none"} {
		if line := t08R28Line(out, id); line != "R28 "+id+" ok" {
			t.Errorf("R28: %s", line)
		}
	}
}

// t08R28Line is the probe's one line for an id.
func t08R28Line(out, id string) string {
	prefix := "R28 " + id + " "
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no R28 line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapRustT08WritesConformance(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/W7", check: t08W7},
		{id: "rust/W16", check: t08W16},
		{id: "rust/R28", check: t08R28},
		{id: "rust/P1", check: t08P1},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
