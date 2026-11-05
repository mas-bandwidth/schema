package tabledemo_test

import (
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

// TestGeneratedMessageWriterNeverTruncates holds docs/SPEC-TABLES.md §5 and
// §3.3 for the generated message save: a value outside its declaration, one
// the read refuses or one whose ranged offset does not fit its bits, and a
// flags bit past the declared W (a mask rides at W bits here) each refuse the
// batch invalid_value. Nothing is narrowed onto the wire. The in-range twin of
// each case saves.
func TestGeneratedMessageWriterNeverTruncates(t *testing.T) {
	t.Parallel()
	saveLoadout := func(v *tabledemo.LoadoutConfig) (int64, string) {
		var rep tabledemo.TableReport
		buf := make([]byte, 1024)
		return tabledemo.LoadoutConfigSaveMessages([]*tabledemo.LoadoutConfig{v}, buf, &rep), rep.Reason
	}
	saveRanged := func(v *tabledemo.RangedSigned) (int64, string) {
		var rep tabledemo.TableReport
		buf := make([]byte, 1024)
		return tabledemo.RangedSignedSaveMessages([]*tabledemo.RangedSigned{v}, buf, &rep), rep.Reason
	}
	for _, c := range []struct {
		name   string
		accept bool
		save   func() (int64, string)
	}{
		{"perks inside W", true, func() (int64, string) {
			var v tabledemo.LoadoutConfig
			tabledemo.LoadoutConfigReset(&v)
			v.Perks = 0b101
			return saveLoadout(&v)
		}},
		{"perks bit past W", false, func() (int64, string) {
			var v tabledemo.LoadoutConfig
			tabledemo.LoadoutConfigReset(&v)
			v.Perks = 0b1001
			return saveLoadout(&v)
		}},
		{"i8_inside at its max", true, func() (int64, string) {
			var v tabledemo.RangedSigned
			tabledemo.RangedSignedReset(&v)
			v.I8Inside = 126
			return saveRanged(&v)
		}},
		{"i8_inside one past its max, inside its bits", false, func() (int64, string) {
			var v tabledemo.RangedSigned
			tabledemo.RangedSignedReset(&v)
			v.I8Inside = 127
			return saveRanged(&v)
		}},
		{"i8_inside one under its min, past its bits", false, func() (int64, string) {
			var v tabledemo.RangedSigned
			tabledemo.RangedSignedReset(&v)
			v.I8Inside = -128
			return saveRanged(&v)
		}},
	} {
		n, reason := c.save()
		if c.accept && n < 0 {
			t.Errorf("%s: the save refused %q", c.name, reason)
		}
		if !c.accept && (n >= 0 || reason != "invalid_value") {
			t.Errorf("%s: the save answered n=%d reason=%q, want -1 and invalid_value", c.name, n, reason)
		}
	}
}
