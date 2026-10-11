package elixirtable

// THE ROADMAP'S CONFORMANCE-INTEGERS TASKS ON THE ELIXIR LEG (docs/roadmap.sexp
// node `fixed-tables`; docs/SPEC-TABLES.md §3.4 fixed form;
// docs/FIXED-FORM-ALGORITHM.md §5.2's PLAN, §5.5's closure;
// docs/FIXED-FORM-VERSIONING-TESTS.md). One subtest per task id, table-driven,
// t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS and the shared runtime
// body it emits with, so the harness is reachable on a tree that never built the
// C++ reference corpus: the compile-and-run half of this leg's fixed form is
// fixedversioning_test.go, fixedform_test.go (whose
// TestFixedLayoutMatchesTheCppReference holds the reference's own constants) and
// test/elixir-fixedform/main.exs, which the corpus gate runs.
//
// plan-execution/elixir/W6 [owed]: the plan PARTITIONS the record image per
// field and a record run SPLITS the file into records; the emitted identity plan
// coalesces the partition back to one run over the body.
//
// plan-execution/elixir/P1 [verify]: the identity lane and the compiled lane run
// the SAME loop; a lane whose wire hash IS this build's own layout is `:identity`
// and runs the emitted `@t_plan`, every other entry compiles a plan.
//
// interoperability/elixir/W13 [verify]: the layout bytes and hash the emitter
// writes are the walk's (ir's, the same one the C++ reference emits from),
// held here against ir and by TestFixedLayoutMatchesTheCppReference against the
// reference's constants.
//
// interoperability/elixir/P2 [verify]: the write's field order and widths and
// the read's pattern are the same list, so write-read-write is byte-identical.
//
// interoperability/elixir/R27 [weak]: the manifest is the wire; the corpus
// manifest `build/fixedform-corpus/manifest.txt` is read as the single value
// oracle and held against the corpus bytes.
//
// hostile-input/elixir/P3 [weak]: hostile bytes are refused by name, whole, and
// a reader never throws.
//
// hostile-input/elixir/P4 [verify]: a wrong plan goes red by name —
// `:plan_too_large` / `:layout_record_too_large` — never a guess.
//
// The seven capability rows each run the emitted writer and reader over a
// NON-DEFAULT and BOUNDARY value of the construct — bool, int8..int64,
// uint8..uint64, int128, uint128, a ranged integer and bits(N) — in a target the
// gate runs: t08RunProbe compiles the generated unit and executes the probe with
// the leg's own toolchain (§5.9 #18). The emitted struct is written, the bytes
// are read back and the value that comes back must equal the value that went in.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t08Scalars is one fixed table carrying every construct this card's capability
// rows name, with a non-default range on the ranged and 128-bit fields so the
// emitted bound is not zero.
const t08Scalars = `package t08pkg

fixed table T
{
    b bool = false
    i8 int8 = 0
    i16 int16 = 0
    i32 int32 = 0
    i64 int64 = 0
    u8 uint8 = 0
    u16 uint16 = 0
    u32 uint32 = 0
    u64 uint64 = 0
    w int128 = 0 | min = -100, max = 100
    v uint128 = 0
    r int32 = 0 | min = -5, max = 5
    three bits(3)
}
`

// t08Module is the emitted fixed module of a unit, the one <Base>Fixed.ex.
func t08Module(t *testing.T, src string) string {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t03Fixed(t, out)
}

// t08Has is a required substring of the emitted source, named by rule.
func t08Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t08Between is the slice of src from the first `from` marker to the first
// `to` marker after it; it bounds one emitted clause.
func t08Between(t *testing.T, src, from, to string) string {
	t.Helper()
	i := strings.Index(src, from)
	if i < 0 {
		t.Fatalf("the emitted source has no %q", from)
	}
	rest := src[i+len(from):]
	j := strings.Index(rest, to)
	if j < 0 {
		t.Fatalf("the emitted source has no %q after %q", to, from)
	}
	return rest[:j]
}

var t08PlanRe = regexp.MustCompile(`(?s)@t_plan \[\s*\{:copy, 0, 0, (\d+)\}\s*\]`)

var t08ScalarRe = regexp.MustCompile(`(?:little-)?(?:signed|unsigned)-\d+`)

