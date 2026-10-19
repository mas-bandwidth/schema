// Fuzz targets over the generated message readers: the announced table form's
// <Name>LoadMessages (docs/SPEC-TABLES.md §3.3, §6.5). The property for every
// input: the read never panics, it
// never reads past its input (each target reads a fresh exact-length copy, and
// -race's checkptr refuses an unsafe read outside it), and an accepted value's
// re-save, read back by the same entry point, is equal. A crash is a defect in
// the reader, never in this harness.
package tabledemo_test

import (
	"bytes"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

// messagesValues is the caller's storage one message read fills. A batch larger
// than this answers table_too_large from the entry point, which is itself a
// path worth reading; it keeps a wide body from allocating the whole batch
// bound on every input.
const messagesValues = 8

type messagesTarget struct {
	name     string
	zero     func() any
	load     func(*tabledemo.TableVocabulary, []byte) ([]any, bool)
	save     func([]any) ([]byte, bool)
	jsonText func(any) ([]byte, bool)
}

// messagesTargetOf adapts one table's `<T>LoadMessages`, `<T>SaveMessages` and
// `<T>MeasureMessages` entry points (§3.3's batch codec and its strict read,
// §6.5).
func messagesTargetOf[T any](name string, reset func(*T),
	load func([]T, *tabledemo.TableVocabulary, []byte, *tabledemo.TableReport) (int64, bool),
	measure func([]*T, *tabledemo.TableReport) int64,
	save func([]*T, []byte, *tabledemo.TableReport) int64,
	jsonMeasure func(*T) int64,
	jsonDump func(*T, []byte) int64) messagesTarget {
	return messagesTarget{
		name: name,
		zero: func() any {
			v := new(T)
			reset(v)
			return v
		},
		load: func(vocabulary *tabledemo.TableVocabulary, data []byte) ([]any, bool) {
			values := make([]T, messagesValues)
			n, ok := load(values, vocabulary, data, &tabledemo.TableReport{})
			if !ok || n <= 0 {
				return nil, false
			}
			accepted := make([]any, n)
			for i := range accepted {
				accepted[i] = &values[i]
			}
			return accepted, true
		},
		save: func(values []any) ([]byte, bool) {
			pointers := make([]*T, len(values))
			for i, value := range values {
				pointers[i] = value.(*T)
			}
			size := measure(pointers, &tabledemo.TableReport{})
			if size < 0 {
				return nil, false
			}
			image := make([]byte, size)
			if n := save(pointers, image, &tabledemo.TableReport{}); n != size {
				return nil, false
			}
			return image, true
		},
		jsonText: func(value any) ([]byte, bool) {
			v := value.(*T)
			size := jsonMeasure(v)
			if size < 0 {
				return nil, false
			}
			text := make([]byte, size)
			if n := jsonDump(v, text); n != size {
				return nil, false
			}
			return text, true
		},
	}
}

var messagesTargets = []messagesTarget{
	messagesTargetOf("TeamConfig", tabledemo.TeamConfigReset,
		tabledemo.TeamConfigLoadMessages, tabledemo.TeamConfigMeasureMessages, tabledemo.TeamConfigSaveMessages,
		tabledemo.TeamConfigToJsonMeasure, tabledemo.TeamConfigToJson),
	messagesTargetOf("GunnerConfig", tabledemo.GunnerConfigReset,
		tabledemo.GunnerConfigLoadMessages, tabledemo.GunnerConfigMeasureMessages, tabledemo.GunnerConfigSaveMessages,
		tabledemo.GunnerConfigToJsonMeasure, tabledemo.GunnerConfigToJson),
	messagesTargetOf("TurretConfig", tabledemo.TurretConfigReset,
		tabledemo.TurretConfigLoadMessages, tabledemo.TurretConfigMeasureMessages, tabledemo.TurretConfigSaveMessages,
		tabledemo.TurretConfigToJsonMeasure, tabledemo.TurretConfigToJson),
	messagesTargetOf("HullConfig", tabledemo.HullConfigReset,
		tabledemo.HullConfigLoadMessages, tabledemo.HullConfigMeasureMessages, tabledemo.HullConfigSaveMessages,
		tabledemo.HullConfigToJsonMeasure, tabledemo.HullConfigToJson),
	messagesTargetOf("KeyedConfig", tabledemo.KeyedConfigReset,
		tabledemo.KeyedConfigLoadMessages, tabledemo.KeyedConfigMeasureMessages, tabledemo.KeyedConfigSaveMessages,
		tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson),
	messagesTargetOf("ArchiveConfig", tabledemo.ArchiveConfigReset,
		tabledemo.ArchiveConfigLoadMessages, tabledemo.ArchiveConfigMeasureMessages, tabledemo.ArchiveConfigSaveMessages,
		tabledemo.ArchiveConfigToJsonMeasure, tabledemo.ArchiveConfigToJson),
	messagesTargetOf("GunnerSettings", tabledemo.GunnerSettingsReset,
		tabledemo.GunnerSettingsLoadMessages, tabledemo.GunnerSettingsMeasureMessages, tabledemo.GunnerSettingsSaveMessages,
		tabledemo.GunnerSettingsToJsonMeasure, tabledemo.GunnerSettingsToJson),
	messagesTargetOf("ShipEntry", tabledemo.ShipEntryReset,
		tabledemo.ShipEntryLoadMessages, tabledemo.ShipEntryMeasureMessages, tabledemo.ShipEntrySaveMessages,
		tabledemo.ShipEntryToJsonMeasure, tabledemo.ShipEntryToJson),
	messagesTargetOf("GlobalSettings", tabledemo.GlobalSettingsReset,
		tabledemo.GlobalSettingsLoadMessages, tabledemo.GlobalSettingsMeasureMessages, tabledemo.GlobalSettingsSaveMessages,
		tabledemo.GlobalSettingsToJsonMeasure, tabledemo.GlobalSettingsToJson),
	messagesTargetOf("PackConfig", tabledemo.PackConfigReset,
		tabledemo.PackConfigLoadMessages, tabledemo.PackConfigMeasureMessages, tabledemo.PackConfigSaveMessages,
		tabledemo.PackConfigToJsonMeasure, tabledemo.PackConfigToJson),
	messagesTargetOf("RangedSigned", tabledemo.RangedSignedReset,
		tabledemo.RangedSignedLoadMessages, tabledemo.RangedSignedMeasureMessages, tabledemo.RangedSignedSaveMessages,
		tabledemo.RangedSignedToJsonMeasure, tabledemo.RangedSignedToJson),
	messagesTargetOf("RangedUnsigned", tabledemo.RangedUnsignedReset,
		tabledemo.RangedUnsignedLoadMessages, tabledemo.RangedUnsignedMeasureMessages, tabledemo.RangedUnsignedSaveMessages,
		tabledemo.RangedUnsignedToJsonMeasure, tabledemo.RangedUnsignedToJson),
	messagesTargetOf("RangedWidths", tabledemo.RangedWidthsReset,
		tabledemo.RangedWidthsLoadMessages, tabledemo.RangedWidthsMeasureMessages, tabledemo.RangedWidthsSaveMessages,
		tabledemo.RangedWidthsToJsonMeasure, tabledemo.RangedWidthsToJson),
	messagesTargetOf("RootConfig", tabledemo.RootConfigReset,
		tabledemo.RootConfigLoadMessages, tabledemo.RootConfigMeasureMessages, tabledemo.RootConfigSaveMessages,
		tabledemo.RootConfigToJsonMeasure, tabledemo.RootConfigToJson),
	messagesTargetOf("WeaponConfig", tabledemo.WeaponConfigReset,
		tabledemo.WeaponConfigLoadMessages, tabledemo.WeaponConfigMeasureMessages, tabledemo.WeaponConfigSaveMessages,
		tabledemo.WeaponConfigToJsonMeasure, tabledemo.WeaponConfigToJson),
	messagesTargetOf("LoadoutConfig", tabledemo.LoadoutConfigReset,
		tabledemo.LoadoutConfigLoadMessages, tabledemo.LoadoutConfigMeasureMessages, tabledemo.LoadoutConfigSaveMessages,
		tabledemo.LoadoutConfigToJsonMeasure, tabledemo.LoadoutConfigToJson),
	messagesTargetOf("ProfileConfig", tabledemo.ProfileConfigReset,
		tabledemo.ProfileConfigLoadMessages, tabledemo.ProfileConfigMeasureMessages, tabledemo.ProfileConfigSaveMessages,
		tabledemo.ProfileConfigToJsonMeasure, tabledemo.ProfileConfigToJson),
	messagesTargetOf("WideBlob", tabledemo.WideBlobReset,
		tabledemo.WideBlobLoadMessages, tabledemo.WideBlobMeasureMessages, tabledemo.WideBlobSaveMessages,
		tabledemo.WideBlobToJsonMeasure, tabledemo.WideBlobToJson),
}

func FuzzLoadMessages(f *testing.F) {
	vocabulary := messagesVocabulary(f)
	seedMessagesTargets(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		target := messagesTargets[int(data[0])%len(messagesTargets)]
		in := append([]byte(nil), data[1:]...)
		in = in[:len(in):len(in)]
		values, ok := target.load(vocabulary, in)
		if !ok {
			return
		}
		reSaveMessagesReadsBack(t, target, vocabulary, values)
	})
}

