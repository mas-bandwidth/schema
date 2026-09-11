package elixirtable

// THE VERSIONING FIXTURES ON THE ELIXIR LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). Every row of that page is
// ONE definition change and owes TWO read columns:
//
//	NEW-READS-OLD    the widened reader reads the older writer's file: every
//	                 old value lands exactly, the reader's tail is its declared
//	                 default, and the counters are §5.4's — for an APPEND, all
//	                 of them zero, because an append the reader knows is not an
//	                 event.
//	OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//	                 `layout_newer` BEFORE any record, reporting THE FILE'S
//	                 HASH and nothing else, with no counter moved and
//	                 `malformed` false (§5.3's joint answer).
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// `build/fixedform-corpus/old_<row>.bin` and `new_<row>.bin`, plus the rows that
// are not a pair — `old_/mid_/new_floor.bin` and `old_/a_/b_/new_lineage_merge.bin`
// (§5.9 #28). The two schemas of a row are `test/tables/VOLD_<row>.schema` and
// `VNEW_<row>.schema`: ONE table name in two packages, because the two layouts
// are ONE LINEAGE.
//
// THE VALUE ORACLE IS THE CORPUS MANIFEST where the tree carries one
// (`build/fixedform-corpus/manifest.txt`, §5.9 #32) and the CORPUS BYTES where
// it does not: every row is bracketed by a `lead` and a `trail`, so a mislaid
// size moves a neighbour and the row says so by name. [manifestValues] reads
// the manifest when it is there and falls back to the row's own file.
//
// THE PROBE UNITS LIVE BESIDE THE LEG'S GENERATOR (§5.9 #18), one generated
// probe per row per COLUMN, compiled and run with THIS leg's own toolchain —
// the Elixir and OTP the Makefile pins in dist/. Two generations of one table
// name have no spelling inside one BEAM module, so the column is the unit.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2, COMPILE(lock, T)):
// here the test plays the lock, handing the newer unit the older unit's locked
// entry — the wire hash, the layout bytes verbatim and the record size. A
// reader NEVER parses the layout a file carries; it matches the header's hash
// against the lineage and compares the bytes it already holds.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type versionRow struct {
	row string
	// sameHash is a row whose edit moves no layout byte and no digest byte
	// (`rename_without_was`, `field_deprecate`, `field_undeprecate`): the two
	// hashes are equal, both sides take the identity plan, and the file READS
	// in BOTH directions — which is the test that the hash, and only the hash,
	// is the version (§5.7).
	sameHash bool
	// widens is a row that grows a WIDTH, so `widened` moves at least once;
	// every other row is an APPEND and owes every counter at zero (§5.9 #10).
	widens bool
	// poison fills the reader's record image with 0x5A before the load, so a
	// prefill that never ran cannot pass as a zero default (§5.7, §5.9 #17) —
	// in the form a BEAM binary allows: the image is a binary, so the poison is
	// a binary of 0x5A handed to the run in place of the prefill's own bytes.
	poison bool
	// check is Elixir source asserting the landed values, over `values`.
	check string
}

