// Fuzz targets over the generated binary table readers. The property for every
// input: the read never panics, it never reads past its input (each target
// reads a fresh exact-length copy, and -race's checkptr refuses an unsafe read
// outside it), and an accepted value's re-save, read back by the same entry
// point, is equal. A crash is a defect in the reader, never in this harness.
package tabledemo_test

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

type target struct {
	name        string
	value       func() any
	load        func([]byte) (any, bool)
	readStrict  func([]byte) (any, error)
	measure     func(any) int64
	save        func(any, []byte) int64
	jsonMeasure func(any) int64
	jsonDump    func(any, []byte) int64
}

// keyedOf adapts a `<T>Load(value, data, report)` entry point.
func keyedOf[T any](name string, reset func(*T),
	load func(*T, []byte, *tabledemo.TableReport) bool,
	read func([]byte) (*T, error),
	measure func(*T) int64,
	save func(*T, []byte) int64,
	jsonMeasure func(*T) int64,
	jsonDump func(*T, []byte) int64) target {
	return target{
		name: name,
		value: func() any {
			v := new(T)
			reset(v)
			return v
		},
		load: func(data []byte) (any, bool) {
			v := new(T)
			ok := load(v, data, &tabledemo.TableReport{})
			return v, ok
		},
		readStrict: func(data []byte) (any, error) {
			v, err := read(data)
			if v == nil {
				return nil, err
			}
			return v, err
		},
		measure:     func(v any) int64 { return measure(v.(*T)) },
		save:        func(v any, b []byte) int64 { return save(v.(*T), b) },
		jsonMeasure: func(v any) int64 { return jsonMeasure(v.(*T)) },
		jsonDump:    func(v any, b []byte) int64 { return jsonDump(v.(*T), b) },
	}
}

// regionOf adapts the region entry point `PatrolLoad(region, wire, report)`.
// The region is sized by `PatrolLoadMeasure(wire)` and aligned to 64 bytes, as
// a caller sizes it, so every input reaches the region decode; a nil region is
// refused before the wire is read.
func regionOf[T any](name string, reset func(*T),
	loadMeasure func([]byte) int64,
	load func([]byte, []byte, *tabledemo.TableReport) *T,
	read func([]byte) (*T, error),
	measure func(*T, ...tabledemo.TableWriteContext) int64,
	save func(*T, []byte, ...tabledemo.TableWriteContext) int64,
	jsonMeasure func(*T, ...tabledemo.TableAllocator) int64,
	jsonDump func(*T, []byte, ...tabledemo.TableAllocator) int64) target {
	return target{
		name: name,
		value: func() any {
			v := new(T)
			reset(v)
			return v
		},
		load: func(data []byte) (any, bool) {
			size := loadMeasure(data)
			if size < 0 {
				return nil, false
			}
			raw := make([]byte, size+63)
			off := (-uintptr(unsafe.Pointer(unsafe.SliceData(raw)))) & 63
			v := load(raw[off:off+uintptr(size)], data, &tabledemo.TableReport{})
			return v, v != nil
		},
		readStrict: func(data []byte) (any, error) {
			v, err := read(data)
			if v == nil {
				return nil, err
			}
			return v, err
		},
		measure:     func(v any) int64 { return measure(v.(*T)) },
		save:        func(v any, b []byte) int64 { return save(v.(*T), b) },
		jsonMeasure: func(v any) int64 { return jsonMeasure(v.(*T)) },
		jsonDump:    func(v any, b []byte) int64 { return jsonDump(v.(*T), b) },
	}
}

