package elixirtable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedlimit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FIXED FORM'S HARD LIMITS (docs/SPEC-TABLES.md §3.4's fixed_max_depth and
// fixed_record_bound), the shared vectors every leg reads. The NEW reader holds
// the OLD layout as a lineage entry, so the plan it reads the AT-limit file
// with is compiled from a layout that passed the bound: the file reads whole,
// and a reader whose bound is one short refuses it. The PAST-limit file's
// layout is forged under the OLD hash and refused by name before any record is
// touched; the layout bytes on their own name the bound itself. Every answer is
// limits.txt's, which the C++ reference wrote from its own reader. A string
// rides as a binary on this leg, so its used length is the binary's size.

func elixirLimitCounters(v fixedlimit.Vector) string {
	return fmt.Sprintf("report.unknown == %d and report.kind_mismatch == %d and report.widened == %d and report.clamped == %d and report.duplicate == %d and (if report.malformed, do: 1, else: 0) == %d",
		v.Counters["unknown"], v.Counters["kind_mismatch"], v.Counters["widened"], v.Counters["clamped"], v.Counters["duplicate"], v.Counters["malformed"])
}

// elixirLimitParse reads the file's layout bytes through the reader's own
// parser, which answers "ok" or the bound's name.
const elixirLimitParse = `  <<_::binary-size(16), length::little-unsigned-32, rest::binary>> = data
  parsed =
    case R.parse_layout(binary_part(rest, 0, length)) do
      {:ok, _} -> "ok"
      {:error, why} -> Atom.to_string(why)
    end
`

func elixirLimitRun(t *testing.T, row, at, over string, checks func(fixedlimit.Vector) string) {
	t.Helper()
	slowtest.Gate(t, "elixir (it runs the generated NEW reader over the limit vectors)")
	corpus := fixedCorpus(t)
	a, err := fixedlimit.Read(corpus, at)
	if err != nil {
		t.Fatal(err)
	}
	o, err := fixedlimit.Read(corpus, over)
	if err != nil {
		t.Fatal(err)
	}
	if a.Refused() || !o.Refused() {
		t.Fatalf("limits.txt: %s must read and %s must refuse: %+v %+v", at, over, a, o)
	}
	elixirBin := elixirBinary(t)
	alias := "  alias " + ir.GoExportName(versionSchema(t, "VNEW_"+row).Package) + ".FixedRuntime, as: R\n"
	atBody := alias + `  data = File.read!(file())
  {tag, values, report} = load(data)
` + elixirLimitParse + fmt.Sprintf(`  check(tag == :ok and length(values) == 1, "%[1]s: the at-limit file reads one record: #{inspect(tag)} #{why(report)}")
  check(%[2]s, "%[1]s: a counter moved: #{why(report)}")
  v = hd(values)
%[3]s  check(parsed == %[4]q, "%[1]s: the layout parses as #{parsed}, want %[4]s")`, at, elixirLimitCounters(a), checks(a), a.Parse)
	overBody := alias + `  data = File.read!(file())
  {tag, reason, report} = load(data)
` + elixirLimitParse + fmt.Sprintf(`  check(tag == :error and reason == :%[2]s, "%[1]s: #{inspect(tag)} #{inspect(reason)}, want refused %[2]s")
  check(%[3]s, "%[1]s: a refusal moved a counter: #{why(report)}")
  check(parsed == %[4]q, "%[1]s: the layout parses as #{parsed}, want %[4]s")`, over, o.Reason, elixirLimitCounters(o), o.Parse)
	for _, c := range []struct{ file, body string }{{at, atBody}, {over, overBody}} {
		out, err := runVersionProbe(t, elixirBin, "VNEW_"+row, []string{"VOLD_" + row}, 0, filepath.Join(corpus, c.file), c.body)
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.file, err, out)
		}
		t.Logf("%s: %s", c.file, out)
	}
}

func elixirLimitCheck(t *testing.T, a fixedlimit.Vector, path, expr string) string {
	t.Helper()
	want, err := a.Int(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("  check(%[1]s == %[2]d, \"%[3]s landed #{inspect(%[1]s)}, want %[2]d\")\n", expr, want, path)
}

func TestFixedLimitDepth(t *testing.T) {
	elixirLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, func(a fixedlimit.Vector) string {
		return elixirLimitCheck(t, a, "r0.lead", "v.lead") +
			elixirLimitCheck(t, a, fixedlimit.DeepPath(), "v"+strings.Repeat(".d", 63)+".v") +
			elixirLimitCheck(t, a, "r0.trail", "v.trail")
	})
}

func TestFixedLimitRecord(t *testing.T) {
	elixirLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, func(a fixedlimit.Vector) string {
		return elixirLimitCheck(t, a, "r0.lead", "v.lead") +
			elixirLimitCheck(t, a, "r0.v", "v.v") +
			elixirLimitCheck(t, a, "r0.text_length", "byte_size(v.text)") +
			elixirLimitCheck(t, a, "r0.text[0]", ":binary.at(v.text, 0)") +
			elixirLimitCheck(t, a, "r0.text[32761]", ":binary.at(v.text, 32761)") +
			elixirLimitCheck(t, a, "r0.text[65519]", ":binary.at(v.text, 65519)") +
			elixirLimitCheck(t, a, "r0.trail", "v.trail")
	})
}
