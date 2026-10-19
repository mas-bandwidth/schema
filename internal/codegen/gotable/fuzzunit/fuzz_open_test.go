// Fuzz target over the generated cooked readers' Open entry point. The property
// for every input: Open never panics and never reads past its input (the target
// hands it a fresh exact-length copy, and -race's checkptr refuses an unsafe
// read outside it); when the opened value's cook is written and opened again,
// the two values' ToJson text is equal. Open validates only the header, so a
// forged region the cook writer refuses is not a re-save case at all.
package tabledemo_test

import (
	"bytes"
	"runtime"
	"testing"
	"unsafe"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

type openTarget struct {
	name        string
	value       func() any
	open        func([]byte) (any, bool)
	measure     func(any) int64
	cook        func(any, []byte) bool
	jsonMeasure func(any) int64
	jsonDump    func(any, []byte) int64
}

// openOrder is the host byte order, which is the order a cook for this build is
// written in and the order Open matches.
var openOrder = tabledemo.TableByteOrder(tabledemo.TableCookByteOrder)

// cookTarget adapts one table's `<T>Open(cook, base, length)` entry point and
// its `<T>CookMeasure`/`<T>CookFrom` writer.
func cookTarget[T any, C any](name string,
	reset func(*T),
	open func(*C, unsafe.Pointer, int64) bool,
	root func(*C) *T,
	measure func(*T) int64,
	cook func(*T, []byte) bool,
	jsonMeasure func(*T) int64,
	jsonDump func(*T, []byte) int64) openTarget {
	return openTarget{
		name: name,
		value: func() any {
			v := new(T)
			reset(v)
			return v
		},
		open: func(data []byte) (any, bool) {
			var c C
			base, length := openBase(data)
			if !open(&c, base, length) {
				return nil, false
			}
			return root(&c), true
		},
		measure:     func(v any) int64 { return measure(v.(*T)) },
		cook:        func(v any, b []byte) bool { return cook(v.(*T), b) },
		jsonMeasure: func(v any) int64 { return jsonMeasure(v.(*T)) },
		jsonDump:    func(v any, b []byte) int64 { return jsonDump(v.(*T), b) },
	}
}

// openEmpty is the non-nil one-byte base an empty input is handed with a length
// of zero: Open refuses a nil base before it reads anything.
var openEmpty [1]byte

func openBase(data []byte) (unsafe.Pointer, int64) {
	if len(data) == 0 {
		return unsafe.Pointer(&openEmpty[0]), 0
	}
	return unsafe.Pointer(&data[0]), int64(len(data))
}

var openTargets = []openTarget{
	cookTarget("Patrol", tabledemo.PatrolReset, tabledemo.PatrolOpen,
		func(c *tabledemo.PatrolCook) *tabledemo.Patrol { return c.Root() },
		func(v *tabledemo.Patrol) int64 { return tabledemo.PatrolCookMeasure(v) },
		func(v *tabledemo.Patrol, b []byte) bool { return tabledemo.PatrolCookFrom(v, b, openOrder) },
		func(v *tabledemo.Patrol) int64 { return tabledemo.PatrolToJsonMeasure(v) },
		func(v *tabledemo.Patrol, b []byte) int64 { return tabledemo.PatrolToJson(v, b) }),
	cookTarget("TeamConfig", tabledemo.TeamConfigReset, tabledemo.TeamConfigOpen,
		func(c *tabledemo.TeamConfigCook) *tabledemo.TeamConfig { return c.Root() },
		tabledemo.TeamConfigCookMeasure,
		func(v *tabledemo.TeamConfig, b []byte) bool { return tabledemo.TeamConfigCookFrom(v, b, openOrder) },
		tabledemo.TeamConfigToJsonMeasure, tabledemo.TeamConfigToJson),
	cookTarget("GunnerConfig", tabledemo.GunnerConfigReset, tabledemo.GunnerConfigOpen,
		func(c *tabledemo.GunnerConfigCook) *tabledemo.GunnerConfig { return c.Root() },
		tabledemo.GunnerConfigCookMeasure,
		func(v *tabledemo.GunnerConfig, b []byte) bool { return tabledemo.GunnerConfigCookFrom(v, b, openOrder) },
		tabledemo.GunnerConfigToJsonMeasure, tabledemo.GunnerConfigToJson),
	cookTarget("TurretConfig", tabledemo.TurretConfigReset, tabledemo.TurretConfigOpen,
		func(c *tabledemo.TurretConfigCook) *tabledemo.TurretConfig { return c.Root() },
		tabledemo.TurretConfigCookMeasure,
		func(v *tabledemo.TurretConfig, b []byte) bool { return tabledemo.TurretConfigCookFrom(v, b, openOrder) },
		tabledemo.TurretConfigToJsonMeasure, tabledemo.TurretConfigToJson),
	cookTarget("HullConfig", tabledemo.HullConfigReset, tabledemo.HullConfigOpen,
		func(c *tabledemo.HullConfigCook) *tabledemo.HullConfig { return c.Root() },
		tabledemo.HullConfigCookMeasure,
		func(v *tabledemo.HullConfig, b []byte) bool { return tabledemo.HullConfigCookFrom(v, b, openOrder) },
		tabledemo.HullConfigToJsonMeasure, tabledemo.HullConfigToJson),
	cookTarget("KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigOpen,
		func(c *tabledemo.KeyedConfigCook) *tabledemo.KeyedConfig { return c.Root() },
		tabledemo.KeyedConfigCookMeasure,
		func(v *tabledemo.KeyedConfig, b []byte) bool { return tabledemo.KeyedConfigCookFrom(v, b, openOrder) },
		tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson),
	cookTarget("ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigOpen,
		func(c *tabledemo.ArchiveConfigCook) *tabledemo.ArchiveConfig { return c.Root() },
		tabledemo.ArchiveConfigCookMeasure,
		func(v *tabledemo.ArchiveConfig, b []byte) bool {
			return tabledemo.ArchiveConfigCookFrom(v, b, openOrder)
		},
		tabledemo.ArchiveConfigToJsonMeasure, tabledemo.ArchiveConfigToJson),
	cookTarget("GunnerSettings", tabledemo.GunnerSettingsReset, tabledemo.GunnerSettingsOpen,
		func(c *tabledemo.GunnerSettingsCook) *tabledemo.GunnerSettings { return c.Root() },
		tabledemo.GunnerSettingsCookMeasure,
		func(v *tabledemo.GunnerSettings, b []byte) bool {
			return tabledemo.GunnerSettingsCookFrom(v, b, openOrder)
		},
		tabledemo.GunnerSettingsToJsonMeasure, tabledemo.GunnerSettingsToJson),
	cookTarget("ShipEntry", tabledemo.ShipEntryReset, tabledemo.ShipEntryOpen,
		func(c *tabledemo.ShipEntryCook) *tabledemo.ShipEntry { return c.Root() },
		tabledemo.ShipEntryCookMeasure,
		func(v *tabledemo.ShipEntry, b []byte) bool { return tabledemo.ShipEntryCookFrom(v, b, openOrder) },
		tabledemo.ShipEntryToJsonMeasure, tabledemo.ShipEntryToJson),
	cookTarget("GlobalSettings", tabledemo.GlobalSettingsReset, tabledemo.GlobalSettingsOpen,
		func(c *tabledemo.GlobalSettingsCook) *tabledemo.GlobalSettings { return c.Root() },
		tabledemo.GlobalSettingsCookMeasure,
		func(v *tabledemo.GlobalSettings, b []byte) bool {
			return tabledemo.GlobalSettingsCookFrom(v, b, openOrder)
		},
		tabledemo.GlobalSettingsToJsonMeasure, tabledemo.GlobalSettingsToJson),
	cookTarget("PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigOpen,
		func(c *tabledemo.PackConfigCook) *tabledemo.PackConfig { return c.Root() },
		tabledemo.PackConfigCookMeasure,
		func(v *tabledemo.PackConfig, b []byte) bool { return tabledemo.PackConfigCookFrom(v, b, openOrder) },
		tabledemo.PackConfigToJsonMeasure, tabledemo.PackConfigToJson),
	cookTarget("RangedSigned", tabledemo.RangedSignedReset, tabledemo.RangedSignedOpen,
		func(c *tabledemo.RangedSignedCook) *tabledemo.RangedSigned { return c.Root() },
		tabledemo.RangedSignedCookMeasure,
		func(v *tabledemo.RangedSigned, b []byte) bool {
			return tabledemo.RangedSignedCookFrom(v, b, openOrder)
		},
		tabledemo.RangedSignedToJsonMeasure, tabledemo.RangedSignedToJson),
	cookTarget("RangedUnsigned", tabledemo.RangedUnsignedReset, tabledemo.RangedUnsignedOpen,
		func(c *tabledemo.RangedUnsignedCook) *tabledemo.RangedUnsigned { return c.Root() },
		tabledemo.RangedUnsignedCookMeasure,
		func(v *tabledemo.RangedUnsigned, b []byte) bool {
			return tabledemo.RangedUnsignedCookFrom(v, b, openOrder)
		},
		tabledemo.RangedUnsignedToJsonMeasure, tabledemo.RangedUnsignedToJson),
	cookTarget("RangedWidths", tabledemo.RangedWidthsReset, tabledemo.RangedWidthsOpen,
		func(c *tabledemo.RangedWidthsCook) *tabledemo.RangedWidths { return c.Root() },
		tabledemo.RangedWidthsCookMeasure,
		func(v *tabledemo.RangedWidths, b []byte) bool {
			return tabledemo.RangedWidthsCookFrom(v, b, openOrder)
		},
		tabledemo.RangedWidthsToJsonMeasure, tabledemo.RangedWidthsToJson),
	cookTarget("RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigOpen,
		func(c *tabledemo.RootConfigCook) *tabledemo.RootConfig { return c.Root() },
		tabledemo.RootConfigCookMeasure,
		func(v *tabledemo.RootConfig, b []byte) bool { return tabledemo.RootConfigCookFrom(v, b, openOrder) },
		tabledemo.RootConfigToJsonMeasure, tabledemo.RootConfigToJson),
	cookTarget("WeaponConfig", tabledemo.WeaponConfigReset, tabledemo.WeaponConfigOpen,
		func(c *tabledemo.WeaponConfigCook) *tabledemo.WeaponConfig { return c.Root() },
		tabledemo.WeaponConfigCookMeasure,
		func(v *tabledemo.WeaponConfig, b []byte) bool { return tabledemo.WeaponConfigCookFrom(v, b, openOrder) },
		tabledemo.WeaponConfigToJsonMeasure, tabledemo.WeaponConfigToJson),
	cookTarget("LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigOpen,
		func(c *tabledemo.LoadoutConfigCook) *tabledemo.LoadoutConfig { return c.Root() },
		tabledemo.LoadoutConfigCookMeasure,
		func(v *tabledemo.LoadoutConfig, b []byte) bool {
			return tabledemo.LoadoutConfigCookFrom(v, b, openOrder)
		},
		tabledemo.LoadoutConfigToJsonMeasure, tabledemo.LoadoutConfigToJson),
	cookTarget("ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigOpen,
		func(c *tabledemo.ProfileConfigCook) *tabledemo.ProfileConfig { return c.Root() },
		tabledemo.ProfileConfigCookMeasure,
		func(v *tabledemo.ProfileConfig, b []byte) bool {
			return tabledemo.ProfileConfigCookFrom(v, b, openOrder)
		},
		tabledemo.ProfileConfigToJsonMeasure, tabledemo.ProfileConfigToJson),
	cookTarget("WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobOpen,
		func(c *tabledemo.WideBlobCook) *tabledemo.WideBlob { return c.Root() },
		tabledemo.WideBlobCookMeasure,
		func(v *tabledemo.WideBlob, b []byte) bool { return tabledemo.WideBlobCookFrom(v, b, openOrder) },
		tabledemo.WideBlobToJsonMeasure, tabledemo.WideBlobToJson),
}

// FuzzOpen hands each input to one table's Open after a byte chooses the table,
// the same selector shape as FuzzLoad, and checks the accepted value's re-save.
func FuzzOpen(f *testing.F) {
	seedOpenCooks(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		target := openTargets[0]
		wire := append([]byte(nil), data...)
		if len(data) > 0 {
			target = openTargets[int(data[0])%len(openTargets)]
			wire = append([]byte(nil), data[1:]...)
		}
		value, ok := target.open(wire)
		if !ok {
			return
		}
		reCookOpens(t, target, value)
		runtime.KeepAlive(wire)
	})
}

