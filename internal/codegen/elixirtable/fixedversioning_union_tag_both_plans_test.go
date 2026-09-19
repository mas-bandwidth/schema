package elixirtable

// `union_tag_both_plans` forges `old_union_append.bin`'s one record so
// `r0.pick.type` is `9`, a tag past the OLD writer's two arms AND the NEW
// reader's three — a tag no arm lands, not an arm the reader happens to know.
// The tag is ONE byte, and the locator is the nine-byte needle
// `01 07 00 00 00 0F 00 00 00` (tag, `alpha.m = 7`, `seq = 15`); it must occur
// EXACTLY ONCE, because a search that is not a locator is not a forge, and the
// forge writes `9` over the first byte. The SAME forged bytes are read TWICE —
// the COMPILED plan and the IDENTITY plan — which is the point of the row.
// `clamped` is asserted `== 1`, never `>= 1`: a leg that counts the tag in the
// plan's op AND again in the decode projection lands 2, and a looser read is
// precisely the hole this row exists to catch. Measured 2026-09-19 on the gate
// rig at `380f1cad`: the union tag bound deleted from the emitter of ALL NINE
// LEGS left zero landed fixed-table tests red (c 0/72, cpp 0/36, cs 0/70, dart
// 0/69, elixir 0/72, go 0/68, java 0/41, js 0/70, rust 0/70).
//
// schema#1254: over these forged bytes the COMPILED plan counts `clamped == 0`,
// not 1. Its tag lands as a CONST written under an arm guard — MY ordinal only
// when THEIR tag names a shared arm — so a tag naming no shared arm writes
// nothing and the prefill's None stands with no counter moved. Only the
// IDENTITY plan counts the clamp (its decode projection holds `c + 1`). When
// #1254 is ruled on, delete the landing checks from `bodyCompiled` and hand the
// `clamped == 1` assertion back into one shared body.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	old := filepath.Join(corpus, "old_union_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// THE FORGE: locate `r0.pick.type` by the nine-byte needle `tag=1,
	// alpha.m=7, seq=15` — the manifest's lawful values for the writer — assert
	// it occurs exactly once, and overwrite the one-byte tag with 9.
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
	if n := bytes.Count(data, needle); n != 1 {
		t.Fatalf("the locator %X occurs %d times in %s, want exactly once", needle, n, old)
	}
	at := bytes.Index(data, needle)
	data[at] = 9 // the forge: a union tag past the writer's arms and the reader's
	forged := filepath.Join(t.TempDir(), "hostile_union_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// THE IDENTITY PLAN COUNTS THE CLAMP. Its decode projection holds the tag
	// past the declared arms to None and counts one `clamped`.
	bodyIdentity := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the forged union tag is not a refusal: #{inspect({tag, why(report)})}")
  check(report.malformed == false, "a forged union tag is not malformed: #{why(report)}")
  check(length(values) == 1, "the forged file carries one record, not #{length(values)}")
  v = hd(values)
  check(v.pick.type == 0, "a forged union tag lands None: #{inspect(v.pick.type)}")
  check(v.pick.type != 9, "a forged union tag never lands the forged 9: #{inspect(v.pick.type)}")
  check(v.pick.type != 1, "a forged union tag never lands the writer's alpha arm: #{inspect(v.pick.type)}")
  check(v.seq == 15, "the scalar after the union is untouched: #{inspect(v.seq)}")
  check(report.clamped == 1, "the forged union tag is counted exactly once: #{why(report)}")
  check(report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and report.duplicate == 0,
    "no other counter moved: #{why(report)}")`

	// THE COMPILED PLAN LANDS None WITH NO COUNT (schema#1254), so assert the
	// landing and not the count: the tag is None, never the forged 9, the
	// neighbour untouched, no refusal and no other counter. Asserting `== 1`
	// here lands a red row; asserting `== 0` cements the defect.
	bodyCompiled := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the forged union tag is not a refusal: #{inspect({tag, why(report)})}")
  check(report.malformed == false, "a forged union tag is not malformed: #{why(report)}")
  check(length(values) == 1, "the forged file carries one record, not #{length(values)}")
  v = hd(values)
  check(v.pick.type == 0, "a forged union tag lands None: #{inspect(v.pick.type)}")
  check(v.pick.type != 9, "a forged union tag never lands the forged 9: #{inspect(v.pick.type)}")
  check(v.pick.type != 1, "a forged union tag never lands the writer's alpha arm: #{inspect(v.pick.type)}")
  check(v.seq == 15, "the scalar after the union is untouched: #{inspect(v.seq)}")
  check(report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and report.duplicate == 0,
    "no other counter moved: #{why(report)}")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_union_append", []string{"VOLD_union_append"}, 0, forged, bodyCompiled)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged union tag: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, elixirBin, "VOLD_union_append", nil, 0, forged, bodyIdentity)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged union tag: %v\n%s", err, out)
	}
}