var targets = []target{
	regionOf("Patrol", tabledemo.PatrolReset, tabledemo.PatrolLoadMeasure, tabledemo.PatrolLoad, tabledemo.PatrolReadStrict,
		tabledemo.PatrolMeasure, tabledemo.PatrolSave, tabledemo.PatrolToJSONMeasure, tabledemo.PatrolToJSON),
	keyedOf("TeamConfig", tabledemo.TeamConfigReset, tabledemo.TeamConfigLoad, tabledemo.TeamConfigReadStrict,
		tabledemo.TeamConfigMeasure, tabledemo.TeamConfigSave, tabledemo.TeamConfigToJSONMeasure, tabledemo.TeamConfigToJSON),
	keyedOf("GunnerConfig", tabledemo.GunnerConfigReset, tabledemo.GunnerConfigLoad, tabledemo.GunnerConfigReadStrict,
		tabledemo.GunnerConfigMeasure, tabledemo.GunnerConfigSave, tabledemo.GunnerConfigToJSONMeasure, tabledemo.GunnerConfigToJSON),
	keyedOf("TurretConfig", tabledemo.TurretConfigReset, tabledemo.TurretConfigLoad, tabledemo.TurretConfigReadStrict,
		tabledemo.TurretConfigMeasure, tabledemo.TurretConfigSave, tabledemo.TurretConfigToJSONMeasure, tabledemo.TurretConfigToJSON),
	keyedOf("HullConfig", tabledemo.HullConfigReset, tabledemo.HullConfigLoad, tabledemo.HullConfigReadStrict,
		tabledemo.HullConfigMeasure, tabledemo.HullConfigSave, tabledemo.HullConfigToJSONMeasure, tabledemo.HullConfigToJSON),
	keyedOf("KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoad, tabledemo.KeyedConfigReadStrict,
		tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave, tabledemo.KeyedConfigToJSONMeasure, tabledemo.KeyedConfigToJSON),
	keyedOf("ScoreBoard", tabledemo.ScoreBoardReset, tabledemo.ScoreBoardLoad, tabledemo.ScoreBoardReadStrict,
		tabledemo.ScoreBoardMeasure, tabledemo.ScoreBoardSave, tabledemo.ScoreBoardToJSONMeasure, tabledemo.ScoreBoardToJSON),
	keyedOf("ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoad, tabledemo.ArchiveConfigReadStrict,
		tabledemo.ArchiveConfigMeasure, tabledemo.ArchiveConfigSave, tabledemo.ArchiveConfigToJSONMeasure, tabledemo.ArchiveConfigToJSON),
	keyedOf("GunnerSettings", tabledemo.GunnerSettingsReset, tabledemo.GunnerSettingsLoad, tabledemo.GunnerSettingsReadStrict,
		tabledemo.GunnerSettingsMeasure, tabledemo.GunnerSettingsSave, tabledemo.GunnerSettingsToJSONMeasure, tabledemo.GunnerSettingsToJSON),
	keyedOf("ShipEntry", tabledemo.ShipEntryReset, tabledemo.ShipEntryLoad, tabledemo.ShipEntryReadStrict,
		tabledemo.ShipEntryMeasure, tabledemo.ShipEntrySave, tabledemo.ShipEntryToJSONMeasure, tabledemo.ShipEntryToJSON),
	keyedOf("GlobalSettings", tabledemo.GlobalSettingsReset, tabledemo.GlobalSettingsLoad, tabledemo.GlobalSettingsReadStrict,
		tabledemo.GlobalSettingsMeasure, tabledemo.GlobalSettingsSave, tabledemo.GlobalSettingsToJSONMeasure, tabledemo.GlobalSettingsToJSON),
	keyedOf("PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigLoad, tabledemo.PackConfigReadStrict,
		tabledemo.PackConfigMeasure, tabledemo.PackConfigSave, tabledemo.PackConfigToJSONMeasure, tabledemo.PackConfigToJSON),
	keyedOf("RangedSigned", tabledemo.RangedSignedReset, tabledemo.RangedSignedLoad, tabledemo.RangedSignedReadStrict,
		tabledemo.RangedSignedMeasure, tabledemo.RangedSignedSave, tabledemo.RangedSignedToJSONMeasure, tabledemo.RangedSignedToJSON),
	keyedOf("RangedUnsigned", tabledemo.RangedUnsignedReset, tabledemo.RangedUnsignedLoad, tabledemo.RangedUnsignedReadStrict,
		tabledemo.RangedUnsignedMeasure, tabledemo.RangedUnsignedSave, tabledemo.RangedUnsignedToJSONMeasure, tabledemo.RangedUnsignedToJSON),
	keyedOf("RangedWidths", tabledemo.RangedWidthsReset, tabledemo.RangedWidthsLoad, tabledemo.RangedWidthsReadStrict,
		tabledemo.RangedWidthsMeasure, tabledemo.RangedWidthsSave, tabledemo.RangedWidthsToJSONMeasure, tabledemo.RangedWidthsToJSON),
	keyedOf("RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigLoad, tabledemo.RootConfigReadStrict,
		tabledemo.RootConfigMeasure, tabledemo.RootConfigSave, tabledemo.RootConfigToJSONMeasure, tabledemo.RootConfigToJSON),
	keyedOf("WeaponConfig", tabledemo.WeaponConfigReset, tabledemo.WeaponConfigLoad, tabledemo.WeaponConfigReadStrict,
		tabledemo.WeaponConfigMeasure, tabledemo.WeaponConfigSave, tabledemo.WeaponConfigToJSONMeasure, tabledemo.WeaponConfigToJSON),
	keyedOf("LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoad, tabledemo.LoadoutConfigReadStrict,
		tabledemo.LoadoutConfigMeasure, tabledemo.LoadoutConfigSave, tabledemo.LoadoutConfigToJSONMeasure, tabledemo.LoadoutConfigToJSON),
	keyedOf("ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoad, tabledemo.ProfileConfigReadStrict,
		tabledemo.ProfileConfigMeasure, tabledemo.ProfileConfigSave, tabledemo.ProfileConfigToJSONMeasure, tabledemo.ProfileConfigToJSON),
	keyedOf("Attachment", tabledemo.AttachmentReset, tabledemo.AttachmentLoad, tabledemo.AttachmentReadStrict,
		tabledemo.AttachmentMeasure, tabledemo.AttachmentSave, tabledemo.AttachmentToJSONMeasure, tabledemo.AttachmentToJSON),
	keyedOf("Buff", tabledemo.BuffReset, tabledemo.BuffLoad, tabledemo.BuffReadStrict,
		tabledemo.BuffMeasure, tabledemo.BuffSave, tabledemo.BuffToJSONMeasure, tabledemo.BuffToJSON),
	keyedOf("Debuff", tabledemo.DebuffReset, tabledemo.DebuffLoad, tabledemo.DebuffReadStrict,
		tabledemo.DebuffMeasure, tabledemo.DebuffSave, tabledemo.DebuffToJSONMeasure, tabledemo.DebuffToJSON),
	keyedOf("WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobLoad, tabledemo.WideBlobReadStrict,
		tabledemo.WideBlobMeasure, tabledemo.WideBlobSave, tabledemo.WideBlobToJSONMeasure, tabledemo.WideBlobToJSON),
}

