package mootable_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SKIP PATH HOLDS A PAYLOAD TO THE RULES A NAMED READ HOLDS IT TO
// (docs/SPEC-TABLES.md §3.3, §4; internal/tablewire messagedecode.go skip):
// build A writes a field build B does not declare, the batch is damaged in
// exactly one way inside that field's payload, and build B's reader must
// refuse the batch as the Go engine refuses it, though it keeps nothing of
// the field. Each case is one rule of the engine's skip; the undamaged batch
// is accepted by both, so the refusal is the damage's and nothing else's.
type skipCase struct {
	name, writer, json string
	// damage edits the batch's bit stream (the bytes after the form byte):
	// at is the bit the undeclared field's payload starts at, spots every
	// reference the undamaged batch carries, slot the field's own slot
	damage func(t *testing.T, stream []byte, at int, spots []tablewire.BitSpot, slot uint64)
}

// the reader every case reads into: it declares keep and nothing else
const skipReader = "package probe\nenum E { a, b, c, d, e }\nfixed table T\n{\n    keep uint8\n}\n"

func skipWriter(field string) string {
	return "package probe\nenum E { a, b, c, d, e }\nfixed table In\n{\n    x uint8\n    y uint8\n}\nfixed table T\n{\n    keep uint8\n    gone " + field + "\n}\n"
}

// putBits writes the low n bits of v at bit off, least significant first, the
// order the form's writer spells every value in.
func putBits(b []byte, off, n int, v uint64) {
	for i := range n {
		at := off + i
		if v>>uint(i)&1 == 1 {
			b[at/8] |= 1 << uint(at%8)
		} else {
			b[at/8] &^= 1 << uint(at%8)
		}
	}
}

// textByte is the bit of a text payload's first byte: the length's bits,
// then the align.
func textByte(at, lengthBits int) int {
	return (at + lengthBits + 7) / 8 * 8
}

var skipCases = []skipCase{
	{"text_not_utf8", skipWriter("string(8)"), `{"keep":1,"gone":"abcd"}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, textByte(at, 4), 8, 0xff)
		}},
	{"text_nul", skipWriter("string(8)"), `{"keep":1,"gone":"abcd"}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, textByte(at, 4), 8, 0)
		}},
	{"array_count_over_max", skipWriter("[..5]uint8 | min = 0, max = 0"), `{"keep":1,"gone":[0,0]}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, at, 3, 7)
		}},
	{"keyed_key_repeated", skipWriter("[E]uint8"), `{"keep":1,"gone":{"a":1,"b":2}}`,
		func(t *testing.T, s []byte, at int, spots []tablewire.BitSpot, slot uint64) {
			keys := spotsAfter(t, spots, at, 2)
			putBits(s, keys[1].Off, keys[1].Width, keys[0].Value)
		}},
	{"keyed_key_not_a_name", skipWriter("[E]uint8"), `{"keep":1,"gone":{"a":1,"b":2}}`,
		func(t *testing.T, s []byte, at int, spots []tablewire.BitSpot, slot uint64) {
			keys := spotsAfter(t, spots, at, 2)
			putBits(s, keys[1].Off, keys[1].Width, slot)
		}},
	{"f32_not_finite", skipWriter("float32"), `{"keep":1,"gone":1.5}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, at, 32, uint64(math.Float32bits(float32(math.Inf(1)))))
		}},
	{"f64_not_finite", skipWriter("float64"), `{"keep":1,"gone":1.5}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, at, 64, math.Float64bits(math.NaN()))
		}},
	{"quantized_index_over_count", skipWriter("float32 | min = 0, max = 1, resolution = 0.01"), `{"keep":1,"gone":0.5}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, at, 7, 127)
		}},
	{"ranged_past_its_kind", skipWriter("int8 | min = -100, max = 100"), `{"keep":1,"gone":5}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, at, 8, 255)
		}},
	{"f32_element_not_finite", skipWriter("[..4]float32"), `{"keep":1,"gone":[1.5,2.5]}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, at+3, 32, uint64(math.Float32bits(float32(math.Inf(-1)))))
		}},
	{"ranged_element_past_its_kind", skipWriter("[..4]int8 | min = -100, max = 100"), `{"keep":1,"gone":[5,6]}`,
		func(t *testing.T, s []byte, at int, _ []tablewire.BitSpot, _ uint64) {
			putBits(s, at+3, 8, 255)
		}},
	{"nested_field_repeated", skipWriter("In"), `{"keep":1,"gone":{"x":1,"y":2}}`,
		func(t *testing.T, s []byte, at int, spots []tablewire.BitSpot, _ uint64) {
			refs := spotsAfter(t, spots, at, 2)
			putBits(s, refs[1].Off, refs[1].Width, refs[0].Value)
		}},
}

