package elixirtable

// FULL-WIDTH ENUM AND UNION ORDINALS ON THE ELIXIR LEG (docs/roadmap.sexp
// node `fixed-tables`, leaf elixir/R31; docs/FIXED-FORM-ALGORITHM.md §4.1,
// §5.2, §5.8 rows 5, 7 and 14). The same fixture as the Go and C legs: a
// writer with 300 enum variants and 300 union arms, a reader with a subset,
// both guard lanes (the guard chain) stamped above MaxUint32, an 8-byte
// ordinal through the 64-bit temporary, and a remap past the uint16 length
// word refused by name as :plan_too_large.
//
// Assertions on the emitted runtime text are unit tests. The generated-unit
// probe is a functional test: slowtest.Gate first, then the leg's toolchain
// the way this package's roadmap tests get it (elixirBinary, in the probe
// runner).

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func t16ENames(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

func t16EFixture(many, pick, outer []string) string {
	var b strings.Builder
	b.WriteString("package probe\n\ntype Cell { n int32 }\n\nenum Many { ")
	b.WriteString(strings.Join(many, ", "))
	b.WriteString(" }\n\nunion Pick {\n")
	for _, a := range pick {
		fmt.Fprintf(&b, "    %s Cell\n", a)
	}
	b.WriteString("}\n\ntype Holder { inner Inner }\n\nunion Inner {\n    x Cell\n    y Cell\n}\n\nunion Outer {\n")
	for _, a := range outer {
		if a == "a299" {
			fmt.Fprintf(&b, "    %s Holder\n", a)
		} else {
			fmt.Fprintf(&b, "    %s Cell\n", a)
		}
	}
	b.WriteString("}\n\nfixed table Root {\n    many Many\n    pick Pick\n    top Outer\n    seq int32\n}\n")
	return b.String()
}

func t16EWriterSchema() string {
	return t16EFixture(t16ENames("v", 300), t16ENames("a", 300), t16ENames("a", 300))
}

func t16EReaderSchema() string {
	return t16EFixture([]string{"v0", "v1", "v299"}, []string{"a0", "a299"}, []string{"a0", "a299"})
}

func t16EOverflowSchema() string {
	var b strings.Builder
	b.WriteString("package probe\n\nenum Many { ")
	b.WriteString(strings.Join(t16ENames("v", 65536), ", "))
	b.WriteString(" }\n\nfixed table Root {\n    many Many\n    seq int32\n}\n")
	return b.String()
}

func t16ELayoutHex(t *testing.T, schema string) string {
	t.Helper()
	u := unitFrom(t, schema)
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("the fixture declares no fixed table Root")
	}
	return strings.ToUpper(hex.EncodeToString(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))))
}

func TestFixedRoadmapElixirT16OrdinalLanes(t *testing.T) {
	t.Parallel()
	t.Run("elixir/R31", func(t *testing.T) {
		t.Parallel()
		t16ER31(t)
	})
}

func t16ER31(t *testing.T) {
	_, runtime := t06EmitReader(t)

	// THE REMAP CARRIES ITS LENGTH IN SLOT 0 (§5.2: table[0] is n, the
	// writer's variant count, then table[1..n]), and the ordinal reads the
	// length word as its bound — never the tuple's size alone.
	t06Has(t, runtime, "List.to_tuple([their_variants | remap])",
		"R31 the remap table's slot 0 is the writer's variant count (FIXED-FORM-ALGORITHM.md §5.2)")
	t06Has(t, runtime, "raw <= elem(remap, 0)",
		"R31 the ordinal's bound is the remap length word (FIXED-FORM-ALGORITHM.md §5.2)")
	// THE SECOND GUARD LANE (§4.1): an arm inside an arm is guarded by the
	// outer tag too, the chain carrying each tag whole.
	t06Has(t, runtime, "tag ++ [j + 1]",
		"R31 the outer tag joins the guard chain whole (FIXED-FORM-ALGORITHM.md §4.1)")
	// A WRITER PAST THE LENGTH WORD REFUSES BY NAME (§5.8 row 14).
	t06Has(t, runtime, ":plan_too_large",
		"R31 a variant count past the uint16 length word refuses as :plan_too_large (FIXED-FORM-ALGORITHM.md §5.2)")

	slowtest.Gate(t, "the Elixir toolchain (it compiles and runs the generated unit)")
	writerHex := t16ELayoutHex(t, t16EWriterSchema())
	overflowHex := t16ELayoutHex(t, t16EOverflowSchema())
	body := make([]byte, 19)
	body[0], body[1] = 0x2C, 0x01
	body[2], body[3] = 0x2C, 0x01
	body[4], body[5] = 0x09, 0x03
	body[8], body[9] = 0x2C, 0x01
	body[10] = 2
	body[11], body[12] = 0x78, 0x03
	body[15] = 9
	bodyHex := strings.ToUpper(hex.EncodeToString(body))

	probe := strings.ReplaceAll(t16EProbeBody, "@WRITER@", writerHex)
	probe = strings.ReplaceAll(probe, "@BODY@", bodyHex)
	t08RunProbe(t, t16EReaderSchema(), probe)

	over := strings.ReplaceAll(`  {:ok, theirs} = R.parse_layout(Base.decode16!("@OVER@"))
  mine = R.my_layout(T, T.root_fixed_layout())
  res = R.compile(theirs, mine, T.root_fixed_dst(), 8192, R.report())
  check(match?({:error, :plan_too_large, _}, res), "a writer past the uint16 length word must refuse :plan_too_large, got: #{inspect(res)}")`, "@OVER@", overflowHex)
	t08RunProbe(t, t16EReaderSchema(), over)
}