// t08WriteTokens is the ordered width list of the emitted write clause.
func t08WriteTokens(t *testing.T, text string) []string {
	t.Helper()
	seg := t08Between(t, text, "def t_fixed_write_into(value, acc) do", "\n    acc\n  end")
	return t08ScalarRe.FindAllString(seg, -1)
}

// t08ReadTokens is the ordered width list of the emitted projection's pattern.
func t08ReadTokens(t *testing.T, text string) []string {
	t.Helper()
	seg := t08Between(t, text, "def t_fixed_decode(\n", "      ) do")
	return t08ScalarRe.FindAllString(seg, -1)
}

// ---- plan-execution/elixir/W6 ----------------------------------------------

// t08W6 holds W6 to the plan's partition and the record split: the emitted
// identity plan coalesces the per-field partition to ONE `{:copy, 0, 0, body}`
// run over the record body, the runtime compiles a peer's plan partition by
// child id (`match_children`) and coalesces it (`coalesce`), and the emitted
// reader splits a file's records (`t_fixed_split`) on the record size.
func t08W6(t *testing.T) {
	text := t08Module(t, t08Scalars)
	st := t03Table(t, unitFrom(t, t08Scalars), "T")
	body := int(ir.TableFixedTypeBytes(st))

	m := t08PlanRe.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("W6: the identity plan does not coalesce the partition to one run:\n%s", text)
	}
	if got, _ := strconv.Atoi(m[1]); got != body {
		t.Errorf("W6: the coalesced plan runs %d bytes, want the body's %d", got, body)
	}
	t08Has(t, fixedRuntimeBody, "defp match_children(theirs, ti, their_at, mine, mi, dst, my_at, guard, tag, acc, report, census) do", "W6 the runtime partitions a plan by child id")
	t08Has(t, fixedRuntimeBody, "def coalesce(entries), do: coalesce(entries, [])", "W6 the runtime coalesces the compiled partition")
	t08Has(t, text, "defp t_fixed_split(<<>>, _hash, _size, acc), do: {:ok, :lists.reverse(acc)}", "W6 the reader splits a file's records")
	t08Has(t, text, "defp t_fixed_split(records, hash, size, acc) do", "W6 the record split walks the rest of the file")
}

// ---- plan-execution/elixir/P1 ----------------------------------------------

// t08P1 holds P1 to §5.3 step 8 and §5.9 #12: an entry whose wire hash IS this
// build's own layout is `:identity`, and that lane runs the emitted `@t_plan`
// over `@t_body_bytes` through the SAME `t_fixed_run` the compiled lane uses —
// identity and compiled are one projector, not two.
func t08P1(t *testing.T) {
	text := t08Module(t, t08Scalars)
	t08Has(t, fixedRuntimeBody, "defp lineage_lane_of(%{hash: h}, own, _mine, _dst, _capacity) when h == own, do: :identity", "P1 an own-hash entry is the identity lane")
	t08Has(t, text, "defp t_fixed_lane(i, records, hash, report, cap, bcap, copy) do", "P1 the lane dispatch")
	t08Has(t, text, ":identity ->\n        t_fixed_run(@t_plan, @t_body_bytes, records, hash, report, copy, bcap, {0, 0})", "P1 identity runs the emitted plan")
	t08Has(t, text, "{:ok, plan, size, _made, u, k} ->\n        t_fixed_run(plan, size, records, hash, report, copy, bcap, {u, k})", "P1 the compiled lane runs the compiled plan through the same loop")
}

// ---- interoperability/elixir/W13 -------------------------------------------

// t08W13 holds W13 to the shared walk: the layout the emitter writes and the
// hash it prints are ir's own, the same walk the C++ reference emits from
// (TestFixedLayoutMatchesTheCppReference holds that walk to the reference's
// constants).
func t08W13(t *testing.T) {
	text := t08Module(t, t08Scalars)
	st := t03Table(t, unitFrom(t, t08Scalars), "T")
	want := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	got := t03Layout(t, text, "t")
	if len(got) != len(want) {
		t.Fatalf("W13: the emitted layout is %d bytes, ir's walk is %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("W13: the emitted layout byte %d = %#x, ir's walk (the C++ reference's) = %#x", i, got[i], want[i])
		}
	}
	if h := t03Hash(t, text, "t"); h != ir.TableFixedLayoutHash(want, st) {
		t.Errorf("W13: the emitted @t_hash = %#x, ir's = %#x", h, ir.TableFixedLayoutHash(want, st))
	}
}

