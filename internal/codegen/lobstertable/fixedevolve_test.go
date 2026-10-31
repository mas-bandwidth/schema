package lobstertable

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type lobsterVersionRow struct {
	row      string
	sameHash bool
	widens   bool
	check    string
}

var lobsterVersionRows = []lobsterVersionRow{
	{row: "array_bounded_grow", check: `
    assert_eq(back[0].ValsCount, 4, "vals_count")
    assert_eq(back[0].Vals[0], 1000, "vals[0]")
    assert_eq(back[0].Vals[3], 1003, "vals[3]")
    assert_eq(back[0].Vals[4], 0, "vals[4] default")
    assert_eq(back[0].Vals[7], 0, "vals[7] default")`},
	{row: "array_elem_widen", widens: true},
	{row: "array_fixed_grow", check: `
    assert_eq(back[0].Vals[0].X, -500, "vals[0].x")
    assert_eq(back[0].Vals[3].Y, -603, "vals[3].y")
    assert_eq(back[0].Vals[4].X, 7, "vals[4].x prefill default")
    assert_eq(back[0].Vals[7].Y, 9, "vals[7].y prefill default")`},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
    assert_eq(back[0].X, 11, "x")
    assert_eq(back[0].Y, 22, "y")
    assert_eq(back[0].Z, 33, "z")
    assert_eq(back[0].W, 77, "w prefill default")`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "fixed_I_grow_element", widens: true, check: `
    assert_eq(n, 1, "fixed_I_grow_element records")
    assert_eq(back[0].Vals[0], -1, "vals[0]")
    assert_eq(back[0].Vals[1], -128, "vals[1]")
    assert_eq(back[0].Vals[2], 0, "vals[2]")
    assert_eq(back[0].Vals[3], 112, "vals[3]")
    assert_eq(back[0].Lead, 0xAAAAAAAA, "lead")
    assert_eq(back[0].Trail, 0xBBBBBBBB, "trail")`},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
    assert_eq(n, 3, "int_widen records")
    assert_eq(back[0].V, -1, "r0.v")
    assert_eq(back[1].V, -32768, "r1.v")
    assert_eq(back[2].V, 32767, "r2.v")
    for(3) k:
        assert_eq(back[k].Lead, 0xAAAAAAAA, "lead")
        assert_eq(back[k].Trail, 0xBBBBBBBB, "trail")`},
	{row: "keyed_array_enum_append", check: `
    assert_eq(back[0].Slots[0].N, 101, "slots[0].n")
    assert_eq(back[0].Slots[1].N, 102, "slots[1].n")
    assert_eq(back[0].Slots[2].N, 103, "slots[2].n")
    assert_eq(back[0].Slots[3].N, 7, "slots[3].n prefill default")
    assert_eq(back[0].Seq, 22, "seq")`},
	{row: "nested_append", check: `
    assert_eq(back[0].V.X, 1, "v.x")
    assert_eq(back[0].V.Y, 2, "v.y")
    assert_eq(back[0].V.Z, 3, "v.z")
    assert_eq(back[0].V.W, 88, "v.w prefill default")
    assert_eq(back[0].Seq, 24, "seq")`},
	{row: "optional_add", check: `
    assert_true(back[0].LinkPresent, "T into ?T owes LinkPresent = true")
    assert_eq(back[0].Link.Value, 777, "Link.Value")`},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	{row: "union_arm_payload_widen", check: `
    assert_eq(int(back[0].Pick.Type), 1, "pick.type")
    assert_eq(back[0].Pick.Alpha.X, 21, "pick.alpha.x")
    assert_eq(back[0].Pick.Alpha.Y, 55, "pick.alpha.y prefill default")
    assert_eq(back[0].Seq, 18, "seq")`},
}