// spotsAfter is the first n references at or after bit at.
func spotsAfter(t *testing.T, spots []tablewire.BitSpot, at, n int) []tablewire.BitSpot {
	t.Helper()
	var out []tablewire.BitSpot
	for _, s := range spots {
		if s.Off >= at && len(out) < n {
			out = append(out, s)
		}
	}
	if len(out) < n {
		t.Fatalf("the payload carries %d references, the case needs %d", len(out), n)
	}
	return out
}

// skipInputs is build A's announcement, its undamaged batch and the damaged
// one, and the Go engine's verdicts on both read into build B.
func skipInputs(t *testing.T, c skipCase) (ub *ir.Unit, ann, good, bad []byte, wantGood, wantBad string) {
	t.Helper()
	ua := unitFromSource(t, c.writer)
	ub = unitFromSource(t, skipReader)
	ma := tabletext.NewModel(ua)
	ia := ma.New(ua.Tables["T"])
	var ra tabletext.Report
	if !ma.Read(ia, []byte(c.json), &ra) || ra.Refused {
		t.Fatalf("the Go engine does not read the writer's JSON: %+v", ra)
	}
	good, err := tablewire.EncodeMessages(ma, []*tabletext.Instance{ia})
	if err != nil {
		t.Fatal(err)
	}
	ann = tablewire.Announce(ua)
	var vocab tablewire.Vocabulary
	var rv tabletext.Report
	if err := vocab.AnnounceRead(ann, &rv); err != nil {
		t.Fatal(err)
	}
	var gone *ir.Field
	for _, f := range ua.Tables["T"].Fields {
		if f.Name == "gone" {
			gone = f
		}
	}
	var slot uint64
	for i, e := range tablewire.MessageVocabulary(ua) {
		if e.Id == ir.TableFieldWireId(gone) && e.Kind != 0 {
			slot = uint64(i + 1)
		}
	}
	v := vocab
	spots := tablewire.MessageSpots(ma, ua.Tables["T"], good, &v)
	at := -1
	for _, s := range spots {
		if s.Value == slot && at < 0 {
			at = s.Off + s.Width
		}
	}
	if slot == 0 || at < 0 {
		t.Fatalf("the batch carries no reference to the undeclared field (slot %d)", slot)
	}
	bad = append([]byte(nil), good...)
	c.damage(t, bad[1:], at, spots, slot)
	verdict := func(msg []byte) string {
		mb := tabletext.NewModel(ub)
		ib := mb.New(ub.Tables["T"])
		var rb tabletext.Report
		vv := vocab
		if _, _, err := tablewire.DecodeMessages(mb, []*tabletext.Instance{ib}, msg, &vv, &rb); err != nil {
			return "refuse"
		}
		file, err := tablewire.Encode(mb, ib)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("accept %x", file)
	}
	return ub, ann, good, bad, verdict(good), verdict(bad)
}

func TestMooMessageSkipHoldsTheEnginesRules(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	for _, c := range skipCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ub, ann, good, bad, wantGood, wantBad := skipInputs(t, c)
			if !strings.HasPrefix(wantGood, "accept") || wantBad != "refuse" {
				t.Fatalf("the Go engine answers %s undamaged and %s damaged: the case is not one rule of its skip", wantGood, wantBad)
			}
			files, err := generate(ub)
			if err != nil {
				t.Fatal(err)
			}
			if got := runEvolve(t, ub, files, "T", ann, good); got != wantGood {
				t.Fatalf("the undamaged batch:\nmoo: %s\ngo:  %s", got, wantGood)
			}
			if got := runEvolve(t, ub, files, "T", ann, bad); got != "refuse" {
				t.Fatalf("the damaged batch: moo answers %s, the Go engine refuses", got)
			}
		})
	}
}