// ---- interoperability/elixir/P2 --------------------------------------------

// t08P2 holds P2 to write-read-write byte identity: the write lays the fields in
// declared order at their declared storage widths and the projection matches the
// SAME widths in the SAME order, so reading a written image returns the value
// that writes the same bytes.
func t08P2(t *testing.T) {
	text := t08Module(t, t08Scalars)
	w := t08WriteTokens(t, text)
	r := t08ReadTokens(t, text)
	if len(w) == 0 || len(r) == 0 {
		t.Fatalf("P2: write widths %v, read widths %v", w, r)
	}
	if strings.Join(w, " ") != strings.Join(r, " ") {
		t.Errorf("P2: the write lays %v, the read matches %v", w, r)
	}
	t08Has(t, text, "<<h::little-unsigned-64, body::binary-size(^size), rest::binary>> when h == hash ->", "P2 the read takes the per-record hash then the same body size")
	t08Has(t, text, "t_fixed_write_into(value, <<@t_hash::little-unsigned-64>>)", "P2 the write lands the same eight-byte hash before the body")
}

// ---- interoperability/elixir/R27 -------------------------------------------

// t08R27 holds R27 to the manifest as the wire's single value oracle: the corpus
// manifest `build/fixedform-corpus/manifest.txt` — §5.9 #32's one line per row —
// is read as the oracle and its brackets are held against the corpus bytes, so
// the elixir leg reads to the same value oracle every other leg reads to.
//
// This is the same assertion the lineage harness's TestFixedVersioningReads
// TheManifest makes, restated here so the card's own test proves it; the branch
// is live only when the tree carries the manifest, so a tree without one skips
// rather than reading the very `.bin` it asserts.
func t08R27(t *testing.T) {
	corpus := fixedCorpus(t)
	if _, err := os.Stat(filepath.Join(corpus, "manifest.txt")); err != nil {
		t.Skip("the tree carries no corpus manifest")
	}
	fired := 0
	for _, r := range versionRows {
		lead, trail, ok := manifestBrackets(t, corpus, "old_"+r.row+".bin")
		if !ok {
			// the row is in the manifest — manifestBrackets fails by name when
			// it is not — and its table declares no brackets.
			continue
		}
		data, err := os.ReadFile(filepath.Join(corpus, "old_"+r.row+".bin"))
		if err != nil {
			t.Fatalf("R27 %s: %v", r.row, err)
		}
		body := fixedTypeBytes(versionRoot(t, versionSchema(t, "VOLD_"+r.row)))
		at := 20 + int64(binary.LittleEndian.Uint32(data[16:20])) + 8
		wantLead := binary.LittleEndian.Uint32(data[at : at+4])
		wantTrail := binary.LittleEndian.Uint32(data[at+body-4 : at+body])
		if lead != wantLead || trail != wantTrail {
			t.Fatalf("R27 %s: the manifest says lead=0x%08X trail=0x%08X, the bytes say 0x%08X 0x%08X",
				r.row, lead, trail, wantLead, wantTrail)
		}
		fired++
	}
	// A BRANCH THAT NEVER FIRED IS THE BUG THIS TEST EXISTS FOR.
	if fired == 0 {
		t.Fatal("R27: not one row read its brackets from build/fixedform-corpus/manifest.txt: the manifest is not the oracle")
	}
}

// ---- hostile-input/elixir/P3 -----------------------------------------------

// t08P3 holds P3 to the refusal-by-name of hostile bytes: a layout is refused
// WHOLE (`parse_layout`) and every hostile path answers an `{:error, reason}`
// tuple that the emitted reader returns by name, never a throw (`rescue`).
func t08P3(t *testing.T) {
	text := t08Module(t, t08Scalars)
	t08Has(t, fixedRuntimeBody, "def parse_layout(bytes) when is_binary(bytes) do", "P3 a layout is parsed whole")
	t08Has(t, fixedRuntimeBody, "defp layout_closes(_, _), do: {:error, :layout_tree_unclosed}", "P3 an unclosed layout is refused whole")
	t08Has(t, fixedRuntimeBody, "rescue\n    _ -> {:error, :layout_malformed}", "P3 a hostile entry answers by name, never a throw")
	t08Has(t, text, "{:error, :malformed, %{report | malformed: true}}", "P3 a short file is malformed, not a throw")
	t08Has(t, text, "def t_fixed_load(data, opts \\\\ []) when is_binary(data) do", "P3 the family read verdict is a tuple")
}

