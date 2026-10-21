package elixirtable

// THE T12 ORDINALS-AND-ARMS ROADMAP SUITE ON THE ELIXIR LEG (docs/roadmap.sexp
// node `fixed-tables`: interoperability/elixir/R27,
// union-field-arms/elixir/valid-data, and payload-free-arms/elixir/valid-data;
// docs/SPEC-TABLES.md §2.6, §3.4; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.1, §5.7
// step 1, §5.9 #32). One subtest per task id, table-driven, t.Parallel() first.

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapElixirT12OrdinalsArms(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id  string
		run func(t *testing.T)
	}{
		{id: "elixir/R27", run: t12R27},
		{id: "union-field-arms/elixir/valid-data", run: t12UnionFieldArms},
		{id: "payload-free-arms/elixir/valid-data", run: t12PayloadFreeArms},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.run(t)
		})
	}
}

// ---- interoperability/elixir/R27 --------------------------------------------

// t12R27 holds R27 to the corpus manifest (`build/fixedform-corpus/manifest.txt`,
// §5.7 step 1 and §5.9 #32) as the single value oracle: under
// SCHEMA_REQUIRE_CORPUS=1 a missing corpus or manifest is a test failure, never
// a skip; every versioning row's brackets in the manifest match the corpus
// bytes; and the Elixir fixed-form reader loads `fx1.bin` to the exact values
// the manifest records for `file=fx1.bin`.
func t12R27(t *testing.T) {
	corpus := fixedCorpus(t)
	manifestPath := filepath.Join(corpus, "manifest.txt")
	if _, err := os.Stat(manifestPath); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the corpus manifest is not in %s: %v", manifestPath, err)
		}
		t.Skip("the tree carries no corpus manifest: make tables-fixedform-corpus")
	}

	fired := 0
	for _, r := range versionRows {
		lead, trail, ok := manifestBrackets(t, corpus, "old_"+r.row+".bin")
		if !ok {
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
	if fired == 0 {
		t.Fatal("R27: not one row read its brackets from build/fixedform-corpus/manifest.txt")
	}

	f, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	want := map[string]int64{}
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "file=fx1.bin ") {
			continue
		}
		found = true
		idx := strings.Index(line, "values=")
		if idx < 0 {
			t.Fatal("R27: file=fx1.bin line in manifest.txt carries no values=")
		}
		for _, kv := range strings.Split(line[idx+len("values="):], ",") {
			eq := strings.Index(kv, "=")
			if eq <= 0 {
				continue
			}
			if v, err := strconv.ParseInt(kv[eq+1:], 10, 64); err == nil {
				want[kv[:eq]] = v
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("R27: the manifest carries no line for fx1.bin")
	}
	for _, k := range []string{"r0.keep", "r0.narrow", "r0.nested.a", "r0.label_length", "r0.marks_count", "r1.keep"} {
		if _, ok := want[k]; !ok {
			t.Fatalf("R27: the manifest's fx1.bin line does not declare %s", k)
		}
	}

	elixirBin := elixirBinary(t)
	probeBody := fmt.Sprintf(`  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok and report.malformed == false, "fx1.bin load refused: #{inspect({tag, report})}")
  check(length(values) == 2, "fx1.bin record count #{length(values)}, want 2")
  [r0, r1] = values
  check(r0.keep == %d, "r0.keep = #{r0.keep}, manifest wants %d")
  check(r0.narrow == %d, "r0.narrow = #{r0.narrow}, manifest wants %d")
  check(r0.nested.a == %d, "r0.nested.a = #{r0.nested.a}, manifest wants %d")
  check(byte_size(r0.label) == %d, "r0.label_length = #{byte_size(r0.label)}, manifest wants %d")
  check(length(r0.marks) == %d, "r0.marks_count = #{length(r0.marks)}, manifest wants %d")
  check(r1.keep == %d, "r1.keep = #{r1.keep}, manifest wants %d")`,
		want["r0.keep"], want["r0.keep"],
		want["r0.narrow"], want["r0.narrow"],
		want["r0.nested.a"], want["r0.nested.a"],
		want["r0.label_length"], want["r0.label_length"],
		want["r0.marks_count"], want["r0.marks_count"],
		want["r1.keep"], want["r1.keep"])
	if out, err := runVersionProbe(t, elixirBin, "FX1", nil, 0, filepath.Join(corpus, "fx1.bin"), probeBody); err != nil {
		t.Fatalf("R27 fx1.bin probe: %v\n%s", err, out)
	}
}

// ---- union-field-arms/elixir/valid-data -------------------------------------

// t12UnionFieldArms holds union-field-arms/elixir/valid-data to §2.6 and §3.4:
// a union arm holding a scalar, a string(N), a bytes(N), a counted array or a
// fixed array directly lowers to its field storage on the Elixir union struct
// and round-trips through the fixed form's writer, identity plan and compiled
// plan at non-default and boundary values.
func t12UnionFieldArms(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain (it compiles and runs the generated module)")
	oldSchema := `package probe

union Pick
{
    num   int32
    label string(8)
    blob  bytes(4)
    cells [..3]int32
}

fixed table Root
{
    pick Pick
    tail int32
}
`
	newSchema := `package probe

union Pick
{
    num   int32
    label string(8)
    blob  bytes(4)
    cells [..3]int32
    pair  [2]int32
}

fixed table Root
{
    pick Pick
    tail int32
}
`
	uOld := unitFrom(t, oldSchema)
	oldRoot := uOld.Tables["Root"]
	if oldRoot == nil {
		t.Fatal("missing Root in oldSchema")
	}
	oldLayoutHex := strings.ToUpper(hex.EncodeToString(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(oldRoot))))

	uNew := unitFrom(t, newSchema)
	body := strings.ReplaceAll(`  v0 = %Probe.Root{
    pick: %Probe.Pick{type: Probe.PickType.num(), num: -2_147_483_648},
    tail: 1
  }
  v1 = %Probe.Root{
    pick: %Probe.Pick{type: Probe.PickType.label(), label: "hello"},
    tail: 2
  }
  v2 = %Probe.Root{
    pick: %Probe.Pick{type: Probe.PickType.blob(), blob: <<0, 1, 2, 3>>},
    tail: 3
  }
  v3 = %Probe.Root{
    pick: %Probe.Pick{type: Probe.PickType.cells(), cells: [-2_147_483_648, 0, 2_147_483_647]},
    tail: 4
  }
  v4 = %Probe.Root{
    pick: %Probe.Pick{type: Probe.PickType.pair(), pair: [-5, 5]},
    tail: 5
  }
  saved = @@SAVE@@([v0, v1, v2, v3, v4])
  {:ok, [b0, b1, b2, b3, b4], rep} = @@LOAD@@(saved)
  check(rep.malformed == false and rep.clamped == 0, "union-field-arms load report: #{inspect(rep)}")
  check(b0 == v0, "scalar arm did not round-trip: #{inspect(b0)} vs #{inspect(v0)}")
  check(b1 == v1, "text arm did not round-trip: #{inspect(b1)} vs #{inspect(v1)}")
  check(b2 == v2, "bytes arm did not round-trip: #{inspect(b2)} vs #{inspect(v2)}")
  check(b3 == v3, "counted-array arm did not round-trip: #{inspect(b3)} vs #{inspect(v3)}")
  check(b4 == v4, "fixed-array arm did not round-trip: #{inspect(b4)} vs #{inspect(v4)}")

  # Compiled-plan path: compile the 4-arm writer layout against the 5-arm reader
  # destination table and run each record body through R.run + root_fixed_decode.
  {:ok, theirs} = R.parse_layout(Base.decode16!("@@OLD_LAYOUT@@"))
  mine = R.my_layout(T, T.root_fixed_layout())
  {:ok, plan, _made, _rep} = R.compile(theirs, mine, T.root_fixed_dst(), 4096, R.report())
  for {v, idx} <- Enum.with_index([v0, v1, v2, v3]) do
    <<_hash::little-unsigned-64, rec_body::binary>> = T.root_fixed_record(v)
    {image, r_run} = R.run(plan, rec_body, T.root_fixed_prefill(), R.report())
    {decoded, c, m} = T.root_fixed_decode(image, 0, false)
    check(r_run.malformed == false and c == 0 and m == false, "compiled plan arm #{idx} report: #{inspect({r_run, c, m})}")
    check(decoded == v, "compiled plan arm #{idx} did not round-trip: #{inspect(decoded)} vs #{inspect(v)}")
  end`, "@@OLD_LAYOUT@@", oldLayoutHex)
	t09Probe(t, uNew, "Root", body)
}