func TestLobsterFixedFormEvolution(t *testing.T) {
	t.Parallel()
	corpus := lobsterFixedCorpus(t)
	var compared atomic.Int64

	t.Run("new_reads_old", func(t *testing.T) {
		for _, r := range lobsterVersionRows {
			t.Run(r.row, func(t *testing.T) {
				t.Parallel()
				newer := lobsterReadSchema(t, "VNEW_"+r.row)
				older := lobsterReadSchema(t, "VOLD_"+r.row)
				root := lobsterFixedRoot(t, older)
				table := root.Name
				uNew := lobsterUnitOf(t, newer)
				pkg := uNew.Package
				lead, trail := lobsterBrackets(t, corpus, r.row, ir.TableFixedTypeBytes(root))
				bracket := ""
				if lobsterHasField(root, "lead") && lobsterHasField(root, "trail") {
					bracket = fmt.Sprintf("\n    assert_eq(back[0].Lead, 0x%08X, \"lead\")\n    assert_eq(back[0].Trail, 0x%08X, \"trail\")", lead, trail)
				}
				counters := `assert_eq(r.widened, 0, "append row widened == 0")`
				if r.widens {
					counters = `assert_true(r.widened > 0, "widening row widened > 0")`
				}
				inBytes, err := os.ReadFile(filepath.Join(corpus, "old_"+r.row+".bin"))
				if err != nil {
					t.Fatal(err)
				}
				if field, ok := corpusNonFinite(t, corpus, "old_"+r.row+".bin"); ok {
					// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): an oracle
					// file that still carries a NaN or an infinity (the C++
					// corpus before finite-tables #2232 regenerates it) is
					// refused by the strict and the tolerant read alike, named
					body := fmt.Sprintf(`
    let data = read_in("in.bin")
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoad(back, data, plan, r)
    assert_eq(n, -1, "a non-finite float refuses the strict read")
    assert_true(r.reason == "non_finite_float: %s.%s" and r.non_finite_float == "%s.%s", "named: " + r.reason)
    let t = schema_rt.TableReport {}
    let m = %s.%sFixedLoadTolerant(back, data, plan, t)
    assert_eq(m, -1, "a non-finite float refuses the tolerant read")
    assert_true(schema_rt.fixed_report_err(t) == "non_finite_float", "err: " + schema_rt.fixed_report_err(t))
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table, table, field, table, field, pkg, table)
					out := runLobsterVersionProbe(t, newer, []string{older}, 0, body, map[string][]byte{"in.bin": inBytes})
					if strings.TrimSpace(out) != "PASS" {
						t.Fatalf("NEW-READS-OLD %s (non-finite oracle): got %q", r.row, out)
					}
					compared.Add(1)
					return
				}
				body := fmt.Sprintf(`
    let data = read_in("in.bin")
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoad(back, data, plan, r)
    assert_true(n >= 1, "n >= 1")
    assert_true(r.reason == "" and not r.malformed and r.verdict == schema_rt.TableOpenExact, "clean verdict: " + r.reason)
    assert_true(r.unknown == 0 and r.kind_mismatch == 0 and r.clamped == 0 and r.duplicate == 0, "zero counters")
    %s%s%s
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table, counters, bracket, r.check)
				out := runLobsterVersionProbe(t, newer, []string{older}, 0, body, map[string][]byte{"in.bin": inBytes})
				if strings.TrimSpace(out) != "PASS" {
					t.Fatalf("NEW-READS-OLD %s: got %q", r.row, out)
				}
				compared.Add(1)
			})
		}
	})

	t.Run("old_refuses_new", func(t *testing.T) {
		for _, r := range lobsterVersionRows {
			t.Run(r.row, func(t *testing.T) {
				t.Parallel()
				older := lobsterReadSchema(t, "VOLD_"+r.row)
				root := lobsterFixedRoot(t, older)
				table := root.Name
				uOld := lobsterUnitOf(t, older)
				pkg := uOld.Package
				inBytes, err := os.ReadFile(filepath.Join(corpus, "new_"+r.row+".bin"))
				if err != nil {
					t.Fatal(err)
				}
				var checkBody string
				if r.sameHash {
					checkBody = `
    assert_true(n >= 1, "sameHash reads both ways")
    assert_true(r.reason == "" and not r.malformed, "sameHash clean")`
				} else {
					checkBody = fmt.Sprintf(`
    let want = read_uint64_le(data, 8)
    assert_eq(n, -1, "old refuses new")
    assert_true(r.verdict == schema_rt.TableOpenRefused, "verdict refused")
    assert_true(r.reason == "layout_unknown", "reason layout_unknown, got " + r.reason)
    assert_eq(r.layout_hash, want, "layout_hash matches file header")
    assert_true(not r.malformed, "refusal by name is not malformed")
    assert_true(r.widened == 0 and r.unknown == 0 and r.kind_mismatch == 0 and r.clamped == 0 and r.duplicate == 0, "REFUSE moves no counter")
    assert_true(%s.%sFixedSave([back[0]]) == %s.%sFixedSave([fresh]), "REFUSE touched destination")`, pkg, table, pkg, table)
				}
				body := fmt.Sprintf(`
    let data = read_in("in.bin")
    let fresh = %s.New%s()
    %s.%sReset(fresh)
    let back:[%s.%s] = []
    for(8):
        let v = %s.New%s()
        %s.%sReset(v)
        push(back, v)
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoad(back, data, plan, r)
    %s
    print "PASS"
`, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, pkg, table, checkBody)
				out := runLobsterVersionProbe(t, older, nil, 0, body, map[string][]byte{"in.bin": inBytes})
				if strings.TrimSpace(out) != "PASS" {
					t.Fatalf("OLD-REFUSES-NEW %s: got %q", r.row, out)
				}
				compared.Add(1)
			})
		}
	})

	t.Run("floor", func(t *testing.T) {
		older := lobsterReadSchema(t, "VOLD_floor")
		mid := lobsterReadSchema(t, "VMID_floor")
		newer := lobsterReadSchema(t, "VNEW_floor")
		table := lobsterFixedRoot(t, older).Name
		pkg := lobsterUnitOf(t, newer).Package
		oldBytes, err := os.ReadFile(filepath.Join(corpus, "old_floor.bin"))
		if err != nil {
			t.Fatal(err)
		}
		midBytes, err := os.ReadFile(filepath.Join(corpus, "mid_floor.bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{"old.bin": oldBytes, "mid.bin": midBytes}

		t.Run("floor_at", func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoad(back, read_in("old.bin"), plan, r)
    assert_true(n >= 1 and r.reason == "" and not r.malformed, "floor_at reads")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table)
			if out := runLobsterVersionProbe(t, newer, []string{older, mid}, 0, body, inputs); strings.TrimSpace(out) != "PASS" {
				t.Fatalf("floor_at: %q", out)
			}
			compared.Add(1)
		})

		t.Run("floor_below", func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let old_data = read_in("old.bin")
    let n = %s.%sFixedLoad(back, old_data, plan, r)
    assert_true(n == -1 and r.reason == "layout_unsupported", "floor_below refuses layout_unsupported")
    assert_eq(r.layout_hash, read_uint64_le(old_data, 8), "floor_below layout_hash")
    assert_true(not r.malformed and r.widened == 0 and r.unknown == 0 and r.clamped == 0, "REFUSE is total")
    let m = %s.%sFixedLoad(back, read_in("mid.bin"), plan, r)
    assert_true(m >= 1 and r.reason == "", "mid_floor at raised floor still reads")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table, pkg, table)
			if out := runLobsterVersionProbe(t, newer, []string{older, mid}, 1, body, inputs); strings.TrimSpace(out) != "PASS" {
				t.Fatalf("floor_below: %q", out)
			}
			compared.Add(1)
		})

		t.Run("floor_raise_live", func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let mid_data = read_in("mid.bin")
    let n = %s.%sFixedLoad(back, mid_data, plan, r)
    assert_true(n == -1 and r.reason == "layout_unsupported", "floor_raise_live refuses layout_unsupported")
    assert_eq(r.layout_hash, read_uint64_le(mid_data, 8), "floor_raise_live layout_hash")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table)
			if out := runLobsterVersionProbe(t, newer, []string{older, mid}, 2, body, inputs); strings.TrimSpace(out) != "PASS" {
				t.Fatalf("floor_raise_live: %q", out)
			}
			compared.Add(1)
		})
	})

	t.Run("hash", func(t *testing.T) {
		t.Parallel()
		newer := lobsterReadSchema(t, "VNEW_field_append")
		older := lobsterReadSchema(t, "VOLD_field_append")
		table := lobsterFixedRoot(t, older).Name
		pkg := lobsterUnitOf(t, newer).Package
		oldBytes, err := os.ReadFile(filepath.Join(corpus, "old_field_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		newBytes, err := os.ReadFile(filepath.Join(corpus, "new_field_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		unknownBytes := append([]byte(nil), oldBytes...)
		binary.LittleEndian.PutUint64(unknownBytes[8:16], 0xDEADBEEFCAFEF00D)
		differBytes := append([]byte(nil), oldBytes...)
		differBytes[20] ^= 0xFF
		inputs := map[string][]byte{
			"unknown.bin": unknownBytes,
			"differ.bin":  differBytes,
			"ident.bin":   newBytes,
		}
		body := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let unk = read_in("unknown.bin")
    let n1 = %s.%sFixedLoad(back, unk, plan, r)
    assert_true(n1 == -1 and r.reason == "layout_unknown" and not r.malformed, "hash_unknown")
    assert_eq(r.layout_hash, read_uint64_le(unk, 8), "hash_unknown layout_hash")
    let n2 = %s.%sFixedLoad(back, read_in("differ.bin"), plan, r)
    assert_true(n2 == -1 and r.reason == "layout_malformed" and not r.malformed, "hash_known_bytes_differ")
    let n3 = %s.%sFixedLoad(back, read_in("ident.bin"), plan, r)
    assert_true(n3 >= 1 and r.reason == "" and not r.malformed and back[0].W == 777, "hash_identity")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table, pkg, table, pkg, table)
		if out := runLobsterVersionProbe(t, newer, []string{older}, 0, body, inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("hash: %q", out)
		}
		compared.Add(3)
	})

	t.Run("lineage_merge", func(t *testing.T) {
		t.Parallel()
		merged := lobsterReadSchema(t, "VNEW_lineage_merge")
		a := lobsterReadSchema(t, "VBRA_lineage_merge")
		b := lobsterReadSchema(t, "VBRB_lineage_merge")
		base := lobsterReadSchema(t, "VOLD_lineage_merge")
		table := lobsterFixedRoot(t, base).Name
		pkg := lobsterUnitOf(t, merged).Package
		aBytes, err := os.ReadFile(filepath.Join(corpus, "a_lineage_merge.bin"))
		if err != nil {
			t.Fatal(err)
		}
		bBytes, err := os.ReadFile(filepath.Join(corpus, "b_lineage_merge.bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{"a.bin": aBytes, "b.bin": bBytes}
		body := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let na = %s.%sFixedLoad(back, read_in("a.bin"), plan, r)
    assert_true(na >= 1 and r.reason == "" and not r.malformed, "a_lineage_merge reads")
    let nb = %s.%sFixedLoad(back, read_in("b.bin"), plan, r)
    assert_true(nb >= 1 and r.reason == "" and not r.malformed, "b_lineage_merge reads")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table, pkg, table)
		if out := runLobsterVersionProbe(t, merged, []string{base, a, b}, 0, body, inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("lineage_merge: %q", out)
		}
		compared.Add(2)
	})

	t.Run("writer_bound_count", func(t *testing.T) {
		t.Parallel()
		newer := lobsterReadSchema(t, "VNEW_array_bounded_grow")
		writer := lobsterReadSchema(t, "VOLD_array_bounded_grow")
		distractor := lobsterReadSchema(t, "VMID_array_bounded_grow")
		table := lobsterFixedRoot(t, writer).Name
		pkg := lobsterUnitOf(t, newer).Package
		raw, err := os.ReadFile(filepath.Join(corpus, "old_array_bounded_grow.bin"))
		if err != nil {
			t.Fatal(err)
		}
		needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
		at := bytes.Index(raw, needle)
		if at < 0 || bytes.Contains(raw[at+1:], needle) {
			t.Fatal("writer_bound_count needle locator failed")
		}
		forged7 := append([]byte(nil), raw...)
		binary.LittleEndian.PutUint32(forged7[at+4:], 7)
		forgedNeg := append([]byte(nil), raw...)
		binary.LittleEndian.PutUint32(forgedNeg[at+4:], 0xFFFFFFFF)
		forged9 := append([]byte(nil), raw...)
		binary.LittleEndian.PutUint32(forged9[at+4:], 9)
		inputs := map[string][]byte{"f7.bin": forged7, "fneg.bin": forgedNeg, "f9.bin": forged9}
		body := fmt.Sprintf(`
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    for(["f7.bin", "fneg.bin", "f9.bin"]) fname:
        let back:[%s.%s] = []
        let v = %s.New%s()
        v.Lead = 0x5A5A5A5A
        v.ValsCount = 0x5A5A5A5A
        v.Trail = 0x5A5A5A5A
        push(back, v)
        let n = %s.%sFixedLoad(back, read_in(fname), plan, r)
        assert_true(n == -1 and r.verdict == schema_rt.TableOpenRefused and r.reason == "count_over_bound", fname + " count_over_bound")
        assert_true(not r.malformed and r.clamped == 0, fname + " counters untouched")
        assert_true(back[0].Lead == 0x5A5A5A5A and back[0].ValsCount == 0x5A5A5A5A and back[0].Trail == 0x5A5A5A5A, fname + " destination untouched")
    print "PASS"
`, pkg, pkg, table, pkg, table, pkg, table)
		if out := runLobsterVersionProbe(t, newer, []string{writer}, 0, body, inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("writer_bound_count single peer: %q", out)
		}
		if out := runLobsterVersionProbe(t, newer, []string{writer, distractor}, 0, body, inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("writer_bound_count two peers: %q", out)
		}
		compared.Add(4)
	})

	t.Run("forged_ordinal_both_plans", func(t *testing.T) {
		t.Parallel()
		newer := lobsterReadSchema(t, "VNEW_enum_append")
		older := lobsterReadSchema(t, "VOLD_enum_append")
		table := lobsterFixedRoot(t, older).Name
		raw, err := os.ReadFile(filepath.Join(corpus, "old_enum_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		needle := []byte{0x03, 0x09, 0x00, 0x00, 0x00}
		at := bytes.Index(raw, needle)
		if at < 0 || bytes.Contains(raw[at+1:], needle) {
			t.Fatal("forged_ordinal needle locator failed")
		}
		forged := append([]byte(nil), raw...)
		forged[at] = 4
		inputs := map[string][]byte{"in.bin": forged}
		makeBody := func(pkg string) string {
			return fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoadTolerant(back, read_in("in.bin"), plan, r)
    assert_eq(n, 1, "forged ordinal reads 1 record")
    assert_true(r.verdict == schema_rt.TableOpenExact and r.reason == "" and not r.malformed, "not refused")
    assert_eq(int(back[0].Tier), 0, "forged ordinal lands None (0)")
    assert_eq(back[0].Seq, 9, "seq == 9")
    assert_eq(r.clamped, 1, "clamped == 1")
    assert_true(r.unknown == 0 and r.kind_mismatch == 0 and r.widened == 0 and r.duplicate == 0, "other counters 0")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table)
		}
		if out := runLobsterVersionProbe(t, newer, []string{older}, 0, makeBody(lobsterUnitOf(t, newer).Package), inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("forged_ordinal compiled: %q", out)
		}
		if out := runLobsterVersionProbe(t, older, nil, 0, makeBody(lobsterUnitOf(t, older).Package), inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("forged_ordinal identity: %q", out)
		}
		compared.Add(2)
	})

	t.Run("union_tag_both_plans", func(t *testing.T) {
		t.Parallel()
		newer := lobsterReadSchema(t, "VNEW_union_append")
		older := lobsterReadSchema(t, "VOLD_union_append")
		table := lobsterFixedRoot(t, older).Name
		raw, err := os.ReadFile(filepath.Join(corpus, "old_union_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
		at := bytes.Index(raw, needle)
		if at < 0 || bytes.Contains(raw[at+1:], needle) {
			t.Fatal("union_tag needle locator failed")
		}
		forged := append([]byte(nil), raw...)
		forged[at] = 9
		inputs := map[string][]byte{"in.bin": forged}
		makeBody := func(pkg string) string {
			return fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoadTolerant(back, read_in("in.bin"), plan, r)
    assert_eq(n, 1, "forged union tag reads 1 record")
    assert_true(r.verdict == schema_rt.TableOpenExact and r.reason == "" and not r.malformed, "not refused")
    assert_eq(int(back[0].Pick.Type), 0, "forged union tag lands None (0)")
    assert_eq(back[0].Seq, 15, "seq == 15")
    assert_eq(r.clamped, 1, "clamped == 1")
    assert_true(r.unknown == 0 and r.kind_mismatch == 0 and r.widened == 0 and r.duplicate == 0, "other counters 0")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table)
		}
		if out := runLobsterVersionProbe(t, newer, []string{older}, 0, makeBody(lobsterUnitOf(t, newer).Package), inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("union_tag compiled: %q", out)
		}
		if out := runLobsterVersionProbe(t, older, nil, 0, makeBody(lobsterUnitOf(t, older).Package), inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("union_tag identity: %q", out)
		}
		compared.Add(2)
	})

	t.Run("refuse_writes_nothing", func(t *testing.T) {
		t.Parallel()
		newer := lobsterReadSchema(t, "VNEW_nested_append")
		older := lobsterReadSchema(t, "VOLD_nested_append")
		table := lobsterFixedRoot(t, older).Name
		pkg := lobsterUnitOf(t, newer).Package
		raw, err := os.ReadFile(filepath.Join(corpus, "old_nested_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		needle := []byte{1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 24, 0, 0, 0}
		at := bytes.Index(raw, needle)
		if at < 0 || bytes.Contains(raw[at+1:], needle) {
			t.Fatal("refuse_writes_nothing needle locator failed")
		}
		forged := append([]byte(nil), raw...)
		for i := at - 8; i < at; i++ {
			forged[i] = ^forged[i]
		}
		body := fmt.Sprintf(`
    let back:[%s.%s] = []
    let p = %s.New%s()
    p.V.X = 0x5A5A5A5A
    p.V.Y = 0x5A5A5A5A
    p.V.Z = 0x5A5A5A5A
    p.V.W = 0x5A5A5A5A
    p.Seq = 0x5A5A5A5A
    push(back, p)
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoad(back, read_in("in.bin"), plan, r)
    assert_eq(n, -1, "no_layout returns -1")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "no_layout" and not r.malformed, "no_layout verdict")
    assert_eq(r.layout_hash, 0, "no_layout layout_hash == 0")
    assert_true(r.widened == 0 and r.unknown == 0 and r.kind_mismatch == 0 and r.clamped == 0 and r.duplicate == 0, "no counter moves")
    assert_true(back[0].V.X == 0x5A5A5A5A and back[0].V.W == 0x5A5A5A5A and back[0].Seq == 0x5A5A5A5A, "destination untouched")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table)
		if out := runLobsterVersionProbe(t, newer, []string{older}, 0, body, map[string][]byte{"in.bin": forged}); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("refuse_writes_nothing: %q", out)
		}
		compared.Add(1)
	})

	t.Run("unknown_census", func(t *testing.T) {
		t.Parallel()
		older := lobsterReadSchema(t, "VOLD_unknown_census")
		newer := lobsterReadSchema(t, "VNEW_unknown_census")
		table := lobsterFixedRoot(t, older).Name
		pkg := lobsterUnitOf(t, newer).Package
		oneBytes, err := os.ReadFile(filepath.Join(corpus, "old_unknown_census.bin"))
		if err != nil {
			t.Fatal(err)
		}
		manyBytes, err := os.ReadFile(filepath.Join(corpus, "many_unknown_census.bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{"one.bin": oneBytes, "many.bin": manyBytes}
		body := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n1 = %s.%sFixedLoad(back, read_in("one.bin"), plan, r)
    assert_eq(n1, 1, "unknown_census 1 record")
    assert_eq(r.unknown, 1, "unknown == 1 once per peer, not per element")
    assert_true(r.kind_mismatch == 0 and r.widened == 0 and r.clamped == 0 and not r.malformed, "clean counters")
    assert_true(back[0].Lead == 1 and back[0].Trail == 2, "brackets")
    for(4) k:
        assert_eq(back[0].Items[k].A, 10 + k, "items.a")
    let n1b = %s.%sFixedLoad(back, read_in("one.bin"), plan, r)
    assert_true(n1b == 1 and r.unknown == 1, "second read same census")

    let n3 = %s.%sFixedLoad(back, read_in("many.bin"), plan, r)
    assert_eq(n3, 3, "unknown_census_many 3 records")
    assert_eq(r.unknown, 1, "unknown == 1 once per peer, not per record")
    for(3) rec:
        assert_true(back[rec].Lead == 1 and back[rec].Trail == 2, "many brackets")
        for(4) k:
            assert_eq(back[rec].Items[k].A, (rec + 1) * 10 + k, "many items.a")
    print "PASS"
`, pkg, table, pkg, table, pkg, pkg, table, pkg, table, pkg, table)
		if out := runLobsterVersionProbe(t, newer, []string{older}, 0, body, inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("unknown_census: %q", out)
		}
		compared.Add(2)
	})

	t.Run("cfloat", func(t *testing.T) {
		t.Parallel()
		olderRef := lobsterReadSchema(t, "VOLD_cfloat_res_refine")
		newerRef := lobsterReadSchema(t, "VNEW_cfloat_res_refine")
		tableRef := lobsterFixedRoot(t, olderRef).Name
		pkgRef := lobsterUnitOf(t, newerRef).Package
		aimRef := lobsterManifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim")
		refBytes, err := os.ReadFile(filepath.Join(corpus, "old_cfloat_res_refine.bin"))
		if err != nil {
			t.Fatal(err)
		}
		bodyRef := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoad(back, read_in("in.bin"), plan, r)
    assert_eq(n, 1, "cfloat_res_refine n == 1")
    assert_true(r.clamped == 0 and r.widened == 0 and r.unknown == 0 and not r.malformed, "cfloat_res_refine counters")
    assert_true(back[0].Lead == 1 and back[0].Trail == 2, "cfloat_res_refine brackets")
    let saved = write_float32_le(schema_rt.zeros(4), 0, back[0].Aim)
    assert_eq(read_uint32_le(saved, 0), %d, "cfloat_res_refine aim bits")
    print "PASS"
`, pkgRef, tableRef, pkgRef, tableRef, pkgRef, pkgRef, tableRef, int64(aimRef))
		if out := runLobsterVersionProbe(t, newerRef, []string{olderRef}, 0, bodyRef, map[string][]byte{"in.bin": refBytes}); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("cfloat_res_refine: %q", out)
		}

		olderWid := lobsterReadSchema(t, "VOLD_cfloat_range_widen")
		newerWid := lobsterReadSchema(t, "VNEW_cfloat_range_widen")
		tableWid := lobsterFixedRoot(t, olderWid).Name
		pkgWid := lobsterUnitOf(t, newerWid).Package
		aimClean := lobsterManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim")
		aimHostile := lobsterManifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim")
		cleanBytes, err := os.ReadFile(filepath.Join(corpus, "old_cfloat_range_widen.bin"))
		if err != nil {
			t.Fatal(err)
		}
		hostileBytes, err := os.ReadFile(filepath.Join(corpus, "hostile_cfloat_range_widen.bin"))
		if err != nil {
			t.Fatal(err)
		}
		bodyWid := fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n1 = %s.%sFixedLoad(back, read_in("clean.bin"), plan, r)
    assert_true(n1 == 1 and r.clamped == 0 and not r.malformed, "cfloat_range_widen clean")
    assert_eq(read_uint32_le(write_float32_le(schema_rt.zeros(4), 0, back[0].Aim), 0), %d, "cfloat_range_widen clean bits")
    let n2 = %s.%sFixedLoad(back, read_in("hostile.bin"), plan, r)
    assert_true(n2 == 1 and r.clamped == 0 and not r.malformed, "cfloat_range_widen hostile")
    assert_eq(read_uint32_le(write_float32_le(schema_rt.zeros(4), 0, back[0].Aim), 0), %d, "cfloat_range_widen hostile bits")
    print "PASS"
`, pkgWid, tableWid, pkgWid, tableWid, pkgWid, pkgWid, tableWid, int64(aimClean), pkgWid, tableWid, int64(aimHostile))
		if out := runLobsterVersionProbe(t, newerWid, []string{olderWid}, 0, bodyWid, map[string][]byte{"clean.bin": cleanBytes, "hostile.bin": hostileBytes}); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("cfloat_range_widen: %q", out)
		}
		compared.Add(3)
	})

	t.Run("union_unselected_arm", func(t *testing.T) {
		t.Parallel()
		older := lobsterReadSchema(t, "VOLD_union_append")
		newer := lobsterReadSchema(t, "VNEW_union_append")
		table := lobsterFixedRoot(t, older).Name
		inBytes, err := os.ReadFile(filepath.Join(corpus, "old_union_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{"in.bin": inBytes}
		makeBody := func(pkg string, hasGamma bool) string {
			gammaPoison := ""
			if hasGamma {
				gammaPoison = "\n    back[0].Pick.Gamma.P = 0x5A5A5A5A"
			}
			return fmt.Sprintf(`
    let back:[%s.%s] = []
    for(8): push(back, %s.New%s())
    back[0].Pick.Alpha.M = 0x5A5A5A5A
    back[0].Pick.Beta.N = 0x5A5A5A5A%s
    back[0].Seq = 0x5A5A5A5A
    let r = schema_rt.TableReport {}
    let plan = %s.make_plan(4096)
    let n = %s.%sFixedLoad(back, read_in("in.bin"), plan, r)
    assert_eq(n, 1, "union_unselected_arm n == 1")
    assert_true(r.reason == "" and not r.malformed and r.clamped == 0 and r.widened == 0 and r.unknown == 0, "clean counters")
    assert_eq(int(back[0].Pick.Type), 1, "pick.type == Alpha")
    assert_eq(back[0].Pick.Alpha.M, 7, "pick.alpha.m == 7")
    assert_eq(back[0].Seq, 15, "seq == 15")
    print "PASS"
`, pkg, table, pkg, table, gammaPoison, pkg, pkg, table)
		}
		if out := runLobsterVersionProbe(t, newer, []string{older}, 0, makeBody(lobsterUnitOf(t, newer).Package, true), inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("union_unselected_arm compiled: %q", out)
		}
		if out := runLobsterVersionProbe(t, older, nil, 0, makeBody(lobsterUnitOf(t, older).Package, false), inputs); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("union_unselected_arm identity: %q", out)
		}
		compared.Add(2)
	})

	t.Cleanup(func() {
		t.Logf("fixed-form evolution cases compared: %d", compared.Load())
	})
}

func TestLobsterFixedFormNamesEveryRefusal(t *testing.T) {
	t.Parallel()
	corpus := lobsterFixedCorpus(t)
	newer := lobsterReadSchema(t, "VNEW_field_append")
	older := lobsterReadSchema(t, "VOLD_field_append")
	table := lobsterFixedRoot(t, older).Name
	pkg := lobsterUnitOf(t, newer).Package
	valid, err := os.ReadFile(filepath.Join(corpus, "new_field_append.bin"))
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
    let valid = read_in("valid.bin")
    let plan = %s.make_plan(4096)

    // 1. empty file -> truncated (malformed)
    let back:[%s.%s] = [ %s.New%s() ]
    back[0].X = 0x5A
    back[0].Y = 0x5A
    back[0].Z = 0x5A
    back[0].W = 0x5A
    let r = schema_rt.TableReport {}
    assert_eq(%s.%sFixedLoad(back, "", plan, r), -1, "empty")
    assert_true(r.malformed and r.reason == "truncated" and back[0].X == 0x5A, "truncated empty")

    // 2. form 1 -> previous_form
    let f1 = write_int8_le(schema_rt.zeros(10), 0, 1)
    assert_eq(%s.%sFixedLoad(back, f1, plan, r), -1, "form 1")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.fixed_raised and r.reason == "previous_form" and not r.malformed, "previous_form")
    assert_true(schema_rt.fixed_report_err(r) == "unknown_form", "unknown_form via fixed_report_err")

    // 3. form 2 -> message_form_as_file
    let f2 = write_int8_le(schema_rt.zeros(3), 0, 2)
    assert_eq(%s.%sFixedLoad(back, f2, plan, r), -1, "form 2")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.fixed_raised and r.reason == "message_form_as_file" and not r.malformed, "message_form_as_file")

    // 4. form 0 / form 99 -> newer_form
    let f0 = write_int8_le(substring(valid, 0, length(valid)), 0, 0)
    assert_eq(%s.%sFixedLoad(back, f0, plan, r), -1, "form 0")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.fixed_raised and r.reason == "newer_form" and not r.malformed, "newer_form")

    // 5. short header (< 20 bytes) -> truncated
    let short_hdr = substring(valid, 0, 19)
    assert_eq(%s.%sFixedLoad(back, short_hdr, plan, r), -1, "short header")
    assert_true(r.malformed and r.reason == "truncated", "short header truncated")

    // 6. reserved byte nonzero -> reserved_not_zero
    let res_nz = write_int8_le(substring(valid, 0, length(valid)), 3, 1)
    assert_eq(%s.%sFixedLoad(back, res_nz, plan, r), -1, "reserved_not_zero")
    assert_true(r.malformed and r.reason == "reserved_not_zero", "reserved_not_zero reason")

    // 7. layout_bytes past file -> layout_malformed
    let bad_len = write_int32_le(substring(valid, 0, 20), 16, 100)
    assert_eq(%s.%sFixedLoad(back, bad_len, plan, r), -1, "layout_malformed short")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "layout_malformed" and not r.malformed, "layout_malformed")

    // 8. unknown hash -> layout_unknown
    let unk_hash = write_int64_le(substring(valid, 0, length(valid)), 8, 0x123456789ABCDEF0)
    assert_eq(%s.%sFixedLoad(back, unk_hash, plan, r), -1, "layout_unknown")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "layout_unknown" and r.layout_hash == 0x123456789ABCDEF0, "layout_unknown hash")

    // 9. plan_too_large -> plan_too_large
    let tiny_plan:[schema_rt.TableFixedEntry] = []
    assert_eq(%s.%sFixedLoad(back, valid, tiny_plan, r), -1, "plan_too_large")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "plan_too_large" and not r.malformed, "plan_too_large reason")

    // 10. ragged record tail -> truncated
    let ragged = substring(valid, 0, length(valid) - 1)
    assert_eq(%s.%sFixedLoad(back, ragged, plan, r), -1, "ragged")
    assert_true(r.malformed and r.reason == "truncated", "ragged truncated")

    // 11. batch_too_large -> batch_too_large
    let empty_values:[%s.%s] = []
    assert_eq(%s.%sFixedLoad(empty_values, valid, plan, r), -1, "batch_too_large")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "batch_too_large" and not r.malformed, "batch_too_large reason")

    // 12. per-record hash mismatch -> no_layout
    let rec_at = 20 + read_uint32_le(valid, 16)
    let bad_rec = write_int64_le(substring(valid, 0, length(valid)), rec_at, ~read_uint64_le(valid, rec_at))
    assert_eq(%s.%sFixedLoad(back, bad_rec, plan, r), -1, "no_layout")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "no_layout" and not r.malformed, "no_layout reason")

    // 13. FixedSaveChecked short buffer -> truncated
    let n_save, _, why_save = %s.%sFixedSaveChecked(back, schema_rt.zeros(4))
    assert_true(n_save == -1 and why_save == "truncated", "FixedSaveChecked truncated")

    print "PASS 13"
