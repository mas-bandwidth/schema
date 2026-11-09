package packet

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestSeededSchemaCompiles(t *testing.T) {
	schemas := Schemas()
	if len(schemas) == 0 {
		t.Fatal("no seeded packet schemas")
	}
	for _, s := range schemas {
		dir := t.TempDir()
		path := filepath.Join(dir, "PacketCarrier.schema")
		if err := os.WriteFile(path, []byte(s.Source), 0o644); err != nil {
			t.Fatal(err)
		}
		unit, err := compiler.New().Load([]string{path})
		if err != nil {
			t.Fatalf("seeded schema %s does not compile: %v", s.Name, err)
		}
		if unit.Package != "packetcarrier" {
			t.Errorf("schema %s: package %q, want packetcarrier", s.Name, unit.Package)
		}
		if unit.Structs["Small"] == nil || unit.Structs["T"] == nil || unit.Unions["U"] == nil {
			t.Fatalf("schema %s: unit is missing Small, T or U", s.Name)
		}
		data := fieldNamed(unit.Structs["T"], "data")
		if data == nil {
			t.Fatal("T has no data field")
		}
		if ir.AlignedFixedByteArrays(unit.Structs["T"])[data] {
			t.Fatal("data is statically byte-aligned; after a union it must ride from the next bit")
		}
		if !alignBefore(unit.Structs["T"], "u") {
			t.Fatal("T has no align item before the union")
		}
	}
}

func TestSeedIsOutsideExamples(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "examples"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range Schemas() {
		src := []byte(s.Source)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(repoRoot(t), "examples", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(b, src) {
				t.Fatalf("seeded schema %s is examples/%s", s.Name, e.Name())
			}
		}
	}
}

func TestGeneratorEmitsCanonicalJSON(t *testing.T) {
	const seed = uint64(0x5EED0001)
	schemas := Schemas()
	if len(schemas) == 0 {
		t.Fatal("no seeded packet schemas")
	}
	for _, s := range schemas {
		a, err := GenerateJSON(s, seed, 64)
		if err != nil {
			t.Fatalf("schema %s: %v", s.Name, err)
		}
		b, err := GenerateJSON(s, seed, 64)
		if err != nil {
			t.Fatal(err)
		}
		if len(a) != 64 {
			t.Fatalf("schema %s: generated %d instances, want 64", s.Name, len(a))
		}
		arms := 0
		for i := range a {
			if !bytes.Equal(a[i], b[i]) {
				t.Fatalf("schema %s instance %d: generator is not deterministic", s.Name, i)
			}
			inst, err := ParseCanonicalJSON(a[i])
			if err != nil {
				t.Fatalf("schema %s instance %d: %s: %v", s.Name, i, a[i], err)
			}
			if inst.HasA {
				arms++
				if inst.V > 127 {
					t.Errorf("instance %d: v=%d outside [0, 127]", i, inst.V)
				}
			} else if inst.V != 0 {
				t.Errorf("instance %d: untaken arm carries v=%d", i, inst.V)
			}
			if again := inst.CanonicalJSON(); !bytes.Equal(again, a[i]) {
				t.Errorf("instance %d: %s re-emits as %s", i, a[i], again)
			}
		}
		if arms == 0 || arms == len(a) {
			t.Fatalf("schema %s: generator produced %d taken arms out of %d", s.Name, arms, len(a))
		}
	}
	if _, err := Generate(Schema{Name: "missing"}, seed, 1); err == nil {
		t.Fatal("an unseeded schema produced instances")
	}
}

// None with data {1, 2, 3, 4} is 1<<1 | 2<<9 | 3<<17 | 4<<25 = 0x08060402,
// 33 bits, five bytes. A taken arm fills the first byte (one tag bit plus
// seven value bits), so the array then rides on a byte boundary.
func TestHandVectorsMatchTheBitLayout(t *testing.T) {
	vectors := []struct {
		inst Instance
		wire []byte
	}{
		{Instance{Data: [4]uint8{1, 2, 3, 4}}, []byte{0x02, 0x04, 0x06, 0x08, 0x00}},
		{Instance{Data: [4]uint8{0, 0, 0, 0}}, []byte{0x00, 0x00, 0x00, 0x00, 0x00}},
		{Instance{Data: [4]uint8{0xff, 0, 0, 0}}, []byte{0xfe, 0x01, 0x00, 0x00, 0x00}},
		{Instance{HasA: true, V: 0, Data: [4]uint8{1, 2, 3, 4}}, []byte{0x01, 0x01, 0x02, 0x03, 0x04}},
		{Instance{HasA: true, V: 1, Data: [4]uint8{0, 0, 0, 0}}, []byte{0x03, 0x00, 0x00, 0x00, 0x00}},
		{Instance{HasA: true, V: 127, Data: [4]uint8{0x80, 0, 0, 1}}, []byte{0xff, 0x80, 0x00, 0x00, 0x01}},
	}
	for _, v := range vectors {
		js := v.inst.CanonicalJSON()
		for _, d := range Drivers() {
			got, err := d.Write(js)
			if err != nil {
				t.Fatalf("driver %s write %s: %v", d.Name(), js, err)
			}
			if !bytes.Equal(got, v.wire) {
				t.Errorf("driver %s wrote %x for %s, want %x", d.Name(), got, js, v.wire)
			}
			back, err := d.Read(v.wire)
			if err != nil {
				t.Fatalf("driver %s read %x: %v", d.Name(), v.wire, err)
			}
			if !bytes.Equal(back, js) {
				t.Errorf("driver %s read %x as %s, want %s", d.Name(), v.wire, back, js)
			}
		}
	}
}

