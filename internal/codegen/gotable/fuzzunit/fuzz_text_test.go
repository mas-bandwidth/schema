// Fuzz targets over the generated table text readers. The property for every
// input: the read never panics, it never reads past its input (each target
// reads a fresh exact-length copy, and -race's checkptr refuses an unsafe read
// outside it), and an accepted value's re-encoding, read back by the same entry
// point, is equal. A crash is a defect in the reader, never in this harness.
package tabledemo_test

import (
	"bytes"
	"fmt"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

type textTarget struct {
	name        string
	value       func() any
	loadText    func(string) (any, bool)
	fromJson    func([]byte) (any, func(), bool)
	saveText    func(any) (string, error)
	jsonMeasure func(any) int64
	jsonDump    func(any, []byte) int64
}

// keyedTextOf adapts the fixed-value entry points `<T>LoadText` and
// `<T>FromJson(value, text, report)`.
func keyedTextOf[T any](name string, reset func(*T),
	loadText func(string, *tabledemo.TableReport) (*T, error),
	fromJson func(*T, []byte, *tabledemo.TableReport) bool,
	saveText func(*T) (string, error),
	jsonMeasure func(*T) int64,
	jsonDump func(*T, []byte) int64) textTarget {
	return textTarget{
		name: name,
		value: func() any {
			v := new(T)
			reset(v)
			return v
		},
		loadText: func(text string) (any, bool) {
			v, err := loadText(text, &tabledemo.TableReport{})
			return v, v != nil && err == nil
		},
		fromJson: func(text []byte) (any, func(), bool) {
			v := new(T)
			ok := fromJson(v, text, &tabledemo.TableReport{})
			return v, func() {}, ok
		},
		saveText:    func(v any) (string, error) { return saveText(v.(*T)) },
		jsonMeasure: func(v any) int64 { return jsonMeasure(v.(*T)) },
		jsonDump:    func(v any, b []byte) int64 { return jsonDump(v.(*T), b) },
	}
}

var textTargets = []textTarget{
	{
		name:  "Patrol",
		value: func() any { v := new(tabledemo.Patrol); tabledemo.PatrolReset(v); return v },
		loadText: func(text string) (any, bool) {
			v, err := tabledemo.PatrolLoadText(text, &tabledemo.TableReport{})
			return v, v != nil && err == nil
		},
		fromJson: func(text []byte) (any, func(), bool) {
			var b tabledemo.PatrolBuilder
			if !b.Init() {
				return nil, func() {}, false
			}
			if !tabledemo.PatrolFromJson(&b, text, &tabledemo.TableReport{}) {
				b.Shutdown()
				return nil, func() {}, false
			}
			return b.GetRoot(), b.Shutdown, true
		},
		saveText:    func(v any) (string, error) { return tabledemo.PatrolSaveText(v.(*tabledemo.Patrol)) },
		jsonMeasure: func(v any) int64 { return tabledemo.PatrolToJsonMeasure(v.(*tabledemo.Patrol)) },
		jsonDump:    func(v any, b []byte) int64 { return tabledemo.PatrolToJson(v.(*tabledemo.Patrol), b) },
	},
	keyedTextOf("TeamConfig", tabledemo.TeamConfigReset, tabledemo.TeamConfigLoadText, tabledemo.TeamConfigFromJson,
		tabledemo.TeamConfigSaveText, tabledemo.TeamConfigToJsonMeasure, tabledemo.TeamConfigToJson),
	keyedTextOf("GunnerConfig", tabledemo.GunnerConfigReset, tabledemo.GunnerConfigLoadText, tabledemo.GunnerConfigFromJson,
		tabledemo.GunnerConfigSaveText, tabledemo.GunnerConfigToJsonMeasure, tabledemo.GunnerConfigToJson),
	keyedTextOf("TurretConfig", tabledemo.TurretConfigReset, tabledemo.TurretConfigLoadText, tabledemo.TurretConfigFromJson,
		tabledemo.TurretConfigSaveText, tabledemo.TurretConfigToJsonMeasure, tabledemo.TurretConfigToJson),
	keyedTextOf("HullConfig", tabledemo.HullConfigReset, tabledemo.HullConfigLoadText, tabledemo.HullConfigFromJson,
		tabledemo.HullConfigSaveText, tabledemo.HullConfigToJsonMeasure, tabledemo.HullConfigToJson),
	keyedTextOf("KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoadText, tabledemo.KeyedConfigFromJson,
		tabledemo.KeyedConfigSaveText, tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson),
	keyedTextOf("ScoreBoard", tabledemo.ScoreBoardReset, tabledemo.ScoreBoardLoadText, tabledemo.ScoreBoardFromJson,
		tabledemo.ScoreBoardSaveText, tabledemo.ScoreBoardToJsonMeasure, tabledemo.ScoreBoardToJson),
	keyedTextOf("ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoadText, tabledemo.ArchiveConfigFromJson,
		tabledemo.ArchiveConfigSaveText, tabledemo.ArchiveConfigToJsonMeasure, tabledemo.ArchiveConfigToJson),
	keyedTextOf("GunnerSettings", tabledemo.GunnerSettingsReset, tabledemo.GunnerSettingsLoadText, tabledemo.GunnerSettingsFromJson,
		tabledemo.GunnerSettingsSaveText, tabledemo.GunnerSettingsToJsonMeasure, tabledemo.GunnerSettingsToJson),
	keyedTextOf("ShipEntry", tabledemo.ShipEntryReset, tabledemo.ShipEntryLoadText, tabledemo.ShipEntryFromJson,
		tabledemo.ShipEntrySaveText, tabledemo.ShipEntryToJsonMeasure, tabledemo.ShipEntryToJson),
	keyedTextOf("GlobalSettings", tabledemo.GlobalSettingsReset, tabledemo.GlobalSettingsLoadText, tabledemo.GlobalSettingsFromJson,
		tabledemo.GlobalSettingsSaveText, tabledemo.GlobalSettingsToJsonMeasure, tabledemo.GlobalSettingsToJson),
	keyedTextOf("PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigLoadText, tabledemo.PackConfigFromJson,
		tabledemo.PackConfigSaveText, tabledemo.PackConfigToJsonMeasure, tabledemo.PackConfigToJson),
	keyedTextOf("RangedSigned", tabledemo.RangedSignedReset, tabledemo.RangedSignedLoadText, tabledemo.RangedSignedFromJson,
		tabledemo.RangedSignedSaveText, tabledemo.RangedSignedToJsonMeasure, tabledemo.RangedSignedToJson),
	keyedTextOf("RangedUnsigned", tabledemo.RangedUnsignedReset, tabledemo.RangedUnsignedLoadText, tabledemo.RangedUnsignedFromJson,
		tabledemo.RangedUnsignedSaveText, tabledemo.RangedUnsignedToJsonMeasure, tabledemo.RangedUnsignedToJson),
	keyedTextOf("RangedWidths", tabledemo.RangedWidthsReset, tabledemo.RangedWidthsLoadText, tabledemo.RangedWidthsFromJson,
		tabledemo.RangedWidthsSaveText, tabledemo.RangedWidthsToJsonMeasure, tabledemo.RangedWidthsToJson),
	keyedTextOf("RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigLoadText, tabledemo.RootConfigFromJson,
		tabledemo.RootConfigSaveText, tabledemo.RootConfigToJsonMeasure, tabledemo.RootConfigToJson),
	keyedTextOf("WeaponConfig", tabledemo.WeaponConfigReset, tabledemo.WeaponConfigLoadText, tabledemo.WeaponConfigFromJson,
		tabledemo.WeaponConfigSaveText, tabledemo.WeaponConfigToJsonMeasure, tabledemo.WeaponConfigToJson),
	keyedTextOf("LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoadText, tabledemo.LoadoutConfigFromJson,
		tabledemo.LoadoutConfigSaveText, tabledemo.LoadoutConfigToJsonMeasure, tabledemo.LoadoutConfigToJson),
	keyedTextOf("ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoadText, tabledemo.ProfileConfigFromJson,
		tabledemo.ProfileConfigSaveText, tabledemo.ProfileConfigToJsonMeasure, tabledemo.ProfileConfigToJson),
	keyedTextOf("Attachment", tabledemo.AttachmentReset, tabledemo.AttachmentLoadText, tabledemo.AttachmentFromJson,
		tabledemo.AttachmentSaveText, tabledemo.AttachmentToJsonMeasure, tabledemo.AttachmentToJson),
	keyedTextOf("Buff", tabledemo.BuffReset, tabledemo.BuffLoadText, tabledemo.BuffFromJson,
		tabledemo.BuffSaveText, tabledemo.BuffToJsonMeasure, tabledemo.BuffToJson),
	keyedTextOf("Debuff", tabledemo.DebuffReset, tabledemo.DebuffLoadText, tabledemo.DebuffFromJson,
		tabledemo.DebuffSaveText, tabledemo.DebuffToJsonMeasure, tabledemo.DebuffToJson),
	keyedTextOf("WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobLoadText, tabledemo.WideBlobFromJson,
		tabledemo.WideBlobSaveText, tabledemo.WideBlobToJsonMeasure, tabledemo.WideBlobToJson),
}

func FuzzLoadText(f *testing.F) {
	seedTextTargets(f, func(target textTarget, value any) ([]byte, error) {
		text, err := target.saveText(value)
		return []byte(text), err
	})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		target := textTargets[int(data[0])%len(textTargets)]
		in := append([]byte(nil), data[1:]...)
		value, ok := target.loadText(string(in))
		if !ok {
			return
		}
		reSaveReadsBackText(t, target, value)
	})
}