// ---- hostile-input/elixir/P4 -----------------------------------------------

// t08P4 holds P4 to a wrong plan going red by name: the runtime refuses a plan
// past the writer's record (`:layout_record_too_large`) or past the caller's
// capacity (`:plan_too_large`), and the emitted lane returns each by name.
func t08P4(t *testing.T) {
	text := t08Module(t, t08Scalars)
	t08Has(t, fixedRuntimeBody, "{:error, :layout_record_too_large, report}", "P4 an entry past the writer's record is refused")
	t08Has(t, fixedRuntimeBody, "n > capacity ->\n        {:error, :plan_too_large, report}", "P4 a plan past capacity is refused")
	t08Has(t, text, "when made > cap ->", "P4 the declared capacity is checked")
	t08Has(t, text, "{:error, :plan_too_large, report}", "P4 the lane answers plan_too_large by name")
	t08Has(t, text, "{:error, why, report}", "P4 a plan that cannot build answers its own name, never a throw")
}

// ---- the capability rows: a runtime write/read in a target the gate runs ----

// t08RunProbe generates the fixed module for src, writes a generated probe that
// runs `body` against the root table, and runs it with the leg's own toolchain
// (§5.9 #18: a probe is generated, never hand-written). It mirrors
// runVersionProbe with the schema held in memory rather than on disk, and the
// body sees `T` = the fixed module, `R` = its FixedRuntime and `V` = the root's
// struct. A body that fails its `check/2` halts nonzero and the failure text is
// the probe's.
func t08RunProbe(t *testing.T, src, body string) {
	t.Helper()
	u := unitFrom(t, src)
	files, err := elixir.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate fixed: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var names []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	st := versionRoot(t, u)
	mod := ir.GoExportName(u.Package) + "." + ir.GoExportName(fileOf(t, u, st.Name)) + FixedModuleSuffix
	probe := fmt.Sprintf(`# GENERATED BY internal/codegen/elixirtable/fixedroadmap_t08_test.go — the
# conformance-integers probe (docs/SPEC-TABLES.md §3.4, §5.9 #18).
defmodule Probe do
  alias %[1]s, as: T
  alias %[2]s.FixedRuntime, as: R
  alias %[3]s, as: V

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def run do
%[4]s
    if Process.get(:failed) do
      System.halt(1)
    else
      IO.puts("ok")
    end
  end
end

Probe.run()
`, mod, ir.GoExportName(u.Package), ir.GoExportName(u.Package)+"."+st.Name, indentBody(body))
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{}
	for _, n := range sortedRuntimeFirst(names) {
		args = append(args, "-r", n)
	}
	args = append(args, "probe.exs")
	cmd := exec.Command(elixirBinary(t), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
}

// t08RoundTrip is the prose every capability body carries: `values` are written
// with the emitted writer and read back with the emitted reader, and each must
// return as itself on a read that moves no counter. A body names its row.
func t08RoundTrip(what string, values ...string) string {
	wrapped := make([]string, len(values))
	for i, v := range values {
		wrapped[i] = "%" + v
	}
	return "for v <- [" + strings.Join(wrapped, ", ") + "] do\n" +
		"  saved = T.t_fixed_save([v])\n" +
		"  {tag, [back], report} = T.t_fixed_load(saved)\n" +
		"  check(tag == :ok and report.malformed == false, \"" + what + ": write/read refused: #{inspect({tag, report})}\")\n" +
		"  check(back == v, \"" + what + ": #{inspect(v)} did not round-trip: #{inspect(back)}\")\n" +
		"end"
}

// ---- bool-values/elixir/valid-data -----------------------------------------

// t08Bool holds the bool row to §3.4 record, bool: the writer lands one byte,
// zero or one, and the reader turns a non-zero byte back into true. `true` is
// the non-default value the default `false` is not.
func t08Bool(t *testing.T) {
	t08RunProbe(t, t08Scalars, t08RoundTrip("bool-values", "V{b: true}", "V{b: false}"))
}

// ---- signed-integers/elixir/valid-data -------------------------------------

// t08Signed holds the signed-integers row to §3.4 record, int8..int64: all FOUR
// widths are written and read little-endian signed, at BOTH ends of storage.
func t08Signed(t *testing.T) {
	t08RunProbe(t, t08Scalars, t08RoundTrip("signed-integers",
		"V{i8: -128, i16: -32768, i32: -2147483648, i64: -9223372036854775808}",
		"V{i8: 127, i16: 32767, i32: 2147483647, i64: 9223372036854775807}"))
}

// ---- unsigned-integers/elixir/valid-data -----------------------------------

// t08Unsigned holds the unsigned-integers row to §3.4 record, uint8..uint64:
// all FOUR widths are written and read little-endian unsigned, at zero and at
// the top of storage.
func t08Unsigned(t *testing.T) {
	t08RunProbe(t, t08Scalars, t08RoundTrip("unsigned-integers",
		"V{u8: 0, u16: 0, u32: 0, u64: 0}",
		"V{u8: 1, u16: 1, u32: 1, u64: 1}",
		"V{u8: 255, u16: 65535, u32: 4294967295, u64: 18446744073709551615}"))
}

// ---- signed-128/elixir/valid-data ------------------------------------------

// t08Signed128 holds the signed-128 row to §3.4 and SPEC §4.3: a raw 128-bit
// signed field is the RANGED operation, sixteen bytes little-endian signed, and
// the emitted bound sits before the value. The declared bounds -100 and 100 are
// both written and read back.
func t08Signed128(t *testing.T) {
	t08RunProbe(t, t08Scalars, t08RoundTrip("signed-128",
		"V{w: -100}", "V{w: 0}", "V{w: 100}"))
}

// ---- unsigned-128/elixir/valid-data ----------------------------------------

// t08Unsigned128 holds the unsigned-128 row to §3.4 record, uint128: sixteen
// bytes little-endian unsigned, written and read — at zero, at 2^64 (a NONZERO
// HIGH LIMB) and at the top of 128-bit storage.
func t08Unsigned128(t *testing.T) {
	t08RunProbe(t, t08Scalars, t08RoundTrip("unsigned-128",
		"V{v: 0}",
		"V{v: 18446744073709551616}",
		"V{v: 340282366920938463463374607431768211455}"))
}

// ---- ranged-integers/elixir/valid-data -------------------------------------

// t08Ranged holds the ranged-integers row to §3.4: a ranged field rides as its
// storage-width integer, clamped to its declared non-default bound. The declared
// bounds -5 and 5 are both written and read back.
func t08Ranged(t *testing.T) {
	t08RunProbe(t, t08Scalars, t08RoundTrip("ranged-integers",
		"V{r: -5}", "V{r: 0}", "V{r: 5}"))
}

// ---- bits-values/elixir/valid-data -----------------------------------------

// t08Bits holds the bits-values row to §3.4 and SPEC-TABLES §5256/§6728:
// bits(N) is RANGED with base 0 and width N, so it rides as the storage-width
// integer — 4 bytes for N <= 32 — written and read, including the top of the
// 3-bit domain.
func t08Bits(t *testing.T) {
	t08RunProbe(t, t08Scalars, t08RoundTrip("bits-values",
		"V{three: 0}", "V{three: 7}"))
}

// ---- the card's test -------------------------------------------------------

func TestFixedRoadmapElixirT08ConformanceIntegers(t *testing.T) {
	t.Parallel()

	tasks := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"elixir/W6", t08W6},
		{"elixir/P1", t08P1},
		{"elixir/W13", t08W13},
		{"elixir/P2", t08P2},
		{"elixir/R27", t08R27},
		{"elixir/P3", t08P3},
		{"elixir/P4", t08P4},
		{"bool-values/elixir/valid-data", t08Bool},
		{"signed-integers/elixir/valid-data", t08Signed},
		{"unsigned-integers/elixir/valid-data", t08Unsigned},
		{"signed-128/elixir/valid-data", t08Signed128},
		{"unsigned-128/elixir/valid-data", t08Unsigned128},
		{"ranged-integers/elixir/valid-data", t08Ranged},
		{"bits-values/elixir/valid-data", t08Bits},
	}

	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.run(t)
		})
	}
}
