package gotable

import (
	"fmt"
	"testing"
)

// The crashers are the low and high LoadoutConfig samples of §16.11 with the
// enum field's variant reference byte (offset 3: ref 1, kind 30, payload) set
// to 0x00. Reference 0 is the enum's None, a valid spelling of a different
// value, and the id table's first-use order changes with it. The 0x03 byte
// the card names is NOT this crasher: entry 3 is a field-name id, so the read
// counts one unknown and is not silent. Both facts are pinned here, because
// docs/SPEC-TABLES.md §3 and §16.11 state them: a variant reference resolves
// BY ENTRY, and a same-length swap to another valid value is outside a fuzz
// target's assertion unless the table declares integrity. This is case B of
// the card: harness, not reader, so no reader line changes.
const lowLoadoutJSON = `{"grade":"Bronze","grades":[],"podium":["Bronze","Bronze","Bronze"],"perks":[],"primary":{"damage":-1.5,"speed":-1.5,"penetration":0,"channel":0,"homing":false,"effect":{"buff":{"multiplier":-1.5}}},"backups":[{"damage":-1.5,"speed":-1.5,"penetration":0,"channel":0,"homing":false,"effect":{"buff":{"multiplier":-1.5}}},{"damage":-1.5,"speed":-1.5,"penetration":0,"channel":0,"homing":false,"effect":{"buff":{"multiplier":-1.5}}}],"attachments":[]}`

const highLoadoutJSON = `{"grade":"Gold","grades":["Gold","Gold","Gold","Gold"],"podium":["Gold","Gold","Gold"],"perks":["Shielded","Cloaked","Turbo"],"primary":{"damage":1.5,"speed":1.5,"penetration":10,"channel":63,"homing":true,"effect":{"buff":{"multiplier":1.5}}},"backups":[{"damage":1.5,"speed":1.5,"penetration":10,"channel":63,"homing":true,"effect":{"buff":{"multiplier":1.5}}},{"damage":1.5,"speed":1.5,"penetration":10,"channel":63,"homing":true,"effect":{"buff":{"multiplier":1.5}}}],"attachments":[{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5}]}`

// The two crashers pinned as literal hex: the LoadoutConfig wires above with
// the enum field's variant reference byte (offset 3: reference 1, payload kind
// 30) set to 0x00, the enum's None.
const lowCrasherHex = "01011e00030e051e03020202040d1f050a0000c0bf060a0000c0bf070400000000080f090d070a0a0000c0bf00000b0e420d021f050a0000c0bf060a0000c0bf070400000000080f090d070a0a0000c0bf00001f050a0000c0bf060a0000c0bf070400000000080f090d070a0a0000c0bf000000d48ac477299ba83291c5c4aa397792c44925d7235af46fb439e983f77f1331bfc07fb38abe08637fd2b1ac025705331732e9139e30c5314f9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9a24cc8a11f5f028de0b00000000000000"

const highCrasherHex = "01011e00030e061e0402020202040e051e0302020205090700000000000000060d25070a0000c03f080a0000c03f09040a0000000a063f0b01010c0f0d0d070e0a0000c03f00000f0e4e0d0225070a0000c03f080a0000c03f09040a0000000a063f0b01010c0f0d0d070e0a0000c03f000025070a0000c03f080a0000c03f09040a0000000a063f0b01010c0f0d0d070e0a0000c03f0000100e720d080d110407000000120a0000c03f000d110407000000120a0000c03f000d110407000000120a0000c03f000d110407000000120a0000c03f000d110407000000120a0000c03f000d110407000000120a0000c03f000d110407000000120a0000c03f000d110407000000120a0000c03f0000d48ac477299ba832b318320d7ec916c4c599f782764e0ad94925d7235af46fb44ae5967084f5064b39e983f77f1331bfc07fb38abe08637fd2b1ac025705331732e9139e30c5314fa4edcad59a3e01a5593f2e60bc8765ad9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9a24cc8a11f5f028de419a244003aa01f9d131fef61816776ae6b4e78a35d1f9ee1200000000000000"