`, pkg, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table, pkg, table)
	out := runLobsterVersionProbe(t, newer, []string{older}, 0, body, map[string][]byte{"valid.bin": valid})
	if strings.TrimSpace(out) != "PASS 13" {
		t.Fatalf("header/batch/save refusals: got %q", out)
	}

	refusalSchema := `package rprobe
fixed table CheckedHost | integrity = "crc32c" {
    lead   uint32 = 1
    marks  [..4]int32
    label  string(8)
    val    int32 | min = 0, max = 100
    trail  uint32 = 2
}
`
	body2 := `
    let good = rprobe.NewCheckedHost()
    good.Lead = 10
    good.MarksCount = 2
    good.Label = "abc"
    good.LabelLength = 3
    good.Val = 50
    good.Trail = 20
    good.Marks = [11, 22, 0, 0]
    let need = rprobe.CheckedHostFixedMeasure(1)
    let saved_n, wire, save_why = rprobe.CheckedHostFixedSaveChecked([good], schema_rt.zeros(need))
    assert_true(saved_n == need and save_why == "", "good CheckedHostFixedSaveChecked: " + save_why)

    // 14. invalid_value on FixedSaveChecked
    let bad_val = rprobe.NewCheckedHost()
    bad_val.Lead = 10
    bad_val.MarksCount = 2
    bad_val.Label = "abc"
    bad_val.LabelLength = 3
    bad_val.Val = 200
    bad_val.Trail = 20
    bad_val.Marks = [11, 22, 0, 0]
    let nv, _, wv = rprobe.CheckedHostFixedSaveChecked([bad_val], schema_rt.zeros(need))
    assert_true(nv == -1 and wv == "invalid_value", "invalid_value range")

    // 15. integrity_mismatch on corrupted CRC32 trailer
    let bad_crc = write_int8_le(substring(wire, 0, length(wire)), length(wire) - 1, read_uint8_le(wire, length(wire) - 1) ^ 0xFF)
    let back:[rprobe.CheckedHost] = [ rprobe.NewCheckedHost() ]
    let r = schema_rt.TableReport {}
    let plan = rprobe.make_plan(256)
    assert_eq(rprobe.CheckedHostFixedLoad(back, bad_crc, plan, r), -1, "integrity_mismatch")
    assert_true(r.verdict == schema_rt.TableOpenDamaged and r.malformed and r.reason == "integrity_mismatch", "integrity_mismatch report")

    let rec_body = 20 + read_uint32_le(wire, 16) + 8

    // 16. count_over_bound
    let f_cnt = write_int32_le(substring(wire, 0, length(wire)), rec_body + 4, 5)
    let ign_1, f_cnt_sealed = schema_rt.fixed_integrity_seal(f_cnt, need - 4)
    assert_eq(ign_1, need, "seal 1")
    assert_eq(rprobe.CheckedHostFixedLoad(back, f_cnt_sealed, plan, r), -1, "count_over_bound")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "count_over_bound", "count_over_bound reason")

    // 17. length_over_bound (string length 9 > 8)
    let f_len = write_int32_le(substring(wire, 0, length(wire)), rec_body + 24, 9)
    let ign_2, f_len_sealed = schema_rt.fixed_integrity_seal(f_len, need - 4)
    assert_eq(ign_2, need, "seal 2")
    assert_eq(rprobe.CheckedHostFixedLoad(back, f_len_sealed, plan, r), -1, "length_over_bound")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "length_over_bound", "length_over_bound reason")

    // 18. text_ill_formed (invalid UTF-8 0xFF inside label[0..3])
    let f_utf8 = write_int8_le(substring(wire, 0, length(wire)), rec_body + 28, 0xFF)
    let ign_3, f_utf8_sealed = schema_rt.fixed_integrity_seal(f_utf8, need - 4)
    assert_eq(ign_3, need, "seal 3")
    assert_eq(rprobe.CheckedHostFixedLoad(back, f_utf8_sealed, plan, r), -1, "text_ill_formed utf8")
    assert_true(r.verdict == schema_rt.TableOpenRefused and r.reason == "text_ill_formed", "text_ill_formed utf8 reason")

    // 19. clamped on strict FixedLoad vs FixedLoadTolerant (val = 150 > 100)
    let f_clamp = write_int32_le(substring(wire, 0, length(wire)), rec_body + 36, 150)
    let ign_5, f_clamp_sealed = schema_rt.fixed_integrity_seal(f_clamp, need - 4)
    assert_eq(ign_5, need, "seal 5")
    assert_eq(rprobe.CheckedHostFixedLoadTolerant(back, f_clamp_sealed, plan, r), 1, "tolerant clamp returns 1")
    assert_true(r.clamped == 1 and back[0].Val == 100, "tolerant clamped Val == 100")
    assert_eq(rprobe.CheckedHostFixedLoad(back, f_clamp_sealed, plan, r), -1, "strict FixedLoad refuses clamped")
    assert_true(r.clamped == 1 and schema_rt.fixed_report_err(r) == "clamped" and back[0].Val == 0, "strict FixedLoad resets destination on clamp")

    // 20. layout_record_too_large via fixed_lineage_plans
    let mine, pwhy = schema_rt.fixed_parse_layout(rprobe.CheckedHostFixedLayoutBytes())
    assert_true(pwhy == "", "parse own layout")
    let root_id = schema_rt.fixed_entry_at(mine, 0).id
    let lead_id = schema_rt.fixed_entry_at(mine, 1).id
    let marks_id = schema_rt.fixed_entry_at(mine, 2).id
    let elem_id = schema_rt.fixed_entry_at(mine, 3).id
    var h_layout = write_int32_le(schema_rt.zeros(4 + 4 * 17), 0, 4)
    h_layout = write_int64_le(h_layout, 4, root_id)
    h_layout = write_int8_le(h_layout, 12, 13)
    h_layout = write_int32_le(h_layout, 13, 4)
    h_layout = write_int32_le(h_layout, 17, 2)
    h_layout = write_int64_le(h_layout, 21, lead_id)
    h_layout = write_int8_le(h_layout, 29, 8)
    h_layout = write_int32_le(h_layout, 30, 4)
    h_layout = write_int32_le(h_layout, 34, 0)
    h_layout = write_int64_le(h_layout, 38, marks_id)
    h_layout = write_int8_le(h_layout, 46, 14)
    h_layout = write_int32_le(h_layout, 47, 0)
    h_layout = write_int32_le(h_layout, 51, 1)
    h_layout = write_int64_le(h_layout, 55, elem_id)
    h_layout = write_int8_le(h_layout, 63, 4)
    h_layout = write_int32_le(h_layout, 64, 4)
    h_layout = write_int32_le(h_layout, 68, 0)
    let lanes = schema_rt.fixed_lineage_plans([schema_rt.TableFixedKnownLayout { hash: 0xBEEF, layout: h_layout, layout_bytes: length(h_layout), record: 12 }], rprobe.CheckedHostFixedLayoutBytes(), rprobe.CheckedHostFixedDst, rprobe.CheckedHostFixedHash)
    assert_true(lanes[0].why == "layout_record_too_large", "layout_record_too_large")

    // 21..26. §1.1 layout parser refusal names
    let pv1, pw1 = schema_rt.fixed_parse_layout(schema_rt.zeros(4))
    assert_true(pv1.count == 0 and pw1 == "layout_count_mismatch", "layout_count_mismatch")
    var bad_root = write_int32_le(schema_rt.zeros(4 + 17), 0, 1)
    bad_root = write_int8_le(bad_root, 12, 4)
    bad_root = write_int32_le(bad_root, 13, 4)
    let pv2, pw2 = schema_rt.fixed_parse_layout(bad_root)
    assert_true(pv2.count == 0 and pw2 == "layout_kind_invalid", "layout_kind_invalid")
    var bad_kind = substring(h_layout, 0, length(h_layout))
    bad_kind = write_int8_le(bad_kind, 29, 99)
    let pv3, pw3 = schema_rt.fixed_parse_layout(bad_kind)
    assert_true(pv3.count == 0 and pw3 == "layout_kind_unknown", "layout_kind_unknown")
    var bad_size = substring(h_layout, 0, length(h_layout))
    bad_size = write_int32_le(bad_size, 30, 8)
    let pv4, pw4 = schema_rt.fixed_parse_layout(bad_size)
    assert_true(pv4.count == 0 and pw4 == "layout_size_mismatch", "layout_size_mismatch")
    var bad_tree = substring(h_layout, 0, length(h_layout))
    bad_tree = write_int32_le(bad_tree, 17, 1)
    let pv5, pw5 = schema_rt.fixed_parse_layout(bad_tree)
    assert_true(pv5.count == 0 and pw5 == "layout_tree_unclosed", "layout_tree_unclosed")
    var deep_layout = write_int32_le(schema_rt.zeros(4 + 66 * 17), 0, 66)
    for(65) d_i:
        let off = 4 + d_i * 17
        deep_layout = write_int64_le(deep_layout, off, d_i + 1)
        deep_layout = write_int8_le(deep_layout, off + 8, 13)
        deep_layout = write_int32_le(deep_layout, off + 9, 4)
        deep_layout = write_int32_le(deep_layout, off + 13, 1)
    let leaf_off = 4 + 65 * 17
    deep_layout = write_int64_le(deep_layout, leaf_off, 999)
    deep_layout = write_int8_le(deep_layout, leaf_off + 8, 4)
    deep_layout = write_int32_le(deep_layout, leaf_off + 9, 4)
    deep_layout = write_int32_le(deep_layout, leaf_off + 13, 0)
    let pv6, pw6 = schema_rt.fixed_parse_layout(deep_layout)
    assert_true(pv6.count == 0 and pw6 == "layout_too_deep", "layout_too_deep")

    print "PASS 26"