func TestCrossDriverWriteRead(t *testing.T) {
	drivers := Drivers()
	if len(drivers) < 2 {
		t.Fatalf("packet carrier has %d drivers, want at least 2", len(drivers))
	}
	for _, s := range Schemas() {
		instances, err := GenerateJSON(s, 0x5EED0001, 64)
		if err != nil {
			t.Fatal(err)
		}
		for _, js := range instances {
			written := make([][]byte, len(drivers))
			for i, d := range drivers {
				wire, err := d.Write(js)
				if err != nil {
					t.Fatalf("driver %s write %s: %v", d.Name(), js, err)
				}
				written[i] = wire
				if i > 0 && !bytes.Equal(wire, written[0]) {
					t.Fatalf("driver %s wrote %x, driver %s wrote %x for %s", d.Name(), wire, drivers[0].Name(), written[0], js)
				}
			}
			for _, d := range drivers {
				for j, wire := range written {
					got, err := d.Read(wire)
					if err != nil {
						t.Fatalf("driver %s reading driver %s bytes %x: %v", d.Name(), drivers[j].Name(), wire, err)
					}
					if !bytes.Equal(got, js) {
						t.Errorf("driver %s reading driver %s: got %s, want %s", d.Name(), drivers[j].Name(), got, js)
					}
				}
			}
		}
	}
}

func TestBitFlipVerdictAgreement(t *testing.T) {
	drivers := Drivers()
	for _, s := range Schemas() {
		instances, err := GenerateJSON(s, 0x5EED0001, 16)
		if err != nil {
			t.Fatal(err)
		}
		for _, js := range instances {
			wire, err := drivers[0].Write(js)
			if err != nil {
				t.Fatal(err)
			}
			for bit := 0; bit < len(wire)*8; bit++ {
				mutant := append([]byte(nil), wire...)
				mutant[bit/8] ^= 1 << uint(bit%8)
				var want []byte
				var wantErr error
				for i, d := range drivers {
					got, err := d.Read(mutant)
					if i == 0 {
						want, wantErr = got, err
						continue
					}
					if (err == nil) != (wantErr == nil) {
						t.Fatalf("bit %d of %x: driver %s err=%v, driver %s err=%v", bit, wire, d.Name(), err, drivers[0].Name(), wantErr)
					}
					if err == nil && !bytes.Equal(got, want) {
						t.Errorf("bit %d of %x: driver %s decoded %s, driver %s decoded %s", bit, wire, d.Name(), got, drivers[0].Name(), want)
					}
				}
			}
		}
	}
}

// A None packet is 33 bits. Bits 33..39 of the flushed byte are not read,
// and an extra trailing byte is not a refusal.
func TestUnreadTailBitsStayAccepted(t *testing.T) {
	inst := Instance{Data: [4]uint8{1, 2, 3, 4}}
	js := inst.CanonicalJSON()
	for _, d := range Drivers() {
		wire, err := d.Write(js)
		if err != nil {
			t.Fatal(err)
		}
		if len(wire) != 5 {
			t.Fatalf("driver %s wrote %d bytes, want 5", d.Name(), len(wire))
		}
		for bit := 33; bit < 40; bit++ {
			mutant := append([]byte(nil), wire...)
			mutant[bit/8] ^= 1 << uint(bit%8)
			got, err := d.Read(mutant)
			if err != nil {
				t.Fatalf("driver %s bit %d: %v", d.Name(), bit, err)
			}
			if !bytes.Equal(got, js) {
				t.Errorf("driver %s bit %d: got %s, want %s", d.Name(), bit, got, js)
			}
		}
		extra := append(append([]byte(nil), wire...), 0xab)
		got, err := d.Read(extra)
		if err != nil {
			t.Fatalf("driver %s extra byte: %v", d.Name(), err)
		}
		if !bytes.Equal(got, js) {
			t.Errorf("driver %s extra byte: got %s, want %s", d.Name(), got, js)
		}
		for n := 0; n < 4; n++ {
			if _, err := d.Read(wire[:n]); err == nil {
				t.Errorf("driver %s accepted a %d-byte buffer", d.Name(), n)
			}
		}
	}
}

func TestOutOfRangeRefused(t *testing.T) {
	bad := []string{
		`{"u":{"a":{"v":128}},"data":[1,2,3,4]}`,
		`{"u":{"a":{"v":1}},"data":[1,2,3]}`,
		`{"u":{"a":{"v":1}},"data":[1,2,3,256]}`,
		`{"u":{},"data":[0,0,0,0]}`,
		`{"u":null,"data":[0,0,0,0],"extra":1}`,
	}
	for _, js := range bad {
		if _, err := ParseCanonicalJSON([]byte(js)); err == nil {
			t.Errorf("canonical JSON accepted %s", js)
		}
		for _, d := range Drivers() {
			if _, err := d.Write([]byte(js)); err == nil {
				t.Errorf("driver %s wrote %s", d.Name(), js)
			}
		}
	}
}

func fieldNamed(st *ir.Struct, name string) *ir.Field {
	for _, item := range st.Items {
		f, ok := item.(*ir.FieldItem)
		if ok && f.F.Name == name {
			return f.F
		}
	}
	return nil
}

func alignBefore(st *ir.Struct, field string) bool {
	seen := false
	for _, item := range st.Items {
		switch item := item.(type) {
		case *ir.AlignItem:
			seen = true
		case *ir.FieldItem:
			if item.F.Name == field {
				return seen
			}
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}
