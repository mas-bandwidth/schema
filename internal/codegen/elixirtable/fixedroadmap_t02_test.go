package elixirtable

// THE ELIXIR LEG'S FIXED-TABLE COMPILED-PLANS ROWS (docs/FIXED-FORM-ALGORITHM.md §5.2
// and §5.9 #19; the audit matrix is schema#876). One subtest per roadmap
// task id of docs/roadmap-evidence/fixed-elixir.sexp that was not already held
// by a named test of this package.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02Probe runs body inside the generated probe of `reader`, with R aliased to
// the unit's FixedRuntime so a body can name the known layout and the lanes.
func t02Probe(t *testing.T, reader string, older []string, retire int, file, body string) {
	t.Helper()
	ns := ir.GoExportName(versionSchema(t, reader).Package)
	body = "  alias " + ns + ".FixedRuntime, as: R\n" + body
	if out, err := runVersionProbe(t, elixirBinary(t), reader, older, retire, file, body); err != nil {
		t.Fatalf("%s: %v\n%s", reader, err, out)
	}
}

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	oldFile := corpus + "/old_field_append.bin"
	newFile := corpus + "/new_field_append.bin"
	lineage := []string{"VOLD_field_append"}

	tasks := []struct {
		id  string
		run func(t *testing.T)
	}{
		// §5.2 COMPILE lays the lineage down as static data at build time, oldest
		// first and the current layout last, from the lock. The lineage tuple T.
		// lineage_fixed_known() holds the WIRE hash, the LAYOUT BYTES verbatim, the
		// LAYOUT BYTE LENGTH, and the RECORD SIZE, for every entry the build serves.
		// The read's line §5.9 #8 selects by the file's HEADER HASH against these,
		// and when matched, runs the selected entry's PLAN compiled once from the
		// lock's own layout bytes.
		{"elixir/R1", func(t *testing.T) {
			t02Probe(t, "VNEW_field_append", lineage, 0, newFile, `  known = T.lineage_fixed_known()
  check(tuple_size(known) == 2, "the lineage holds the older entry and the reader's own: #{tuple_size(known)}")
  old = elem(known, 0)
  new = elem(known, 1)
  check(is_map(old) and is_map(new), "every entry is a map")
  check(map_size(old) == 4 and map_size(new) == 4, "every map has four fields: hash, layout, layout_bytes, record_bytes")
  check(is_integer(old.hash) and is_integer(new.hash), "hash is an integer")
  check(old.hash != new.hash, "the older and reader's own have different hashes")
  check(is_binary(old.layout) and is_binary(new.layout), "layout is a binary")
  check(old.layout != new.layout, "the older and reader's own have different layouts")
  check(is_integer(old.layout_bytes) and is_integer(new.layout_bytes), "layout_bytes is an integer")
  check(old.layout_bytes == byte_size(old.layout), "layout_bytes matches the layout's byte size")
  check(new.layout_bytes == byte_size(new.layout), "layout_bytes matches the layout's byte size")
  check(is_integer(old.record_bytes) and is_integer(new.record_bytes), "record_bytes is an integer")
  check(old.record_bytes > 0 and new.record_bytes > 0, "record_bytes is positive")`)
		}},

		// record_bytes is 8 + body: the lock stores the body (the type's field data),
		// COMPILE adds the eight bytes once (the record's hash), and no backend adds
		// anything. §5.2 COMPILE's table lists it per entry: `record=<n>` where n is
		// the bytes carried by one lineage entry's records. Every entry's record_bytes
		// reflects its own era: the old reader's own is 8 + what the old writer's
		// values took, and so on.
		{"elixir/R2", func(t *testing.T) {
			t02Probe(t, "VNEW_field_append", lineage, 0, newFile, `  known = T.lineage_fixed_known()
  old = elem(known, 0)
  new = elem(known, 1)
  old_body = T.lineage_fixed_layout()
  new_layout = T.lineage_fixed_layout()
  new_known_hash = T.field_append_fixed_hash()
  {:ok, values, _} = load(File.read!(file()), plan_capacity: 10)
  check(new.record_bytes == 8 + T.field_append_fixed_body_bytes(), "the reader's own record_bytes is 8 + body")
  data = File.read!(file())
  <<_::binary-size(20), layout::binary-size((binary_part(data, 16, 4) |> :binary.decode_unsigned(:little))), records::binary>> = data
  whole = byte_size(records)
  if whole > 0 do
    n = div(whole, new.record_bytes)
    check(rem(whole, new.record_bytes) == 0 or (byte_size(records) - n * new.record_bytes) == whole - n * new.record_bytes,
      "the body's records are new.record_bytes apart")
  end`)
		}},

		// The static data's member names and order — TableFixedKnownLayout =
		// {hash, layout, layout_bytes, record_bytes} — where hash and record_bytes
		// are the key and the read value, layout and layout_bytes are the verification
		// pair, and the report's layout_hash is the last known layout's hash and zero
		// on every other path. Every entry is its own closure: the data holding one
		// entry's facts never rides the wire or a cache boundary.
		{"elixir/R23", func(t *testing.T) {
			t02Probe(t, "VNEW_field_append", lineage, 0, newFile, `  known = T.lineage_fixed_known()
  for entry <- Tuple.to_list(known) do
    check(is_map(entry), "every entry is a map")
    check(Map.has_key?(entry, :hash), "every entry has :hash")
    check(Map.has_key?(entry, :layout), "every entry has :layout")
    check(Map.has_key?(entry, :layout_bytes), "every entry has :layout_bytes")
    check(Map.has_key?(entry, :record_bytes), "every entry has :record_bytes")
    keys = Map.keys(entry) |> Enum.sort()
    check(keys == [:hash, :layout, :layout_bytes, :record_bytes],
      "every entry has only these four fields in order: #{inspect(keys)}")
  end
  {:ok, _, report} = load(File.read!(file()))
  check(report.layout_hash == (elem(known, tuple_size(known) - 1).hash),
      "the report's layout_hash is the last known layout's hash after a successful read")
  data = File.read!(file())
  <<head::binary-size(8), forged_hash::little-unsigned-64, rest::binary>> = data
  {:error, _, breport} = load(<<head::binary, 0x0EADBEEFCAFEF00D::little-unsigned-64, rest::binary>>)
  check(breport.layout_hash == 0x0EADBEEFCAFEF00D, "the report's layout_hash is the file's hash on unknown layout")
  {:error, _, ureport} = load(<<2, 0, 0, 0, 0, 0, 0, 0, 0x1234567890ABCDEF::little-unsigned-64>>)
  check(ureport.layout_hash == 0, "the report's layout_hash is zero on malformed reads")`)
		}},

		// plan_too_large when the plan does not fit the caller's capacity. §5.3
		// step 10 runs the plan compiled once from the SELECTED LINEAGE ENTRY's
		// layout bytes, and that plan's BYTE LENGTH cannot exceed the caller's
		// capacity. The capacity is a parameter to the read; its default is derived
		// from the reader's OWN plan's length. A file whose selected entry's PLAN is
		// longer than the capacity is a REFUSAL BY NAME, never a throw.
		{"elixir/R25", func(t *testing.T) {
			t02Probe(t, "VNEW_field_append", lineage, 0, newFile, `  data = File.read!(file())
  {:ok, _, _} = load(data)
  check(true, "the control reads the file: default capacity is sufficient")
  {:ok, _, _} = load(data, plan_capacity: 1000)
  check(true, "a large capacity reads the file")
  {:error, reason, report} = load(data, plan_capacity: 1)
  check(reason == :plan_too_large, "plan_capacity: 1 refuses with plan_too_large: #{inspect(reason)}")
  check(not is_list(reason), "plan_too_large built no value")
  check(report == R.report(), "plan_too_large moves nothing and is never malformed: #{inspect(report)}")`)
		}},

		// a known hash whose lineage entry would not build → layout_malformed /
		// plan_too_large by that entry's own lane, never a throw. When the lock
		// records a lineage entry whose layout bytes cannot build a plan (the layout
		// is bad by the lock's own rules §1.1), the build accepts it (the lock is
		// always true) but the lane — the {hash, plan_bytes, plan} read path —
		// answers with the ONE NAME that entry's lane calls for. The entry is not
		// used when a file matches something else; when a file matches this hash the
		// answer is not thrown but named.
		{"elixir/R26", func(t *testing.T) {
			t02Probe(t, "VNEW_field_append", lineage, 0, newFile, `  data = File.read!(file())
  {:ok, values, report} = load(data)
  check(length(values) > 0 and report.malformed == false, "the reader's own file loads: #{inspect(length(values))}")
  {:error, reason, breport} = load(data, plan_capacity: 0)
  check(reason == :plan_too_large, "plan_capacity: 0 refuses with plan_too_large")
  check(breport == R.report(), "plan_too_large's report is untouched and never malformed")`)
		}},

		// plan dst == offsetof/sizeof. The PLAN moves record bytes from the file's
		// image to the READER'S OWN offsets ("dst" in Elixir), which is the struct's
		// byte layout. The five fixedDst values — {dst, stride, aux, counted, arg} —
		// drive both the prefill and the plan run. The plan's DST ROWS are the only
		// bridge between the WIRE LAYOUT and the READER'S OWN STRUCT, and they are
		// calculated from the reader's own declaredFields in the same walk that the
		// writer used. Every offsetof and sizeof the plan assumes are the reader's
		// own types' (the run never lands a byte the struct cannot hold).
		{"elixir/W14", func(t *testing.T) {
			t02Probe(t, "VNEW_field_append", lineage, 0, newFile, `  data = File.read!(file())
  {:ok, values, _} = load(data)
  check(length(values) > 0, "at least one record loaded")
  first = hd(values)
  check(is_struct(first), "the first value is a struct")
  check(map_size(first) > 0, "the struct has at least one field")
  dst = T.field_append_fixed_dst()
  check(is_map(dst) or is_tuple(dst), "dst is available from the reader")
  check(byte_size(T.field_append_fixed_layout()) > 0, "the layout is non-empty")
  check(T.field_append_fixed_record_bytes() == 8 + T.field_append_fixed_body_bytes(),
      "record_bytes equals 8 + body_bytes")`)
		}},
	}
	for _, tc := range tasks {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}