// ---- payload-free-arms/elixir/valid-data ------------------------------------

// t12PayloadFreeArms holds payload-free-arms/elixir/valid-data to §2.6 and
// FIXED-FORM-ALGORITHM.md §1/§3.1: a payload-free arm has no storage in the
// union struct, writes its tag ordinal with zero-filled widest-arm slack, and
// reads back with its tag ordinal intact and `clamped == 0` (never falling into
// the out-of-range `_ ->` clamp clause).
func t12PayloadFreeArms(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain (it compiles and runs the generated module)")
	u := unitFrom(t, `package probe

union Pick
{
    num  int32
    ping
}

fixed table Root
{
    pick Pick
    tail int32
}
`)
	t09Probe(t, u, "Root", `  v0 = %Probe.Root{
    pick: %Probe.Pick{type: Probe.PickType.ping()},
    tail: 5
  }
  v1 = %Probe.Root{
    pick: %Probe.Pick{type: Probe.PickType.num(), num: 2_147_483_647},
    tail: 6
  }
  saved = @@SAVE@@([v0, v1])
  {:ok, [b0, b1], rep} = @@LOAD@@(saved)
  check(rep.malformed == false and rep.clamped == 0, "payload-free-arms load report: #{inspect(rep)}")
  check(b0.pick.type == Probe.PickType.ping() and b0.tail == 5,
    "payload-free arm ping did not round-trip: #{inspect(b0)}")
  check(b1 == v1, "scalar arm beside payload-free arm did not round-trip: #{inspect(b1)}")`)
}