// TestTargetsLoadTheirSeeds requires every target's load to accept the
// writer's image of a value away from its defaults, so the fuzz targets reach
// each entry point's decode rather than a refusal of the harness's own
// arguments (a region load given no region refuses before it reads the wire).
func TestTargetsLoadTheirSeeds(t *testing.T) {
	t.Parallel()
	for _, target := range targets {
		image, err := writerImage(target, awayFromDefaults(target.value()))
		if err != nil {
			t.Fatalf("%s: %v", target.name, err)
		}
		if _, ok := target.load(image); !ok {
			t.Errorf("%s: the load refused the writer's own image, so the fuzz target never reaches its decode", target.name)
		}
	}
}

func FuzzLoad(f *testing.F) {
	seedTargets(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		target := targets[int(data[0])%len(targets)]
		wire := append([]byte(nil), data[1:]...)
		value, ok := target.load(wire)
		if !ok {
			return
		}
		reSaveReadsBack(t, target, value, target.load)
	})
}

func FuzzReadStrict(f *testing.F) {
	seedTargets(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		target := targets[int(data[0])%len(targets)]
		wire := append([]byte(nil), data[1:]...)
		value, err := target.readStrict(wire)
		if err != nil {
			return
		}
		reSaveReadsBack(t, target, value, func(wire []byte) (any, bool) {
			v, err := target.readStrict(wire)
			return v, err == nil && v != nil
		})
	})
}

