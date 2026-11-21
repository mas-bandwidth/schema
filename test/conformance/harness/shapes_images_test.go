package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// GOOSE-3 (review 2026-11-21): a fixed-form image whose plain float32 or
// float64 field holds a NaN or an infinity. The FIXED read tests the raw bits
// for a non-finite value first (docs/SPEC-TABLES.md §3.3's special-values
// table, FIXED row) and refuses it `non_finite_float`, so every such image is
// a `wire-refused` instance. No writer makes one, so each is the shapes
// unit's fixed_ends.bin with one field's bytes replaced: `plain` holds
// float32 1.25 and `wide` float64 1.25 there, and each pattern occurs once.
var shapesNonFinite = []struct {
	name  string
	from  []byte
	bytes []byte
}{
	{"fixed_f32_nan", f32le(1.25), u32le(0x7FC00000)},
	{"fixed_f32_inf", f32le(1.25), u32le(0x7F800000)},
	{"fixed_f32_neginf", f32le(1.25), u32le(0xFF800000)},
	{"fixed_f32_signalling_nan", f32le(1.25), u32le(0x7F800001)},
	{"fixed_f64_nan", f64le(1.25), u64le(0x7FF8000000000000)},
	{"fixed_f64_inf", f64le(1.25), u64le(0x7FF0000000000000)},
	{"fixed_f64_neginf", f64le(1.25), u64le(0xFFF0000000000000)},
	// A COMPRESSED FLOAT32 AT EACH END AND ONE FLOAT32 STEP PAST IT. The
	// fixed form carries the float whole, and its read holds each end
	// against the writer's range at float32 (TABLEWIRE-1): float32(0.1) is
	// `max = 0.1` and reads, and the next float32 up is past it and refuses
	// `out_of_range`; float32(0.7) is `min = 0.7` and reads, and the next
	// float32 down refuses. fixed_ends.bin holds tenth 0.05 and seventh 0.75.
	{"fixed_tenth_at_max", f32le(0.05), f32le(0.1)},
	{"fixed_tenth_past_max", f32le(0.05), f32le(math.Nextafter32(0.1, 1))},
	{"fixed_seventh_at_min", f32le(0.75), f32le(0.7)},
	{"fixed_seventh_below_min", f32le(0.75), f32le(math.Nextafter32(0.7, 0))},
}

// shapesCountImage is fixed_octets_at_min.bin with the counted array's count
// one below its Min (CS-3): the one byte where it differs from
// fixed_octets_three.bin (the same value with one more element) is the count,
// stored whole, and it is set to Min - 1.
func shapesCountImage(atMin, three []byte) ([]byte, bool) {
	if len(atMin) != len(three) {
		return nil, false
	}
	at := -1
	for i := range atMin {
		if atMin[i] != three[i] {
			if at >= 0 {
				return nil, false
			}
			at = i
		}
	}
	if at < 0 || atMin[at] == 0 {
		return nil, false
	}
	out := append([]byte(nil), atMin...)
	out[at]--
	return out, true
}

func f32le(v float32) []byte { return u32le(math.Float32bits(v)) }
func f64le(v float64) []byte { return u64le(math.Float64bits(v)) }
func u32le(v uint32) []byte  { return binary.LittleEndian.AppendUint32(nil, v) }
func u64le(v uint64) []byte  { return binary.LittleEndian.AppendUint64(nil, v) }

const shapesWireDir = "wire/tables/shapes" // under the testdata tree

// shapesImage is one non-finite image: the base with the one occurrence of
// from replaced.
func shapesImage(base, from, to []byte) ([]byte, bool) {
	at := bytes.Index(base, from)
	if at < 0 || bytes.Contains(base[at+1:], from) {
		return nil, false
	}
	out := append([]byte(nil), base...)
	copy(out[at:], to)
	return out, true
}

func TestShapesImagesArePinned(t *testing.T) {
	root, err := treeRoot()
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join(root, shapesWireDir, "fixed_ends.bin"))
	if err != nil {
		t.Fatal(err)
	}
	atMin, err := os.ReadFile(filepath.Join(root, shapesWireDir, "fixed_octets_at_min.bin"))
	if err != nil {
		t.Fatal(err)
	}
	three, err := os.ReadFile(filepath.Join(root, shapesWireDir, "fixed_octets_three.bin"))
	if err != nil {
		t.Fatal(err)
	}
	images := map[string][]byte{}
	for _, img := range shapesNonFinite {
		want, ok := shapesImage(base, img.from, img.bytes)
		if !ok {
			t.Fatalf("%s: the base does not hold the field's pattern exactly once", img.name)
		}
		images[img.name] = want
	}
	below, ok := shapesCountImage(atMin, three)
	if !ok {
		t.Fatal("fixed_octets_below_min: the two bases do not differ in exactly one count byte")
	}
	images["fixed_octets_below_min"] = below
	write := os.Getenv("SHAPES_WRITE") == "1"
	for name, want := range images {
		path := filepath.Join(root, shapesWireDir, name+".bin")
		if write {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v — run with SHAPES_WRITE=1 to write it", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the file is not the generator's bytes — run with SHAPES_WRITE=1", name)
		}
	}
}