// reSaveMessagesReadsBack is the accepted-value half of the property (§3.3):
// the values the reader accepted save in the same writer image, and reading that
// image back through the same entry point yields the same JSON text.
func reSaveMessagesReadsBack(t *testing.T, target messagesTarget, vocabulary *tabledemo.TableVocabulary, values []any) {
	t.Helper()
	wire, ok := target.save(values)
	if !ok {
		t.Fatalf("%s: the values the read accepted do not save", target.name)
	}
	again, ok := target.load(vocabulary, wire)
	if !ok || len(again) != len(values) {
		t.Fatalf("%s: the values the read accepted do not re-read from their own save (%d bytes)", target.name, len(wire))
	}
	for i := range values {
		first, firstOK := target.jsonText(values[i])
		second, secondOK := target.jsonText(again[i])
		if firstOK && secondOK {
			if !bytes.Equal(first, second) {
				t.Fatalf("%s: re-save round trip differs for value %d\n first: %s\nsecond: %s", target.name, i, first, second)
			}
			continue
		}
		// A wire-legal value may hold a float JSON cannot spell (NaN, an
		// infinity) or string bytes that are not UTF-8; ToJson refuses those.
		// The re-save equality is still checked, on the writer image itself.
		againWire, ok := target.save(again)
		if !ok || !bytes.Equal(wire, againWire) {
			t.Fatalf("%s: re-save round trip differs for value %d and ToJson cannot represent the value", target.name, i)
		}
		return
	}
}