func FuzzFromJson(f *testing.F) {
	seedTextTargets(f, textImage)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		target := textTargets[int(data[0])%len(textTargets)]
		in := append([]byte(nil), data[1:]...)
		value, done, ok := target.fromJson(in)
		defer done()
		if !ok {
			return
		}
		first, firstOK := textJson(target, value)
		if !firstOK {
			t.Fatalf("%s: an accepted value does not re-encode as JSON", target.name)
		}
		again, againDone, ok := target.fromJson(first)
		if !ok {
			againDone()
			t.Fatalf("%s: the value the read accepted does not re-read from its own JSON (%d bytes)", target.name, len(first))
		}
		defer againDone()
		second, secondOK := textJson(target, again)
		if !secondOK {
			t.Fatalf("%s: a re-read value does not re-encode as JSON", target.name)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("%s: re-encode round trip differs\n first: %s\nsecond: %s", target.name, first, second)
		}
	})
}

// reSaveReadsBackText is the accepted-value half of the property for the base64
// text read: the value saves back to base64 text, reading that text through the
// same entry point yields the same value, and the two values have the same JSON
// text. A wire-legal value may hold a float JSON cannot spell or string bytes
// that are not UTF-8; then the save text itself is compared instead.
func reSaveReadsBackText(t *testing.T, target textTarget, value any) {
	t.Helper()
	text, err := target.saveText(value)
	if err != nil {
		t.Fatalf("%s: an accepted value does not save text: %v", target.name, err)
	}
	again, ok := target.loadText(text)
	if !ok {
		t.Fatalf("%s: the value the read accepted does not re-read from its own save text (%d bytes)", target.name, len(text))
	}
	first, firstOK := textJson(target, value)
	second, secondOK := textJson(target, again)
	if firstOK && secondOK {
		if !bytes.Equal(first, second) {
			t.Fatalf("%s: re-save round trip differs\n first: %s\nsecond: %s", target.name, first, second)
		}
		return
	}
	againText, err := target.saveText(again)
	if err != nil || againText != text {
		t.Fatalf("%s: re-save round trip differs and ToJson cannot represent the value", target.name)
	}
}

func textJson(target textTarget, value any) ([]byte, bool) {
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

func textImage(target textTarget, value any) ([]byte, error) {
	size := target.jsonMeasure(value)
	if size < 0 {
		return nil, fmt.Errorf("measure returned %d", size)
	}
	image := make([]byte, size)
	if n := target.jsonDump(value, image); n != size {
		return nil, fmt.Errorf("measure said %d bytes, dump wrote %d", size, n)
	}
	return image, nil
}

// seedTextTargets seeds each table with its selector byte and the writer's
// image of its zero value and of one value with every field away from its
// default. A table whose seed image cannot be built is a setup failure, never a
// table quietly left out of the corpus.
func seedTextTargets(f *testing.F, image func(textTarget, any) ([]byte, error)) {
	f.Helper()
	f.Add([]byte{})
	for i, target := range textTargets {
		selector := byte(i)
		for _, seed := range []struct {
			label string
			value any
		}{
			{"zero", target.value()},
			{"away", awayFromDefaults(target.value())},
		} {
			encoded, err := image(target, seed.value)
			if err != nil {
				f.Fatalf("%s: %s seed: %v", target.name, seed.label, err)
			}
			f.Add(append([]byte{selector}, encoded...))
		}
	}
}
