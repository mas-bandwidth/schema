// Fuzz targets over the generated FIXED-form table readers. The property for
// every input: the read never panics, it never reads past its input (each
// target reads a fresh exact-length copy, and -race's checkptr refuses an
// unsafe read outside it), and an accepted value's re-save, read back by the
// same entry point, is equal. A crash is a defect in the reader, never in
// this harness.
package tabledemo_test

import (
	"bytes"
	"reflect"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

// fixedValueCount bounds the destination slice a fuzz input may fill. Every
// record is at least nine bytes, so a larger slice can never accept more
// records than this.
const fixedValueCount = 64

type fixedTarget struct {
	name    string
	zero    func() any
	away    func() any
	blank   func(int) any
	pair    func(any, any) any
	slice   func(any, int64) any
	plan    func() []tabledemo.TableFixedEntry
	load    func(any, []byte, []tabledemo.TableFixedEntry, *tabledemo.TableReport) int64
	save    func(any, []byte) int64
	measure func(int64) int64
	json    func(any) ([][]byte, bool)
}

// fixedOf adapts a `<T>FixedLoad(values, data, plan, report)` entry point.
func fixedOf[T any](name string, reset func(*T), planMax int32,
	measure func(int64) int64,
	save func([]T, []byte) int64,
	load func([]T, []byte, []tabledemo.TableFixedEntry, *tabledemo.TableReport) int64,
	jsonMeasure func(*T) int64,
	jsonDump func(*T, []byte) int64) fixedTarget {
	return fixedTarget{
		name: name,
		zero: func() any {
			v := new(T)
			reset(v)
			return *v
		},
		away: func() any {
			v := new(T)
			reset(v)
			fillAway(reflect.ValueOf(v).Elem())
			return *v
		},
		blank: func(n int) any { return make([]T, n) },
		pair:  func(first, second any) any { return []T{first.(T), second.(T)} },
		slice: func(values any, n int64) any { return values.([]T)[:n] },
		plan: func() []tabledemo.TableFixedEntry {
			return make([]tabledemo.TableFixedEntry, int(planMax))
		},
		load: func(values any, data []byte, plan []tabledemo.TableFixedEntry, report *tabledemo.TableReport) int64 {
			return load(values.([]T), data, plan, report)
		},
		save:    func(values any, buffer []byte) int64 { return save(values.([]T), buffer) },
		measure: measure,
		json: func(values any) ([][]byte, bool) {
			vs := values.([]T)
			text := make([][]byte, len(vs))
			for i := range vs {
				size := jsonMeasure(&vs[i])
				if size < 0 {
					return nil, false
				}
				text[i] = make([]byte, size)
				if n := jsonDump(&vs[i], text[i]); n != size {
					return nil, false
				}
			}
			return text, true
		},
	}
}

var fixedTargets = []fixedTarget{
	fixedOf("TeamConfig", tabledemo.TeamConfigReset, tabledemo.TeamConfigFixedPlanMax,
		tabledemo.TeamConfigFixedMeasure, tabledemo.TeamConfigFixedSave, tabledemo.TeamConfigFixedLoad,
		tabledemo.TeamConfigToJsonMeasure, tabledemo.TeamConfigToJson),
	fixedOf("GunnerConfig", tabledemo.GunnerConfigReset, tabledemo.GunnerConfigFixedPlanMax,
		tabledemo.GunnerConfigFixedMeasure, tabledemo.GunnerConfigFixedSave, tabledemo.GunnerConfigFixedLoad,
		tabledemo.GunnerConfigToJsonMeasure, tabledemo.GunnerConfigToJson),
	fixedOf("TurretConfig", tabledemo.TurretConfigReset, tabledemo.TurretConfigFixedPlanMax,
		tabledemo.TurretConfigFixedMeasure, tabledemo.TurretConfigFixedSave, tabledemo.TurretConfigFixedLoad,
		tabledemo.TurretConfigToJsonMeasure, tabledemo.TurretConfigToJson),
	fixedOf("HullConfig", tabledemo.HullConfigReset, tabledemo.HullConfigFixedPlanMax,
		tabledemo.HullConfigFixedMeasure, tabledemo.HullConfigFixedSave, tabledemo.HullConfigFixedLoad,
		tabledemo.HullConfigToJsonMeasure, tabledemo.HullConfigToJson),
	fixedOf("KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigFixedPlanMax,
		tabledemo.KeyedConfigFixedMeasure, tabledemo.KeyedConfigFixedSave, tabledemo.KeyedConfigFixedLoad,
		tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson),
	fixedOf("GunnerSettings", tabledemo.GunnerSettingsReset, tabledemo.GunnerSettingsFixedPlanMax,
		tabledemo.GunnerSettingsFixedMeasure, tabledemo.GunnerSettingsFixedSave, tabledemo.GunnerSettingsFixedLoad,
		tabledemo.GunnerSettingsToJsonMeasure, tabledemo.GunnerSettingsToJson),
	fixedOf("ShipEntry", tabledemo.ShipEntryReset, tabledemo.ShipEntryFixedPlanMax,
		tabledemo.ShipEntryFixedMeasure, tabledemo.ShipEntryFixedSave, tabledemo.ShipEntryFixedLoad,
		tabledemo.ShipEntryToJsonMeasure, tabledemo.ShipEntryToJson),
	fixedOf("GlobalSettings", tabledemo.GlobalSettingsReset, tabledemo.GlobalSettingsFixedPlanMax,
		tabledemo.GlobalSettingsFixedMeasure, tabledemo.GlobalSettingsFixedSave, tabledemo.GlobalSettingsFixedLoad,
		tabledemo.GlobalSettingsToJsonMeasure, tabledemo.GlobalSettingsToJson),
	fixedOf("PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigFixedPlanMax,
		tabledemo.PackConfigFixedMeasure, tabledemo.PackConfigFixedSave, tabledemo.PackConfigFixedLoad,
		tabledemo.PackConfigToJsonMeasure, tabledemo.PackConfigToJson),
	fixedOf("RangedSigned", tabledemo.RangedSignedReset, tabledemo.RangedSignedFixedPlanMax,
		tabledemo.RangedSignedFixedMeasure, tabledemo.RangedSignedFixedSave, tabledemo.RangedSignedFixedLoad,
		tabledemo.RangedSignedToJsonMeasure, tabledemo.RangedSignedToJson),
	fixedOf("RangedUnsigned", tabledemo.RangedUnsignedReset, tabledemo.RangedUnsignedFixedPlanMax,
		tabledemo.RangedUnsignedFixedMeasure, tabledemo.RangedUnsignedFixedSave, tabledemo.RangedUnsignedFixedLoad,
		tabledemo.RangedUnsignedToJsonMeasure, tabledemo.RangedUnsignedToJson),
	fixedOf("RangedWidths", tabledemo.RangedWidthsReset, tabledemo.RangedWidthsFixedPlanMax,
		tabledemo.RangedWidthsFixedMeasure, tabledemo.RangedWidthsFixedSave, tabledemo.RangedWidthsFixedLoad,
		tabledemo.RangedWidthsToJsonMeasure, tabledemo.RangedWidthsToJson),
}

func FuzzFixedLoad(f *testing.F) {
	seedFixedTargets(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		target := fixedTargets[int(data[0])%len(fixedTargets)]
		wire := append([]byte(nil), data[1:]...)
		count := max(min(len(wire), fixedValueCount), 1)
		values := target.blank(count)
		n := target.load(values, wire, target.plan(), &tabledemo.TableReport{})
		if n < 0 {
			return
		}
		reSaveFixed(t, target, target.slice(values, n), n)
	})
}