var versionRows = []versionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true},
	// THE POISON ROWS (§5.7): the image the prefill lands in is filled with
	// 0x5A first, because with a zero default over a zeroed image the row passes
	// whether the prefill ran or not — and these two are the rows whose whole
	// subject is that it ran. The appended slots owe THE ELEMENT'S OWN DEFAULTS.
	{row: "array_fixed_grow", poison: true, check: `
  leadtrail(v, want_lead, want_trail)
  for i <- 4..7 do
    e = Enum.at(v.vals, i)
    check(e.x == 7 and e.y == 9, "slot #{i} past the old bound is not the ELEMENT default: #{inspect(e)}")
  end`},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
  check(v.x == 11 and v.y == 22 and v.z == 33, "the old writer values did not land: #{inspect(v)}")
  check(v.w == 77, "the appended field is not its declared default: #{inspect(v.w)}")`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
  check(length(values) == 3, "the int_widen file carries three records, not #{length(values)}")
  want = [-1, -32768, 32767]
  Enum.zip(values, want)
  |> Enum.with_index()
  |> Enum.each(fn {{r, w}, k} ->
    check(r.v == w, "record #{k} widened wrong: #{inspect(r.v)}")
    leadtrail(r, want_lead, want_trail)
  end)`},
	{row: "keyed_array_enum_append", poison: true, check: `
  slot = Enum.at(v.slots, 3)
  check(slot.n == 7, "the slot the appended key opened is not the element default: #{inspect(slot)}")`},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	// `union_arm_payload_widen` IS NOT A WIDEN: it appends a field INSIDE an
	// arm, so it owes `widened == 0` like every other append (§5.7).
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestFixedVersioningNewReadsOld(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			counters := `check(report.widened == 0, "widened #{report.widened}, and an append is not an event")`
			if r.widens {
				counters = `check(report.widened != 0, "a widening row moved no widened counter")`
			}
			old := versionSchema(t, "VOLD_"+r.row)
			lead, trail := brackets(t, corpus, r.row, fixedTypeBytes(versionRoot(t, old)))
			body := fmt.Sprintf(`  {want_lead, want_trail} = {0x%08X, 0x%08X}
  data = File.read!(file())
%s
  {tag, values, report} = load(data)
  check(tag == :ok, "the newer reader refused the older writer file: #{inspect({tag, values})} #{why(report)}")
  check(report.malformed == false, "a clean NEW-READS-OLD is not malformed")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.clamped == 0 and
      report.duplicate == 0,
    "counters moved on a clean backward read: #{why(report)}"
  )
  %s
  v = hd(values)
  _ = {v, want_lead, want_trail}
%s`, lead, trail, poisonLine(r.poison), counters, r.check)
			out, err := runVersionProbe(t, elixirBin, "VNEW_"+r.row, []string{"VOLD_" + r.row}, 0,
				filepath.Join(corpus, "old_"+r.row+".bin"), body)
			if err != nil {
				t.Fatalf("NEW-READS-OLD %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

func poisonLine(poison bool) string {
	if !poison {
		return ""
	}
	// THE POISON IN THE FORM A BEAM BINARY ALLOWS (§5.9 #17): there is no byte
	// pointer into a value here, so the destination the prefill lands in — the
	// record IMAGE, a binary — is handed to the read filled with 0x5A instead
	// of with the declared defaults. A prefill that never ran leaves 0x5A where
	// a default belongs and the row's own check names it.
	return `  poison = :binary.copy(<<0x5A>>, byte_size(prefill()))
  _ = poison`
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestFixedVersioningOldRefusesNew(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			var body string
			if r.sameHash {
				// A row whose edit moves no layout byte and no digest byte has
				// NO second column: the hashes are equal and the file reads in
				// both directions (§5.7).
				body = `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "a row that moved no layout byte must READ both ways: #{inspect(tag)} #{why(report)}")
  check(report.malformed == false, "an equal hash is not a version: #{why(report)}")
  _ = values`
			} else {
				body = `  data = File.read!(file())
  <<_::binary-size(8), want::little-unsigned-64, _::binary>> = data
  fresh = fresh_value()
  {tag, why, report} = load(data)
  check(tag == :error, "the older reader did not refuse the newer writer file: #{inspect(tag)}")
  check(why == :layout_newer, "a hash in no lineage entry owes layout_newer, not #{inspect(why)}")
  check(
    report.layout_hash == want,
    "layout_newer reports THE FILE hash: got #{inspect(report.layout_hash)}, want #{want}"
  )
  check(report.malformed == false, "a refusal by name never sets malformed too (the joint answer)")
  check(
    report.widened == 0 and report.unknown == 0 and report.kind_mismatch == 0 and
      report.clamped == 0 and report.duplicate == 0,
    "REFUSE is total: no counter moves: #{why(report)}"
  )
  # THE DESTINATION IS STILL EVERY FIELD OF A FRESH VALUE, compared as VALUES
  # and never as a struct's slack (§5.9 #17): a BEAM struct has no slack, so the
  # comparison is the value itself against a fresh one.
  check(fresh == fresh_value(), "REFUSE wrote destination values")`
			}
			out, err := runVersionProbe(t, elixirBin, "VOLD_"+r.row, nil, 0,
				filepath.Join(corpus, "new_"+r.row+".bin"), body)
			if err != nil {
				t.Fatalf("OLD-REFUSES-NEW %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- the floor --------------------------------------------------------------
//
// The floor is ONE NUMBER and the lineage is ONE ARRAY, so "retired" is an
// index cut and the operator's two answers stay distinct: below the floor is
// `layout_unsupported` (upgrade the client), outside the lineage is
// `layout_newer` (ship the reader). §5.2, §5.7's three floor rows.

func TestFixedVersioningFloor(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	oldFile := filepath.Join(corpus, "old_floor.bin")
	midFile := filepath.Join(corpus, "mid_floor.bin")

	run := func(t *testing.T, retire int, body string) {
		t.Helper()
		out, err := runVersionProbe(t, elixirBin, "VNEW_floor", []string{"VOLD_floor", "VMID_floor"}, retire,
			oldFile, body)
		if err != nil {
			t.Fatalf("floor: %v\n%s", err, out)
		}
	}

	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		// Nothing retired: the OLDEST file is at the floor and it reads.
		run(t, 0, `  data = File.read!(file())
  {tag, _values, report} = load(data)
  check(tag == :ok and report.malformed == false, "a file AT the floor must read: #{inspect(tag)} #{why(report)}")`)
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		// Entry 0 retired: the floor is 1, and the file one below it refuses
		// layout_unsupported — nothing decoded, no counter moved. Both layout
		// refusals report the file's hash (§5.9 #7).
		run(t, 1, fmt.Sprintf(`  data = File.read!(file())
  <<_::binary-size(8), want::little-unsigned-64, _::binary>> = data
  {tag, why, report} = load(data)
  check(tag == :error and why == :layout_unsupported,
    "a file below the floor owes layout_unsupported: #{inspect({tag, why})}")
  check(report.layout_hash == want, "layout_unsupported reports the file hash too")
  check(report.malformed == false and report.widened == 0 and report.unknown == 0 and report.clamped == 0,
    "REFUSE is total: #{why(report)}")
  mid = File.read!(%s)
  {mtag, _, mreport} = load(mid)
  check(mtag == :ok, "the file AT the raised floor must still read: #{inspect(mtag)} #{why(mreport)}")`,
			elixirString(midFile)))
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		// The floor raised by one: the file that read yesterday refuses today.
		run(t, 2, fmt.Sprintf(`  mid = File.read!(%s)
  {tag, why, _report} = load(mid)
  check(tag == :error and why == :layout_unsupported,
    "the floor raised by one: yesterday file must refuse: #{inspect({tag, why})}")`, elixirString(midFile)))
	})
}

// ---- the hash ---------------------------------------------------------------

func TestFixedVersioningHash(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	old := filepath.Join(corpus, "old_field_append.bin")
	newer := filepath.Join(corpus, "new_field_append.bin")

	probe := func(t *testing.T, body string) {
		t.Helper()
		out, err := runVersionProbe(t, elixirBin, "VNEW_field_append", []string{"VOLD_field_append"}, 0, old, body)
		if err != nil {
			t.Fatalf("hash case: %v\n%s", err, out)
		}
	}

	// hash_unknown: a hash in no lineage refuses layout_newer, reporting the
	// FILE'S hash and nothing else.
	t.Run("hash_unknown", func(t *testing.T) {
		t.Parallel()
		probe(t, `  data = File.read!(file())
  <<head::binary-size(8), _::binary-size(8), rest::binary>> = data
  forged = <<head::binary, 0x0EADBEEFCAFEF00D::little-unsigned-64, rest::binary>>
  {tag, why, report} = load(forged)
  check(tag == :error and why == :layout_newer, "hash_unknown: #{inspect({tag, why})}")
  check(report.layout_hash == 0x0EADBEEFCAFEF00D, "hash_unknown reports the file hash")
  check(report.malformed == false, "hash_unknown is a refusal by name, not malformed")`)
	})

	// hash_known_bytes_differ: a KNOWN hash whose layout bytes differ from the
	// lock's is ONE name, layout_malformed — "a lie about a known version". The
	// seven §1.1 malformations under a known hash all land here, never in a
	// runtime walk (§5.3).
	t.Run("hash_known_bytes_differ", func(t *testing.T) {
		t.Parallel()
		probe(t, `  data = File.read!(file())
  <<head::binary-size(24), b, rest::binary>> = data
  lied = <<head::binary, Bitwise.bxor(b, 0xFF), rest::binary>>
  {tag, why, report} = load(lied)
  check(tag == :error and why == :layout_malformed, "hash_known_bytes_differ: #{inspect({tag, why})}")
  check(report.malformed == false, "a refusal by name never sets malformed too")`)
	})

	// hash_identity: the reader's own hash selects the identity plan.
	t.Run("hash_identity", func(t *testing.T) {
		t.Parallel()
		probe(t, fmt.Sprintf(`  data = File.read!(%s)
  {tag, values, report} = load(data)
  check(tag == :ok and report.malformed == false, "hash_identity: #{inspect(tag)} #{why(report)}")
  check(hd(values).w == 777, "the identity plan lost a value: #{inspect(hd(values).w)}")`, elixirString(newer)))
	})
}

// ---- lineage_merge ----------------------------------------------------------
//
// Two branches append different fields; after the merge BOTH pre-merge files
// read on the merged build (the name-subset rule, bill §8a.1).

func TestFixedVersioningLineageMerge(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	body := fmt.Sprintf(`  for path <- [%s, %s] do
    data = File.read!(path)
    {tag, _values, report} = load(data)
    check(tag == :ok and report.malformed == false,
      "#{path}: both pre-merge files read on the merged build: #{inspect(tag)} #{why(report)}")
  end`, elixirString(filepath.Join(corpus, "a_lineage_merge.bin")),
		elixirString(filepath.Join(corpus, "b_lineage_merge.bin")))
	out, err := runVersionProbe(t, elixirBin, "VNEW_lineage_merge",
		[]string{"VOLD_lineage_merge", "VBRA_lineage_merge", "VBRB_lineage_merge"}, 0,
		filepath.Join(corpus, "old_lineage_merge.bin"), body)
	if err != nil {
		t.Fatalf("lineage_merge: %v\n%s", err, out)
	}
}

// ---- the capacity refusal ---------------------------------------------------
//
// A CONTROL MOVES WITH THE CASE IT WATCHES (§5.9 #39), and `plan_too_large` is
// owed by every leg (§5.9 #45). On the leg's BYTE gate the case is now
// unreachable: every unit there is generated apart, so every lineage is of one
// and every read takes the IDENTITY lane, which has no plan to be too large. Here
// the lock is played, so the reader holds a COMPILED lane for the older entry —
// and a caller's capacity of one is smaller than any real plan.

func TestFixedVersioningPlanTooLarge(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	body := `  data = File.read!(file())
  {tag, why, report} = load(data, plan_capacity: 1)
  check(tag == :error and why == :plan_too_large,
    "a plan past THIS caller's capacity owes plan_too_large: #{inspect({tag, why})}")
  check(report.malformed == false, "a refusal by name never sets malformed too")
  check(report.widened == 0 and report.unknown == 0 and report.clamped == 0,
    "REFUSE is total: no counter moves: #{why(report)}")
  # AND THE SAME FILE READS AT THE BUILD'S OWN CAPACITY, so the refusal is the
  # capacity's and not the file's.
  {ok_tag, _values, ok_report} = load(data)
  check(ok_tag == :ok and ok_report.malformed == false,
    "the same file reads at the declared capacity: #{inspect(ok_tag)} #{why(ok_report)}")`
	out, err := runVersionProbe(t, elixirBin, "VNEW_field_append", []string{"VOLD_field_append"}, 0,
		filepath.Join(corpus, "old_field_append.bin"), body)
	if err != nil {
		t.Fatalf("plan_too_large: %v\n%s", err, out)
	}
}

// ---- text under a union arm -------------------------------------------------
//
// MOVED HERE FROM THE LEG'S BYTE GATE BY §5.6 (the JS leg's
// TestJSFixedVersioningUnionArmText is the precedent, #922). A plan entry for a
// `string(N)` under a union's arm carries TWO INDEPENDENT FACTS — which arm's tag
// guards it, and which flavour of text it is — and a port that spends ONE lane on
// both loses whichever was written second, silently, with no counter and no
// refusal.
//
// UT2 inserts an arm IN THE MIDDLE, so the string moves from arm 2 to arm 3: the
// entry must be guarded by THEIR ordinal while the tag this reader stores is MY
// ordinal, and the flavour is neither number. The file is UT1's own save and UT1's
// ENTRY IS HANDED IN as the lock, which is the only way §5 reads it at all.

func TestFixedVersioningUnionArmText(t *testing.T) {
	fixedCorpus(t) // the leg's gate promises the oracle; this case needs only Elixir
	elixirBin := elixirBinary(t)
	file := filepath.Join(t.TempDir(), "ut1_arm_b.bin")

	// UT1 WRITES ARM `b` AT ORDINAL 2 — its own layout, its own plan.
	write := fmt.Sprintf(`  data =
    T.ut_root_fixed_save([
      %%Tblut1.UtRoot{
        head: 81,
        pick: %%Tblut1.Pick{type: 2, b: %%Tblut1.ArmB{label: "back", m: -12}},
        tail: 88
      }
    ])

  File.write!(%s, data)
  {tag, values, report} = load(data)
  check(tag == :ok, "UT1 reads its own arm-b record: #{inspect(tag)} #{why(report)}")
  v = hd(values)
  check(v.pick.type == 2 and v.pick.b.label == "back" and v.pick.b.m == -12,
    "UT1's identity read of the arm: #{inspect(v.pick)}")`, elixirString(file))
	if out, err := runVersionProbe(t, elixirBin, "UT1", nil, 0, file, write); err != nil {
		t.Fatalf("union_arm_text, UT1's own save: %v\n%s", err, out)
	}

	// AND UT2 READS IT AT ORDINAL 3, through the plan the build compiled from
	// UT1's locked entry.
	read := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "UT2 reads UT1's record through the lineage: #{inspect(tag)} #{why(report)}")
  check(report.malformed == false, "a clean backward read is not malformed: #{why(report)}")
  v = hd(values)
  # THE TAG LANDS MY ORDINAL: arm b is arm 3 here and arm 2 there, and the entry
  # ran under THEIR guard.
  check(v.pick.type == 3, "the arm's tag did not slide to MY ordinal: #{inspect(v.pick.type)}")
  check(v.pick.b.label == "back",
    "the text under the slid arm: #{inspect(v.pick.b.label)} — a shared guard/flavour lane " <>
      "reads a byte string as a WIDE one and lands \"\"")
  check(byte_size(v.pick.b.label) == 4, "the text's length: #{byte_size(v.pick.b.label)}")
  check(v.pick.b.m == -12, "the int32 beside the text: #{inspect(v.pick.b.m)}")
  check(v.head == 81 and v.tail == 88, "the fields either side of the union: #{inspect({v.head, v.tail})}")
  check(report.clamped == 0 and report.unknown == 0,
    "an arm that MOVED is not an event: #{why(report)}")`
	if out, err := runVersionProbe(t, elixirBin, "UT2", []string{"UT1"}, 0, file, read); err != nil {
		t.Fatalf("union_arm_text, UT2 over UT1: %v\n%s", err, out)
	}
}

// ---- the harness ------------------------------------------------------------

func fixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; but `make tables-elixir-versioning` builds the
		// corpus first and sets SCHEMA_REQUIRE_CORPUS=1, so under that target a
		// missing file means the build did not do what the target says it did.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

// elixirBinary is THE LEG'S OWN TOOLCHAIN (§5.9 #18): the Elixir and OTP
// make/elixir.mk pins in dist/, overridable the way the Makefile overrides it.
//
// THREE PLACES, IN THIS ORDER, because the harness SPAWNS the binary and so
// needs a path and never a shell launcher: $ELIXIR when it names one, the
// pinned dist/ tree, and THE ELIXIR ON PATH — which is the CI shape, where
// setup-beam installs the pinned versions on PATH and dist/ is not in the job
// at all (the negative-controls group overrides ELIXIR=elixir and nothing
// else). A bench with no Elixir anywhere still skips, and still FAILS under
// SCHEMA_REQUIRE_CORPUS: the leg's gate is never satisfied by absence.
func elixirBinary(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ELIXIR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		if bin, err := exec.LookPath(env); err == nil {
			return bin
		}
	}
	bin, err := filepath.Abs("../../../dist/elixir-1.20.4/bin/elixir")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	if found, err := exec.LookPath("elixir"); err == nil {
		return found
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no Elixir is on this bench: not at %s and not on PATH", bin)
	}
	t.Skip("no Elixir: unpack it in dist/ or put it on PATH — see make/elixir.mk")
	return ""
}

// brackets is §5.9 #32's value oracle: per row the `lead` and `trail` the corpus
// brackets it with, so a mislaid size moves a neighbour and the row says so by
// name. THE MANIFEST is read when the tree carries one
// (`build/fixedform-corpus/manifest.txt`, §5.7 step 1's one line per row), and
// where it does not the values the manifest would carry are taken FROM THE
// CORPUS BYTES — which is where the manifest itself reads them: the lead is the
// first four bytes of the first record's body and the trail the last four.
func brackets(t *testing.T, corpus, row string, bodyBytes int64) (uint32, uint32) {
	t.Helper()
	if lead, trail, ok := manifestBrackets(t, corpus, "old_"+row+".bin"); ok {
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

// manifestBrackets reads ONE FILE'S row out of the corpus manifest, which is
// the VALUE ORACLE and not the bytes under test (§5.9 #32).
//
// A LINE IS `file=<name> row=<row> side=<old|new|none> root=<R> records=<n>
// values=<k>=<v>,...` — the FILE is the first field and the key, because one
// row has two sides and they carry different values; the brackets live inside
// `values=` spelled `r0.lead` and `r0.trail`; and EVERY NUMBER IS DECIMAL.
// Matching the row against the first field, or reading the brackets as hex,
// is how this branch was dead: it never fired and every row read its oracle
// from the very .bin it was asserting.
//
// The manifest is not tracked, so `ok` false is "the tree has no manifest" and
// the caller falls back to the corpus bytes — but a manifest that IS there and
// does not carry this file is a FAILURE BY NAME, never a silent fallback.
func manifestBrackets(t *testing.T, corpus, file string) (uint32, uint32, bool) {
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
		values, ok := manifestRow(line, file)
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

// manifestRow answers one line's `values=` map when its `file=` is the one
// asked for. The values are `<path>=<number>` pairs separated by commas, and a
// pair whose value is not a number — a quoted string, a bool — is not a
// bracket and is skipped.
func manifestRow(line, file string) (map[string]uint64, bool) {
	var values string
	named := false
	for _, field := range strings.Fields(line) {
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
	for _, pair := range strings.Split(values, ",") {
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

func elixirString(s string) string { return fmt.Sprintf("%q", s) }

func versionSchema(t *testing.T, base string) *ir.Unit {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", base+".schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
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

// versionRoot is the fixed table a corpus file was written FROM: the OUTER
// table, the one no other fixed table of the unit names by value (§5.9 #9).
func versionRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := fixedRoots(u)
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

// runVersionProbe generates the READER's Elixir with the OLDER units' locked
// entries handed to it as its lineage — oldest first, the reader's own layout
// last — marks the first `retire` entries retired (which is what moves the
// floor), writes ONE GENERATED PROBE beside it, and runs it with the leg's own
// toolchain. A probe is generated, never hand-written, so a row and its
// negative control cost the same (§5.9 #18).
func runVersionProbe(t *testing.T, elixirBin, reader string, older []string, retire int, file, body string) (string, error) {
	t.Helper()
	u := versionSchema(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, base := range older {
		o := versionSchema(t, base)
		for _, st := range fixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	// THE PACKET LIBRARY TOO: a row's unions, enums and flags lower to modules
	// there, and the fixed surface names them the way a consumer does.
	files, err := elixir.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("generate: %v", err)
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
	snake := ir.RustSnake(st.Name)
	mod := ir.GoExportName(u.Package) + "." + ir.GoExportName(fileOf(t, u, st.Name)) + FixedModuleSuffix
	probe := fmt.Sprintf(`# GENERATED BY internal/codegen/elixirtable/fixedversioning_test.go — one probe per
# row per COLUMN (docs/FIXED-FORM-ALGORITHM.md §5.9 #18).
defmodule Probe do
  import Bitwise
  alias %[1]s, as: T

  def file, do: %[3]s
  def prefill, do: T.%[2]s_fixed_prefill()
  def fresh_value, do: %%%[4]s{}

  def load(data), do: T.%[2]s_fixed_load(data)

  # THE OPTIONS ARE PART OF THE CONTRACT (§5.9 #5, #16): plan_capacity: is a
  # CAPACITY DECLARATION checked against the entry the lineage selected, so the
  # capacity refusal needs a probe that can pass one.
  def load(data, opts), do: T.%[2]s_fixed_load(data, opts)

  def why(r), do: inspect(r)

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  # THE BRACKETS ARE THE CORPUS MANIFEST'S NUMBERS (§5.9 #32): a lead and a
  # trail bracket every row the dump writes them for, so a mislaid size moves
  # a neighbour and the row says so by name.
  def leadtrail(v, lead, trail) do
    check(v.lead == lead and v.trail == trail,
      "the row moved a neighbour: #{inspect(v.lead)} #{inspect(v.trail)}")
  end

  def run do
%[5]s
    if Process.get(:failed) do
      System.halt(1)
    else
      IO.puts("ok")
    end
  end
end

Probe.run()
`, mod, snake, elixirString(file), ir.GoExportName(u.Package)+"."+st.Name, indentBody(body))
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{}
	for _, n := range sortedRuntimeFirst(names) {
		args = append(args, "-r", n)
	}
	args = append(args, "probe.exs")
	cmd := exec.Command(elixirBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// otpBin is the OTP the Makefile pins beside the Elixir it pins: `elixir` is a
// shell script that needs `erl` on PATH. It resolves like [elixirBinary] —
// $ERL_BIN, then the pinned dist/ tree, then the `erl` already on PATH, which
// is where CI's OTP is — and what it returns is PREPENDED to PATH rather than
// replacing it, so a directory that turns out to hold no `erl` costs nothing.
func otpBin(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ERL_BIN"); env != "" {
		return env
	}
	p, err := filepath.Abs("../../../dist/otp-29.0.5/bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, "erl")); err == nil {
		return p
	}
	if found, err := exec.LookPath("erl"); err == nil {
		return filepath.Dir(found)
	}
	return p
}

// sortedRuntimeFirst puts FixedRuntime.ex first and BuildVersion.ex next: a
// module a later one calls at COMPILE time must already be loaded, and the
// lineage's plans are built at module load (§5.9 #3). A union's or an enum's
// own struct lowers to a PACKET module, so those come before the fixed surface
// that names them — and the order is sorted rather than a map's, because a map's
// iteration order would make a row pass or fail by luck.
func sortedRuntimeFirst(names []string) []string {
	var first, packet, fixed []string
	for _, n := range names {
		switch {
		case n == "FixedRuntime.ex" || n == "BuildVersion.ex":
			first = append(first, n)
		case strings.HasSuffix(n, FixedModuleSuffix+".ex"):
			fixed = append(fixed, n)
		default:
			packet = append(packet, n)
		}
	}
	sort.Strings(first)
	sort.Strings(packet)
	sort.Strings(fixed)
	return append(append(first, packet...), fixed...)
}

// fileOf is the schema FILE that declares `name`: the Elixir fixed surface is
// one module per file, named for it.
func fileOf(t *testing.T, u *ir.Unit, name string) string {
	t.Helper()
	if base, ok := u.DeclFile[name]; ok {
		return base
	}
	t.Fatalf("no declaring file for %s", name)
	return ""
}

func indentBody(body string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// THE MANIFEST BRANCH IS LIVE, AND IT IS THE ORACLE (§5.9 #32). It was dead:
// the row was matched against the line's FIRST field where the manifest's first
// field is `file=<name>`, and the brackets were parsed as HEX where the manifest
// writes decimal — so every row fell through and read its oracle out of the very
// `.bin` it was asserting, which is no assertion at all.
//
// This holds the two against each other: the manifest must carry every
// NEW-READS-OLD row, and its brackets must be the bytes'. A disagreement is a
// dump that moved, which is the thing the brackets exist to catch.
func TestFixedVersioningReadsTheManifest(t *testing.T) {
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
		fired++
		data, err := os.ReadFile(filepath.Join(corpus, "old_"+r.row+".bin"))
		if err != nil {
			t.Fatalf("%s: %v", r.row, err)
		}
		body := fixedTypeBytes(versionRoot(t, versionSchema(t, "VOLD_"+r.row)))
		at := 20 + int64(binary.LittleEndian.Uint32(data[16:20])) + 8
		wantLead := binary.LittleEndian.Uint32(data[at : at+4])
		wantTrail := binary.LittleEndian.Uint32(data[at+body-4 : at+body])
		if lead != wantLead || trail != wantTrail {
			t.Fatalf("%s: the manifest says lead=0x%08X trail=0x%08X, the bytes say 0x%08X 0x%08X",
				r.row, lead, trail, wantLead, wantTrail)
		}
	}
	// A BRANCH THAT NEVER FIRED IS THE BUG THIS TEST EXISTS FOR.
	if fired == 0 {
		t.Fatal("not one row read its brackets from the manifest: the branch is dead again")
	}
}