// messagesVocabulary is the vocabulary the package's own message tests pass
// (§3.3): the package's announcement read once into storage named here.
func messagesVocabulary(tb testing.TB) *tabledemo.TableVocabulary {
	tb.Helper()
	vocabulary := new(tabledemo.TableVocabulary)
	vocabulary.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
	announcement := make([]byte, tabledemo.AnnounceMeasure())
	if n := tabledemo.Announce(announcement); n != int64(len(announcement)) {
		tb.Fatalf("Announce wrote %d bytes, want %d", n, len(announcement))
	}
	var report tabledemo.TableReport
	if !tabledemo.AnnounceRead(vocabulary, announcement, &report) {
		tb.Fatalf("AnnounceRead %+v", report)
	}
	return vocabulary
}

// seedMessagesTargets seeds each table with its selector byte and the
// SaveMessages image of its zero value and of one value with every field away
// from its default. A table whose seed image cannot be built is a setup
// failure, never a table quietly left out of the corpus.
func seedMessagesTargets(f *testing.F) {
	f.Helper()
	f.Add([]byte{})
	for i, target := range messagesTargets {
		selector := byte(i)
		for _, seed := range []struct {
			label string
			value any
		}{
			{"zero", target.zero()},
			{"away", awayFromDefaults(target.zero())},
		} {
			image, ok := target.save([]any{seed.value})
			if !ok {
				f.Fatalf("%s: %s seed: SaveMessages refused", target.name, seed.label)
			}
			f.Add(append([]byte{selector}, image...))
		}
	}
}