// t06EmitReader emits the t16 reader and returns its module and runtime.
func t06EmitReader(t *testing.T) (string, string) {
	t.Helper()
	files, err := Generate(unitFrom(t, t16EReaderSchema()))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var module, runtime string
	for name, data := range files {
		switch name {
		case "ProbeFixed.ex":
			module = string(data)
		case FixedRuntimeModule + ".ex":
			runtime = string(data)
		}
	}
	if runtime == "" {
		t.Fatal("no " + FixedRuntimeModule + ".ex was emitted")
	}
	return module, runtime
}

const t16EProbeBody = `  {:ok, theirs} = R.parse_layout(Base.decode16!("@WRITER@"))
  mine = R.my_layout(T, T.root_fixed_layout())
  {:ok, plan, _made, _rep} = R.compile(theirs, mine, T.root_fixed_dst(), 8192, R.report())
  ord = Enum.find(plan, fn e -> match?({:ordinal, _, _, _, _, _}, e) end)
  check(ord != nil, "the compiled plan carries no ordinal entry")
  {:ordinal, _, _, _, _, remap} = ord
  check(elem(remap, 0) == 300, "the remap length word is #{elem(remap, 0)}, want the writer's 300")
  check(elem(remap, 300) == 3, "remap[300] is #{elem(remap, 300)}, want the reader ordinal 3")
  body = Base.decode16!("@BODY@")
  {image, _r2} = R.run(plan, body, T.root_fixed_prefill(), R.report())
  <<many, picktag, payload::little-unsigned-32, toptag, innertag, innerpayload::little-unsigned-32, seq::little-unsigned-32>> = image
  check(many == 3, "many landed #{many}, want the reader ordinal 3 of writer variant 300")
  check(picktag == 2 and payload == 777, "pick landed tag #{picktag} payload #{payload}, want arm 2 / 777")
  check(toptag == 2 and innertag == 2 and innerpayload == 888, "nested landed #{toptag}/#{innertag}/#{innerpayload}, want 2/2/888")
  check(seq == 9, "seq landed #{seq}, want 9")
  <<_::binary-size(2), rest::binary>> = body
  {fimage, _r3} = R.run(plan, <<301::little-unsigned-16>> <> rest, T.root_fixed_prefill(), R.report())
  check(:binary.at(fimage, 0) == 0, "forged ordinal 301 must land None")
  # BOTH GUARD LANES ABOVE MaxUint32 (§4.1): the copy fires only when both
  # 8-byte tags equal 0x1_0000_0001.
  gsrc = <<4294967297::little-unsigned-64, 4294967297::little-unsigned-64, 7, 7, 7, 7, 0, 0, 0, 0>>
  gplan = [{:guard, 0, 4294967297, 8, {:guard, 8, 4294967297, 8, {:copy, 16, 0, 4}}}]
  {gimage, _r4} = R.run(gplan, gsrc, <<0, 0, 0, 0>>, R.report())
  check(gimage == <<7, 7, 7, 7>>, "the full-width guard lanes did not fire together: #{inspect(gimage)}")
  # THE 64-BIT ORDINAL TEMPORARY (§4.5) OVER THE LENGTH-WORD REMAP (§5.2).
  oplan = [{:ordinal, 0, 0, 8, 8, {3, 1, 2, 3}}]
  {oimage, _r5} = R.run(oplan, <<4294967297::little-unsigned-64>>, <<0::little-unsigned-64>>, R.report())
  check(oimage == <<0::little-unsigned-64>>, "the 8-byte ordinal 0x1_0000_0001 must land None, got: #{inspect(oimage)}")
  {oimage2, _r6} = R.run(oplan, <<70000::little-unsigned-64>>, <<0::little-unsigned-64>>, R.report())
  check(oimage2 == <<0::little-unsigned-64>>, "the 8-byte ordinal 70000 (past 65535) must land None")
  {oimage3, _r7} = R.run(oplan, <<2::little-unsigned-64>>, <<0::little-unsigned-64>>, R.report())
  check(oimage3 == <<2::little-unsigned-64>>, "the 8-byte ordinal 2 must land 2")`