// reSaveReadsBack is the accepted-value half of the property: the value the
// reader accepted saves in the same writer image, and reading that image back
// through the same entry point yields the same JSON text.
func reSaveReadsBack(t *testing.T, target target, value any, reread func([]byte) (any, bool)) {
	t.Helper()
	size := target.measure(value)
	if size < 0 {
		t.Fatalf("%s: an accepted value does not measure", target.name)
	}
	wire := make([]byte, size)
	if n := target.save(value, wire); n != size {
		t.Fatalf("%s: measure said %d bytes, save wrote %d", target.name, size, n)
	}
	again, ok := reread(wire)
	if !ok {
		t.Fatalf("%s: the value the read accepted does not re-read from its own save (%d bytes)", target.name, size)
	}
	first, firstOK := jsonText(target, value)
	second, secondOK := jsonText(target, again)
	if firstOK && secondOK {
		if !bytes.Equal(first, second) {
			t.Fatalf("%s: re-save round trip differs\n first: %s\nsecond: %s", target.name, first, second)
		}
		return
	}
	// A wire-legal value may hold string bytes that are not UTF-8, which
	// ToJSON refuses (never a NaN or an infinity: every read refuses one, §3.3).
	// The re-save equality is still checked, on the writer image itself.
	againImage := make([]byte, size)
	if n := target.save(again, againImage); n != size || !bytes.Equal(wire, againImage) {
		t.Fatalf("%s: re-save round trip differs and ToJSON cannot represent the value", target.name)
	}
}

func jsonText(target target, value any) ([]byte, bool) {
	size := target.jsonMeasure(value)
	if size < 0 {
		return nil, false
	}
	text := make([]byte, size)
	if n := target.jsonDump(value, text); n != size {
		return nil, false
	}
	return text, true
}

// seedTargets seeds each table with its selector byte and the writer's image
// of its zero value and of one value with every field away from its default.
// A table whose seed image cannot be built is a setup failure, never a table
// quietly left out of the corpus.
func seedTargets(f *testing.F) {
	f.Helper()
	f.Add([]byte{})
	for i, target := range targets {
		selector := byte(i)
		for _, seed := range []struct {
			label string
			value any
		}{
			{"zero", target.value()},
			{"away", awayFromDefaults(target.value())},
		} {
			image, err := writerImage(target, seed.value)
			if err != nil {
				f.Fatalf("%s: %s seed: %v", target.name, seed.label, err)
			}
			f.Add(append([]byte{selector}, image...))
		}
	}
}

func writerImage(target target, value any) ([]byte, error) {
	size := target.measure(value)
	if size < 0 {
		return nil, fmt.Errorf("measure returned %d", size)
	}
	image := make([]byte, size)
	if n := target.save(value, image); n != size {
		return nil, fmt.Errorf("measure said %d bytes, save wrote %d", size, n)
	}
	return image, nil
}

// awayFromDefaults walks the exported fields and moves each one off the reset
// value, so the full seed exercises every field's non-default branch.
func awayFromDefaults(value any) any {
	fillAway(reflect.ValueOf(value).Elem())
	return value
}

func fillAway(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillAway(v.Elem())
	case reflect.Struct:
		for _, field := range v.Fields() {
			if !field.CanSet() {
				continue
			}
			fillAway(field)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fillAway(v.Index(i))
		}
	case reflect.Slice:
		if v.CanSet() && v.Len() == 0 {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		}
		for i := 0; i < v.Len(); i++ {
			fillAway(v.Index(i))
		}
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1)
	case reflect.String:
		v.SetString(v.String() + "x")
	}
}