func enumVariantRefSortProbe(t *testing.T, low, high string) {
	t.Helper()
	src := fmt.Sprintf(`package tabledemo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

const lowJSON = %q
const highJSON = %q

const lowPinned = %q
const highPinned = %q

func refSortBuild(t *testing.T, text string) []byte {
	t.Helper()
	var v LoadoutConfig
	var r TableReport
	if !LoadoutConfigFromJson(&v, []byte(text), &r) || r != (TableReport{}) {
		t.Fatalf("sample text: %%+v", r)
	}
	w := make([]byte, max(LoadoutConfigMeasure(&v), 0))
	if LoadoutConfigSave(&v, w) != int64(len(w)) || len(w) == 0 {
		t.Fatal("sample save")
	}
	return w
}

func refSortSave(t *testing.T, v *LoadoutConfig) []byte {
	t.Helper()
	w := make([]byte, max(LoadoutConfigMeasure(v), 0))
	if LoadoutConfigSave(v, w) != int64(len(w)) {
		t.Fatal("save of the loaded value")
	}
	return w
}

func refSortEntries(w []byte) [][]byte {
	n := int(binary.LittleEndian.Uint64(w[len(w)-8:]))
	start := len(w) - 8 - n*8
	out := make([][]byte, n)
	for i := range out {
		out[i] = w[start+i*8 : start+i*8+8]
	}
	return out
}

func refSortEntryCount(w []byte) byte { return byte(binary.LittleEndian.Uint64(w[len(w)-8:])) }

// refSortGrade returns the grade field's enum ordinal (0 is None).
func refSortGrade(t *testing.T, v *LoadoutConfig) uint64 {
	t.Helper()
	return uint64(v.Grade)
}

func TestGoWireEnumVariantRefSort(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, text string; length int }{
		{"low_212", lowJSON, 212},
		{"high_422", highJSON, 422},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := refSortBuild(t, tc.text)
			if len(base) != tc.length {
				t.Fatalf("sample wire is %%d bytes, want %%d", len(base), tc.length)
			}
			// The unmutated sample reads clean and re-encodes byte for byte.
			var clean TableReport
			if !LoadoutConfigLoad(new(LoadoutConfig), base, &clean) || clean != (TableReport{}) {
				t.Fatalf("clean tolerant read: %%+v", clean)
			}
			cleanValue, err := LoadoutConfigReadStrict(base)
			if err != nil || cleanValue == nil {
				t.Fatalf("clean strict read: %%v", err)
			}
			if got := refSortSave(t, cleanValue); !bytes.Equal(got, base) {
				t.Fatalf("clean sample re-encodes to %%x, want %%x", got, base)
			}

			// The crasher: 0x02 over the variant reference becomes 0x00, the
			// enum's None. It is a valid spelling of a different value, so the
			// tolerant report is silent and no reader can tell it from a writer.
			none := bytes.Clone(base)
			none[3] = 0x00
			pinned := lowPinned
			if tc.name == "high_422" {
				pinned = highPinned
			}
			if got := fmt.Sprintf("%%x", none); got != pinned {
				t.Fatalf("crasher bytes %%s do not match the pinned literal hex", got)
			}
			var silent TableReport
			if !LoadoutConfigLoad(new(LoadoutConfig), none, &silent) {
				t.Fatalf("None reference: read stopped, %%+v", silent)
			}
			if silent != (TableReport{}) {
				t.Fatalf("None reference: report %%+v, want the silent class", silent)
			}
			noneValue, err := LoadoutConfigReadStrict(none)
			if err != nil || noneValue == nil {
				t.Fatalf("None reference strict read: %%v", err)
			}
			if refSortGrade(t, noneValue) != 0 {
				t.Fatalf("None reference read as grade %%d, want None", refSortGrade(t, noneValue))
			}
			again := refSortSave(t, noneValue)
			if bytes.Equal(again, none) {
				t.Fatal("None reference re-encoded byte for byte; the first-use renumbering did not happen")
			}
			if len(again) != len(none) {
				t.Fatalf("None reference re-encoded to %%d bytes, want the same %%d", len(again), len(none))
			}
			before, after := refSortEntries(none), refSortEntries(again)
			if !bytes.Equal(before[1], after[2]) || !bytes.Equal(before[2], after[1]) {
				t.Fatalf("the id table did not swap entries 2 and 3: before %%x after %%x", before, after)
			}
			for i := range before {
				if i != 1 && i != 2 && !bytes.Equal(before[i], after[i]) {
					t.Fatalf("id table entry %%d moved: before %%x after %%x", i+1, before[i], after[i])
				}
			}

			// The card's 0x02 -> 0x03: entry 3 is a field-name id (podium in
			// low, grades in high), so the reader counts one unknown and reads
			// None. It is never the other variant and never silent.
			other := bytes.Clone(base)
			other[3] = 0x03
			var unknown TableReport
			if !LoadoutConfigLoad(new(LoadoutConfig), other, &unknown) {
				t.Fatalf("entry-3 reference: read stopped, %%+v", unknown)
			}
			if unknown.Unknown != 1 || unknown != (TableReport{Unknown: 1}) {
				t.Fatalf("entry-3 reference report %%+v, want exactly one unknown", unknown)
			}
			otherValue, err := LoadoutConfigReadStrict(other)
			if err != nil || otherValue == nil {
				t.Fatalf("entry-3 reference strict read: %%v", err)
			}
			if refSortGrade(t, otherValue) != 0 {
				t.Fatalf("entry-3 reference read as grade %%d, want None", refSortGrade(t, otherValue))
			}

			// A reference above the entry count is framing damage, as before.
			over := bytes.Clone(base)
			over[3] = refSortEntryCount(over) + 1
			var overReport TableReport
			if LoadoutConfigLoad(new(LoadoutConfig), over, &overReport) {
				t.Fatalf("reference past the table opened: %%+v", overReport)
			}
			if overValue, err := LoadoutConfigReadStrict(over); overValue != nil || !errors.Is(err, TableRefuseMalformed) {
				t.Fatalf("reference past the table: value %%v err %%v, want malformed", overValue, err)
			}
		})
	}
}
`, low, high, lowCrasherHex, highCrasherHex)
	if out, err := runGeneratedUnit(t, loadUnitPaths(t, "../../..", []string{"tables/examples"}), src, nil); err != nil {
		t.Fatalf("generated: %v\n%s", err, out)
	}
}

func TestGoWireEnumVariantRefSort(t *testing.T) {
	t.Parallel()
	enumVariantRefSortProbe(t, lowLoadoutJSON, highLoadoutJSON)
}