// reCookOpens is the accepted-value half of the property: when the opened
// value cooks, opening that cook again yields the same JSON text.
func reCookOpens(t *testing.T, target openTarget, value any) {
	t.Helper()
	image, ok := openImage(target, value)
	if !ok {
		// Open checks the header and not the nodes, so a forged region need
		// not be a value the writer accepts; a refusal is not a re-save case.
		return
	}
	again, ok := target.open(image)
	if !ok {
		t.Fatalf("%s: the value the open accepted does not re-open from its own cook (%d bytes)", target.name, len(image))
	}
	first, firstOK := openJsonText(target, value)
	second, secondOK := openJsonText(target, again)
	if firstOK && secondOK {
		if !bytes.Equal(first, second) {
			t.Fatalf("%s: re-cook round trip differs\n first: %s\nsecond: %s", target.name, first, second)
		}
		runtime.KeepAlive(image)
		return
	}
	// A forged value may hold a float JSON cannot spell (NaN, an infinity) or
	// string bytes that are not UTF-8; ToJson refuses those. The re-save
	// equality is still checked, on the cook image itself.
	againImage, ok := openImage(target, again)
	if !ok || !bytes.Equal(image, againImage) {
		t.Fatalf("%s: re-cook round trip differs and ToJson cannot represent the value", target.name)
	}
	runtime.KeepAlive(image)
}

func openImage(target openTarget, value any) ([]byte, bool) {
	size := target.measure(value)
	if size < 0 {
		return nil, false
	}
	image := make([]byte, size)
	if !target.cook(value, image) {
		return nil, false
	}
	return image, true
}

func openJsonText(target openTarget, value any) ([]byte, bool) {
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

// seedOpenCooks seeds each table with its selector byte and the cook image of
// its zero value and of one value with every field away from its default. A
// table whose seed image cannot be cooked is a setup failure, never a table
// quietly left out of the corpus.
func seedOpenCooks(f *testing.F) {
	f.Helper()
	f.Add([]byte{})
	for i, target := range openTargets {
		selector := byte(i)
		for _, seed := range []struct {
			label string
			value any
		}{
			{"zero", target.value()},
			{"away", awayFromDefaults(target.value())},
		} {
			image, ok := openImage(target, seed.value)
			if !ok {
				f.Fatalf("%s: %s seed does not cook", target.name, seed.label)
			}
			f.Add(append([]byte{selector}, image...))
		}
	}
}