// reSaveFixed is the accepted-value half of the property: the value the reader
// accepted saves in the same writer image, and reading that image back through
// the same entry point yields the same JSON text.
func reSaveFixed(t *testing.T, target fixedTarget, accepted any, n int64) {
	t.Helper()
	size := target.measure(n)
	if size < 0 {
		t.Fatalf("%s: %d accepted values do not measure", target.name, n)
	}
	wire := make([]byte, size)
	if got := target.save(accepted, wire); got != size {
		t.Fatalf("%s: measure said %d bytes for %d accepted values, save wrote %d", target.name, size, n, got)
	}
	again := target.blank(int(n))
	m := target.load(again, wire, target.plan(), &tabledemo.TableReport{})
	if m != n {
		t.Fatalf("%s: %d accepted values re-save to %d bytes and re-read as %d", target.name, n, size, m)
	}
	second := target.slice(again, m)
	first, firstOK := target.json(accepted)
	secondText, secondOK := target.json(second)
	if firstOK && secondOK {
		if len(first) != len(secondText) {
			t.Fatalf("%s: re-save round trip has %d values, want %d", target.name, len(secondText), len(first))
		}
		for i := range first {
			if !bytes.Equal(first[i], secondText[i]) {
				t.Fatalf("%s: re-save round trip differs at value %d\n first: %s\nsecond: %s",
					target.name, i, first[i], secondText[i])
			}
		}
		return
	}
	// A wire-legal value may hold a float JSON cannot spell (NaN, an
	// infinity) or string bytes that are not UTF-8; ToJson refuses those.
	// The re-save equality is still checked, on the writer image itself.
	againImage := make([]byte, size)
	if got := target.save(second, againImage); got != size || !bytes.Equal(wire, againImage) {
		t.Fatalf("%s: re-save round trip differs and ToJson cannot represent the value", target.name)
	}
}

// seedFixedTargets seeds each table with its selector byte and the writer's
// image of two values of the table: its reset value twice, and one value with
// every field away from its default twice. A table whose seed image cannot be
// built is a setup failure, never a table quietly left out of the corpus.
func seedFixedTargets(f *testing.F) {
	f.Helper()
	f.Add([]byte{})
	for i, target := range fixedTargets {
		selector := byte(i)
		for _, seed := range []struct {
			label  string
			values any
		}{
			{"zero", target.pair(target.zero(), target.zero())},
			{"away", target.pair(target.away(), target.away())},
		} {
			size := target.measure(2)
			if size < 0 {
				f.Fatalf("%s: %s seed does not measure", target.name, seed.label)
			}
			image := make([]byte, size)
			if n := target.save(seed.values, image); n != size {
				f.Fatalf("%s: %s seed: measure said %d bytes, save wrote %d", target.name, seed.label, size, n)
			}
			f.Add(append([]byte{selector}, image...))
		}
	}
}
