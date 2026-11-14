package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// THE DEAD-BYTE VECTORS (docs/SPEC-TABLES.md §3.4, THE SLACK RULE;
// docs/rulings.json fixed-dead-bytes-zero). Every byte of a fixed record that
// no value decides is zero: the bytes past a counted array's live count, past
// a text's or a byte buffer's used length, behind an absent or narrower union
// arm, and under an absent optional's present byte. A reader refuses a record
// where any of them is not. Each vector is a conforming record of the corpus
// with ONE such byte set to a value that would be lawful if it were live (a
// bool's 1, an ordinal in range, a printable text byte), so the dead-byte rule
// is the only rule that can refuse it.
//
// The offsets are derived from the constant-size table of §3.4 by hand, never
// from an implementation's output: the body starts after the sixteen-byte
// header, the layout's u32 length, the layout and the record's eight hash
// bytes. Each is written out beside its vector.
type fixedDeadVector struct {
	name, unit, root string
	base             string // the conforming record the vector damages
	at               int    // the byte's offset in the root's body
	value            byte   // what the vector puts there
}

var fixedDeadVectors = []fixedDeadVector{
	// KeyedConfig: teams [Team]TeamConfig is 3 * (4 + 4 + 16) = 72 bytes; the
	// Interceptor hull at 72 is health 4, mass 4, then turrets at 80; the
	// Cannon turret is damage 4, cooldown 4, then gunner's present byte at 88
	// (0: absent) and its payload, reaction 89..92 and tracking at 93.
	{"fixed_dead_optional_payload", "tabledemo", "KeyedConfig", "testdata/conformance/tables/wire/keyed_config.bin", 93, 1},
	// RootConfig: version_note string(16) is the length at 0 (9, "golden-v1")
	// and sixteen bytes at 4..19; byte 19 is past the used length.
	{"fixed_dead_text_tail", "tabledemo", "RootConfig", "testdata/conformance/tables/wire/root_full.bin", 19, 'x'},
	// RootConfig: weapons [..8]WeaponConfig is the count at 20 (2) and eight
	// 22-byte elements from 24 (damage 4, speed 4, penetration 4, channel 4,
	// homing 1, effect 1 + 4); element 5's homing is 24 + 5*22 + 16 = 150.
	{"fixed_dead_array_tail", "tabledemo", "RootConfig", "testdata/conformance/tables/wire/root_full.bin", 150, 1},
	// RootConfig: profiles [..4]ProfileConfig is the count at 200 (1) and
	// element 0 from 204: name 4 + 32, then icon bytes(16), the length at 240
	// (3) and sixteen bytes at 244..259; byte 259 is past the used length.
	{"fixed_dead_bytes_tail", "tabledemo", "RootConfig", "testdata/conformance/tables/wire/root_full.bin", 259, 1},
	// profiles[0] past icon: experience 4, tilt 1, heading 2, timestamp 8,
	// badge 1, port 2, epoch 8, precision 8, ratings 16, has_loadout 1, so
	// loadout starts at 204 + 107 = 311: grade 1, then grades [..4]Grade, the
	// count at 312 (2) and four ordinals at 316..319; ordinal 3 at 319 is
	// past the count.
	{"fixed_dead_enum_array_tail", "tabledemo", "RootConfig", "testdata/conformance/tables/wire/root_full.bin", 319, 1},
	// loadout past grades: podium 3, perks 8, so primary WeaponConfig starts
	// at 311 + 20 = 331 and its effect tag is at 331 + 17 = 348 (0: no arm);
	// the arm's four bytes are 349..352.
	{"fixed_dead_arm_none", "tabledemo", "RootConfig", "testdata/conformance/tables/wire/root_full.bin", 352, 1},
	// tblr1 Cfg: grade 1, then effect's tag at 1 (3: the payload-free arm
	// ping) and the widest arm's four bytes at 2..5, none of which ping takes.
	{"fixed_dead_arm_void", "tblr1", "Cfg", "testdata/conformance/tables/wire/r1_ping.bin", 5, 1},
}

func (v fixedDeadVector) path() string {
	return filepath.ToSlash(filepath.Join("testdata/conformance/tables/wire", v.name+".bin"))
}

// bytes is the base record with the one dead byte set. It refuses a vector
// whose byte is not zero in the base, since then it would not be dead there.
func (v fixedDeadVector) bytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(v.base)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 20 {
		t.Fatalf("%s: the base is no fixed file", v.name)
	}
	body := 16 + 4 + int(binary.LittleEndian.Uint32(data[16:])) + 8
	if body+v.at >= len(data) {
		t.Fatalf("%s: offset %d is past the body", v.name, v.at)
	}
	if data[body+v.at] != 0 {
		t.Fatalf("%s: the base holds %#x at body offset %d, so it is not slack", v.name, data[body+v.at], v.at)
	}
	out := bytes.Clone(data)
	out[body+v.at] = v.value
	return out
}

// TestFixedDeadVectorBytesArePinned holds each file to its script.
// FIXED_DEAD_WRITE=1 writes the files, which is how a new vector lands.
func TestFixedDeadVectorBytesArePinned(t *testing.T) {
	corpus(t)
	write := os.Getenv("FIXED_DEAD_WRITE") == "1"
	for _, v := range fixedDeadVectors {
		want := v.bytes(t)
		if write {
			if err := os.WriteFile(v.path(), want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(v.path())
		if err != nil {
			t.Errorf("%s: %v — run with FIXED_DEAD_WRITE=1 to write it", v.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the file is not the script's bytes — run with FIXED_DEAD_WRITE=1 to rewrite it", v.name)
		}
	}
}

// TestFixedDeadVectorsAreRefusedRows holds every vector to a manifest row that
// every leg must refuse, and the oracle to the rule: the engine reads each base
// and refuses each vector.
func TestFixedDeadVectorsAreRefusedRows(t *testing.T) {
	m, _, u := corpus(t)
	rows := map[string]Instance{}
	for _, inst := range m.Instances {
		rows[inst.Name] = inst
	}
	for _, v := range fixedDeadVectors {
		row, ok := rows[v.name]
		switch {
		case !ok:
			t.Errorf("%s: the manifest has no instance row for it", v.name)
			continue
		case !row.WireRefused:
			t.Errorf("%s: the row does not say wire-refused", v.name)
		case row.Unit != v.unit || row.Root != v.root || row.Wire != v.path():
			t.Errorf("%s: the row reads %s.%s %s, the vector is %s.%s %s", v.name, row.Unit, row.Root, row.Wire, v.unit, v.root, v.path())
		}
		unit, err := u.get(v.unit)
		if err != nil {
			t.Fatal(err)
		}
		model := tabletext.NewModel(unit)
		def := model.Lookup(v.root)
		if def == nil || !def.FixedDeclared {
			t.Fatalf("%s: %s.%s is not a fixed root", v.name, v.unit, v.root)
		}
		base, err := os.ReadFile(v.base)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := strictDecode(model, model.New(def), base, nil); !ok {
			t.Errorf("%s: the engine refuses the base %s: %v", v.name, v.base, err)
		}
		if ok, _ := strictDecode(model, model.New(def), v.bytes(t), nil); ok {
			t.Errorf("%s: the engine reads a record with a non-zero dead byte", v.name)
		}
	}
}
