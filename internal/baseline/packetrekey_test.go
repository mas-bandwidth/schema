package baseline_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// TestPacketRekeyIsReported (F-003): on the PACKET wire an enum-keyed array's
// slots are positions, so `[Team]uint8` -> `[Axis]uint8` keeps the layout and
// the protocol id (SPEC §4 item 4), and bytes written as Red=11, Blue=22 read
// as East=11, West=22. Nothing in the id moves, so `schema check` against the
// committed baseline is what reports it. The schema pair is committed under
// testdata/packet-rekey; the controls are a swap whose slot names line up, a
// swap to the positional spelling, an unrelated table beside the packet, and
// a reorder of the key enum itself, which moves the protocol id instead.
func TestPacketRekeyIsReported(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join("testdata", "packet-rekey", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	before, after := read("before.schema"), read("after.schema")

	// check runs the CLI's own path: a baseline written by --update from the
	// old source, then the live source checked against that file
	check := func(t *testing.T, old, live string) []error {
		t.Helper()
		dir, paths := writeUnit(t, old)
		if _, _, err := baseline.Update(unit(t, old), paths, "before the rekey"); err != nil {
			t.Fatal(err)
		}
		if w, errs := baseline.Check(unit(t, old), paths); len(w)+len(errs) != 0 {
			t.Fatalf("the unchanged source must check clean: warnings=%v errs=%v", w, errs)
		}
		text, err := os.ReadFile(filepath.Join(dir, baseline.FileName))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(old, "values [Team]") && !strings.Contains(string(text), "keyed values key=Team bound=2 slots=Red,Blue") {
			t.Fatalf("the baseline does not record the packet's keyed array:\n%s", text)
		}
		w, errs := baseline.Check(unit(t, live), paths)
		if len(w) != 0 {
			t.Errorf("unexpected warnings: %v", w)
		}
		return errs
	}

	t.Run("rekey under one protocol id refuses", func(t *testing.T) {
		if a, b := unit(t, before).ProtocolId, unit(t, after).ProtocolId; a != b {
			t.Fatalf("the fixture must keep the protocol id (SPEC §4 item 4): %016x -> %016x", a, b)
		}
		errs := check(t, before, after)
		if len(errs) != 1 {
			t.Fatalf("want exactly one refusal, got %v", errs)
		}
		for _, want := range []string{"Packet.values", "packet array key enum Team -> Axis", "the protocol id does not move", "slot 1 Red -> East", "slot 2 Blue -> West"} {
			if !strings.Contains(errs[0].Error(), want) {
				t.Errorf("the refusal does not say %q:\n  %v", want, errs[0])
			}
		}
	})
	t.Run("an unrelated table beside the packet does not hide it", func(t *testing.T) {
		const save = "\ntable Save\n{\n    n int32\n}\n"
		if errs := check(t, before+save, after+save); len(errs) != 1 || !strings.Contains(errs[0].Error(), "packet array key enum Team -> Axis") {
			t.Errorf("want the packet refusal, got %v", errs)
		}
	})
	t.Run("control: slot names that line up pass", func(t *testing.T) {
		if errs := check(t, before, editOf(t, before, "values [Team]uint8", "values [Crew]uint8")); len(errs) != 0 {
			t.Errorf("Team -> Crew names every slot the same, got %v", errs)
		}
	})
	t.Run("control: the positional spelling passes", func(t *testing.T) {
		if errs := check(t, before, editOf(t, before, "values [Team]uint8", "values [2]uint8")); len(errs) != 0 {
			t.Errorf("[Team] -> [2] renames no slot, got %v", errs)
		}
	})
	t.Run("control: the key enum reordered moves the id instead", func(t *testing.T) {
		reordered := editOf(t, before, "enum Team\n{\n    Red\n    Blue\n}", "enum Team\n{\n    Blue\n    Red\n}")
		if unit(t, before).ProtocolId == unit(t, reordered).ProtocolId {
			t.Fatalf("reordering the key enum must move the protocol id")
		}
		for _, err := range check(t, before, reordered) {
			if strings.Contains(err.Error(), "packet array key enum") {
				t.Errorf("a reorder is the protocol id's to report, not a rekey: %v", err)
			}
		}
	})
	t.Run("attribution", func(t *testing.T) {
		policy := map[string]baseline.TokenRule{}
		for k, v := range baseline.DefaultTokenPolicy {
			if k != "packet-key" {
				policy[k] = v
			}
		}
		got := baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), policy)
		if len(got) != 0 {
			t.Errorf("with the packet-key row removed the rekey must not be judged, got:%s", summary(got))
		}
		if got := baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), baseline.DefaultTokenPolicy); len(got) != 1 {
			t.Errorf("want one finding, got:%s", fmt.Sprint(summary(got)))
		}
	})
}