`
	out2 := runLobsterVersionProbe(t, refusalSchema, nil, 0, body2, nil)
	if strings.TrimSpace(out2) != "PASS 26" {
		t.Fatalf("text/count/clamp/integrity/record_too_large/parse_layout refusals: got %q", out2)
	}
	t.Logf("fixed-form refusals verified by Go name: 26")
}

// ---- helpers ----------------------------------------------------------------

// lobsterFixedCorpus is the C++ reference's byte oracle under the same
// contract as fixedform_test.go's fixedCorpusDir (and ci-fast.yml's go-test-touched job): a tree
// that never built it skips by name, SCHEMA_REQUIRE_CORPUS (set by `make
// test-lobster` after it builds the corpus) makes its absence a failure, and
// a corpus that is present but lacks the evolution files is always a failure
// (a stale oracle, never a quiet skip).
func lobsterFixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		t.Fatalf("the reference's byte oracle in %s has no evolution files: rebuild it with make tables-fixedform-corpus (%v)", dir, err)
	}
	return dir
}

func lobsterReadSchema(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", base+".schema")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func lobsterUnitOf(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func lobsterFixedRoot(t *testing.T, src string) *ir.Struct {
	t.Helper()
	u := lobsterUnitOf(t, src)
	roots := ir.TableFixedRoots(u)
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

func lobsterHasField(st *ir.Struct, name string) bool {
	for _, f := range st.Fields {
		if f != nil && f.Name == name {
			return true
		}
	}
	return false
}

func lobsterBrackets(t *testing.T, corpus, row string, bodyBytes int64) (uint32, uint32) {
	t.Helper()
	if lead, trail, ok := lobsterManifestBrackets(t, corpus, "old_"+row+".bin"); ok {
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

func lobsterManifestBrackets(t *testing.T, corpus, file string) (uint32, uint32, bool) {
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
		values, ok := lobsterManifestRow(line, file)
		if !ok {
			continue
		}
		lead, haveLead := values["r0.lead"]
		trail, haveTrail := values["r0.trail"]
		if !haveLead || !haveTrail {
			return 0, 0, false
		}
		return uint32(lead), uint32(trail), true
	}
	t.Fatalf("the manifest carries no row for %s", file)
	return 0, 0, false
}

func lobsterManifestRow(line, file string) (map[string]uint64, bool) {
	var values string
	named := false
	for field := range strings.FieldsSeq(line) {
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
	for pair := range strings.SplitSeq(values, ",") {
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

func lobsterManifestFloatBits(t *testing.T, corpus, file, section, path string) uint32 {
	t.Helper()
	f, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is not readable: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		var values string
		named := false
		for field := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				named = v == file
			case section:
				values = v
			}
		}
		if !named {
			continue
		}
		for pair := range strings.SplitSeq(values, ",") {
			k, v, ok := strings.Cut(pair, "=")
			if !ok || (k != path && !strings.HasPrefix(k, path+"@")) {
				continue
			}
			_, hex, ok := strings.Cut(v, "|0x")
			if !ok {
				t.Fatalf("manifest %s for %s is %q (no |0x<bits>)", path, file, v)
			}
			bits, err := strconv.ParseUint(hex, 16, 32)
			if err != nil {
				t.Fatalf("manifest %s for %s has unreadable bits %q: %v", path, file, hex, err)
			}
			return uint32(bits)
		}
		t.Fatalf("manifest %s for %s carries no %s", section, file, path)
	}
	t.Fatalf("manifest carries no row for %s", file)
	return 0
}

func runLobsterVersionProbe(t *testing.T, reader string, older []string, retire int, body string, inputs map[string][]byte) string {
	t.Helper()
	u := lobsterUnitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, src := range older {
		o := lobsterUnitOf(t, src)
		for _, st := range ir.TableFixedRoots(o) {
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
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := fmt.Sprintf(`import .ProbeTable
import .schema_runtime

namespace %s

def make_plan(n:int) -> [schema_rt.TableFixedEntry]:
    let out:[schema_rt.TableFixedEntry] = []
    for(n): push(out, schema_rt.TableFixedEntry {})
    return out

namespace main_ns

def read_in(name:string) -> string:
    let f = read_file(name)
    if f: return f
    return ""

def assert_true(cond:bool, msg:string):
    if not cond:
        print "FAIL: " + msg
        assert false

def assert_eq(got:int, want:int, msg:string):
    if got != want:
        print "FAIL: " + msg + " got=" + string(got) + " want=" + string(want)
        assert false

def run():
%s

run()
`, u.Package, body)
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(lobsterrun.Run(t, dir, "main.lobster", inputs))
}

// corpusNonFinite reports whether the oracle's manifest says file carries a
// NaN or an infinity, and names the first such field (its last path
// component): the C++ dump spells one `nan|0x…` or `inf|0x…`
// (docs/FIXED-FORM-ALGORITHM.md §5.7). A finite-only corpus answers false and
// the row is compared byte for byte.
func corpusNonFinite(t *testing.T, corpus, file string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "file="+file+" ") {
			continue
		}
		_, values, _ := strings.Cut(line, " values=")
		for kv := range strings.SplitSeq(values, ",") {
			k, v, _ := strings.Cut(kv, "=")
			if strings.HasPrefix(v, "nan|") || strings.HasPrefix(v, "inf|") || strings.HasPrefix(v, "-inf|") || strings.HasPrefix(v, "+inf|") {
				if i := strings.LastIndex(k, "."); i >= 0 {
					k = k[i+1:]
				}
				return k, true
			}
		}
	}
	return "", false
}
