package gotable

import "testing"

// TestGoMessageIntegerHeldToReaderKind holds docs/SPEC-TABLES.md §3.3 on the
// generated Go message read: a message-form integer is reconstructed as the
// sender's offset plus the announced base and then held to THIS reader's bound,
// its declared range, or the domain of its declared kind where it declares
// none. A value outside that bound refuses the batch; a lawful widening still
// reads. The refused cases are the engine's shared vectors
// (internal/tablewire's TestMessageIntegerHeldToReaderKind).
func TestGoMessageIntegerHeldToReaderKind(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Probe
{
	a int8
	b int16
	c uint8
	d int32
	e uint32
	f int64
	g uint64
	h int128 | min = -1000, max = 1000
	i uint128
	j timestamp
}
`
	runGenerated(t, schema, "package probe\n"+messageIntDomainProbe)
}

const messageIntDomainProbe = `
import "testing"

func messageIntDomainAnnounce(t *testing.T) *TableVocabulary {
	t.Helper()
	var v TableVocabulary
	v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	announcement := make([]byte, AnnounceMeasure())
	if Announce(announcement) != int64(len(announcement)) {
		t.Fatal("announce")
	}
	var report TableReport
	if !AnnounceRead(&v, announcement, &report) {
		t.Fatalf("announce read: %+v", report)
	}
	return &v
}

func messageIntDomainField(t *testing.T, name string) uint64 {
	t.Helper()
	for _, f := range ProbeTableType().Fields {
		if f.Name == name {
			return f.ID
		}
	}
	t.Fatalf("field %q is not declared", name)
	return 0
}

func messageIntDomainSlot(t *testing.T, v *TableVocabulary, id uint64) int {
	t.Helper()
	for i := 0; i < int(v.Count); i++ {
		if v.Entries[i].ID == id {
			return i
		}
	}
	t.Fatalf("no announced entry names %#x", id)
	return -1
}

func messageIntDomainPut(buf []byte, at *int64, value uint64, width int64) {
	for i := int64(0); i < width; i++ {
		if value>>uint(i)&1 != 0 {
			buf[*at/8] |= 1 << uint(*at%8)
		}
		*at++
	}
}

// messageIntDomainBody is a one-body batch: the form byte, the body count, the
// field's reference, its payload, the body's terminating reference and the
// batch's zero pad.
func messageIntDomainBody(v *TableVocabulary, slot int, raw, rawHi uint64, width int64) []byte {
	bits := v.RefBits + width + v.RefBits
	buf := make([]byte, 2+(bits+7)/8)
	buf[0] = 2
	var at int64
	messageIntDomainPut(buf[2:], &at, uint64(slot)+1, v.RefBits)
	messageIntDomainPut(buf[2:], &at, raw, min(width, 64))
	if width > 64 {
		messageIntDomainPut(buf[2:], &at, rawHi, width-64)
	}
	messageIntDomainPut(buf[2:], &at, 0, v.RefBits)
	return buf
}

func TestMessageIntDomain(t *testing.T) {
	t.Parallel()

	// A ranged sender whose reconstructed value lands beyond the reader's
	// declared kind: int8 must hold -128 through 127 and uint8 0 through 255,
	// and the 64- and 128-bit kinds hold their whole domains, high lane
	// included.
	hostile := []struct {
		name, field  string
		base, baseHi uint64
		bits         int64
		raw, rawHi   uint64
	}{
		{name: "int8_above_domain", field: "a", base: 100, bits: 8, raw: 255},
		{name: "uint8_above_domain", field: "c", base: 250, bits: 8, raw: 255},
		{name: "int32_above_domain", field: "d", base: 1<<31 - 1, bits: 32, raw: 1<<32 - 1},
		{name: "uint32_above_domain", field: "e", base: 1<<32 - 1, bits: 32, raw: 1<<32 - 1},
		{name: "int64_above_domain", field: "f", base: 1<<63 - 1, bits: 64, raw: ^uint64(0)},
		{name: "uint64_above_domain", field: "g", base: ^uint64(0), bits: 64, raw: ^uint64(0)},
		{name: "int128_above_domain", field: "h", base: ^uint64(0), baseHi: 1<<63 - 1, bits: 128, raw: 0, rawHi: 1 << 63},
		{name: "uint128_above_domain", field: "i", base: ^uint64(0), baseHi: ^uint64(0), bits: 128, raw: ^uint64(0), rawHi: ^uint64(0)},
	}
	for _, tc := range hostile {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := messageIntDomainAnnounce(t)
			slot := messageIntDomainSlot(t, v, messageIntDomainField(t, tc.field))
			shape := v.Entries[slot].Shape
			shape.Packing = 1
			shape.Bits = tc.bits
			shape.Base = [2]uint64{tc.base, tc.baseHi}
			v.Entries[slot].Shape = shape
			storage := make([]Probe, 1)
			var report TableReport
			count, ok := ProbeLoadMessages(storage, v, messageIntDomainBody(v, slot, tc.raw, tc.rawHi, tc.bits), &report)
			if ok || count != 0 || !report.Refused() {
				t.Fatalf("a value outside the reader kind's domain was accepted: count=%d ok=%v report=%+v", count, ok, report)
			}
		})
	}

	// Every end a reader can hold reads: the low and high ends of the narrower
	// kinds, and a value at each of the 64- and 128-bit domains.
	ends := []struct {
		name, field string
		raw, rawHi  uint64
		wantI       int64
		wantU       uint64
		wantLo      uint64
		wantHi      uint64
		wide        bool
	}{
		{"int8_minimum", "a", 1 << 7, 0, -128, 0, 0, 0, false},
		{"int8_maximum", "a", 1<<7 - 1, 0, 127, 0, 0, 0, false},
		{"uint8_minimum", "c", 0, 0, 0, 0, 0, 0, false},
		{"uint8_maximum", "c", 255, 0, 0, 255, 0, 0, false},
		{"int32_minimum", "d", 1 << 31, 0, -1 << 31, 0, 0, 0, false},
		{"int32_maximum", "d", 1<<31 - 1, 0, 1<<31 - 1, 0, 0, 0, false},
		{"uint32_maximum", "e", 1<<32 - 1, 0, 0, 1<<32 - 1, 0, 0, false},
		{"int64_minimum", "f", 1 << 63, 0, -1 << 63, 0, 0, 0, false},
		{"int64_maximum", "f", 1<<63 - 1, 0, 1<<63 - 1, 0, 0, 0, false},
		{"uint64_maximum", "g", ^uint64(0), 0, 0, ^uint64(0), 0, 0, false},
		{"int128_minimum", "h", 0, 0, 0, 0, ^uint64(999), ^uint64(0), true},
		{"int128_maximum", "h", 2000, 0, 0, 0, 1000, 0, true},
		{"uint128_maximum", "i", ^uint64(0), ^uint64(0), 0, 0, ^uint64(0), ^uint64(0), true},
	}
	for _, tc := range ends {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := messageIntDomainAnnounce(t)
			slot := messageIntDomainSlot(t, v, messageIntDomainField(t, tc.field))
			width := v.Entries[slot].Shape.Bits
			storage := make([]Probe, 1)
			var report TableReport
			count, ok := ProbeLoadMessages(storage, v, messageIntDomainBody(v, slot, tc.raw, tc.rawHi, width), &report)
			if !ok || count != 1 || report.Refused() {
				t.Fatalf("a value at the reader kind's end was refused: count=%d ok=%v report=%+v", count, ok, report)
			}
			got := storage[0]
			switch tc.field {
			case "a":
				if int64(got.A) != tc.wantI {
					t.Fatalf("read %d, want %d", got.A, tc.wantI)
				}
			case "c":
				if uint64(got.C) != tc.wantU {
					t.Fatalf("read %d, want %d", got.C, tc.wantU)
				}
			case "d":
				if int64(got.D) != tc.wantI {
					t.Fatalf("read %d, want %d", got.D, tc.wantI)
				}
			case "e":
				if uint64(got.E) != tc.wantU {
					t.Fatalf("read %d, want %d", got.E, tc.wantU)
				}
			case "f":
				if int64(got.F) != tc.wantI {
					t.Fatalf("read %d, want %d", got.F, tc.wantI)
				}
			case "g":
				if uint64(got.G) != tc.wantU {
					t.Fatalf("read %d, want %d", got.G, tc.wantU)
				}
			case "h":
				if tc.wide && (got.H.Lo != tc.wantLo || got.H.Hi != tc.wantHi) {
					t.Fatalf("read lo=%#x hi=%#x, want lo=%#x hi=%#x", got.H.Lo, got.H.Hi, tc.wantLo, tc.wantHi)
				}
			case "i":
				if tc.wide && (got.I.Lo != tc.wantLo || got.I.Hi != tc.wantHi) {
					t.Fatalf("read lo=%#x hi=%#x, want lo=%#x hi=%#x", got.I.Lo, got.I.Hi, tc.wantLo, tc.wantHi)
				}
			}
		})
	}

	// A lawful widening, a narrower sender kind into a wider reader kind,
	// still decodes exactly.
	t.Run("int8_sender_into_int16_reader", func(t *testing.T) {
		t.Parallel()
		v := messageIntDomainAnnounce(t)
		slot := messageIntDomainSlot(t, v, messageIntDomainField(t, "b"))
		shape := v.Entries[slot].Shape
		shape.Kind = 2
		shape.Bits = 8
		v.Entries[slot].Shape = shape
		storage := make([]Probe, 1)
		var report TableReport
		count, ok := ProbeLoadMessages(storage, v, messageIntDomainBody(v, slot, 1<<7, 0, 8), &report)
		if !ok || count != 1 || report.Refused() || report.Widened != 1 {
			t.Fatalf("a narrower sender kind did not widen: count=%d ok=%v report=%+v", count, ok, report)
		}
		if storage[0].B != -128 {
			t.Fatalf("read %d, want -128", storage[0].B)
		}
	})

	// A timestamp (kind 36) is a signed sixty-four bit count with no declared
	// range, so its bound is all of int64: a pre-epoch count is lawful and
	// reads, and it is sign-extended to 128 bits before the held check.
	t.Run("timestamp_negative_reads", func(t *testing.T) {
		t.Parallel()
		v := messageIntDomainAnnounce(t)
		slot := messageIntDomainSlot(t, v, messageIntDomainField(t, "j"))
		width := v.Entries[slot].Shape.Bits
		storage := make([]Probe, 1)
		var report TableReport
		count, ok := ProbeLoadMessages(storage, v, messageIntDomainBody(v, slot, ^uint64(999999999), 0, width), &report)
		if !ok || count != 1 || report.Refused() {
			t.Fatalf("a lawful negative timestamp count was refused: count=%d ok=%v report=%+v", count, ok, report)
		}
		if storage[0].J != -1000000000 {
			t.Fatalf("read %d, want -1000000000", storage[0].J)
		}
	})
}
`
