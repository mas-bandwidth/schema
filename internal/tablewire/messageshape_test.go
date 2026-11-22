package tablewire

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func shapeVocabulary(t *testing.T, writer, reader *tabletext.Model, syntheticOwn bool) Vocabulary {
	t.Helper()
	var v Vocabulary
	if err := v.AnnounceRead(Announce(writer.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	// This is deliberately synthetic. The real writer's announcement/body is
	// tested first; then only the claimed build changes, retaining its shape.
	if syntheticOwn {
		v.buildVersion = ir.BuildVersion(reader.Unit)
	}
	return v
}

func TestMessageReadDifferentShape(t *testing.T) {
	cases := []struct {
		name, writer, reader, input string
		values                      []uint64
	}{
		{"default", "fixed table Root { value uint8 = 1 | min = 0, max = 15 }", "fixed table Root { value uint8 | min = 0, max = 5 }", `{"value":0}`, []uint64{0}},
		{"shared id other record", "fixed table Root { value uint8 = 1 | min = 0, max = 15 }\nfixed table Other { value uint8 | min = 0, max = 15 }", "fixed table Root { value uint8 | min = 0, max = 5 }\nfixed table Other { value uint8 | min = 0, max = 15 }", `{"value":0}`, []uint64{0}},
		{"order", "fixed table Root { b uint8 | min = 0, max = 15\n a uint8 | min = 0, max = 15 }", "fixed table Root { a uint8 | min = 0, max = 5\n b uint8 | min = 0, max = 5 }", `{"a":1,"b":2}`, []uint64{1, 2}},
		{"keyed order", "enum Key { Second, First }\nfixed table Root { counts [Key]uint8 | min = 0, max = 15 }", "enum Key { First, Second }\nfixed table Root { counts [Key]uint8 | min = 0, max = 5 }", `{"counts":{"First":1,"Second":2}}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer, reader := covModel(t, "package shapex\n"+tc.writer+"\n"), covModel(t, "package shapex\n"+tc.reader+"\n")
			wire, err := EncodeMessage(writer, covPlace(t, writer, "Root", tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if ir.BuildVersion(writer.Unit) == ir.BuildVersion(reader.Unit) {
				t.Fatal("genuine writers unexpectedly share a build")
			}
			for _, synthetic := range []bool{false, true} {
				v := shapeVocabulary(t, writer, reader, synthetic)
				value := reader.New(reader.Lookup("Root"))
				var rep tabletext.Report
				ok, err := DecodeMessage(reader, value, wire, &v, &rep)
				if !ok || err != nil || rep.Refused {
					t.Fatalf("changed-shape synthetic=%v announcement refused: %v %+v", synthetic, err, rep)
				}
				for i, want := range tc.values {
					if value.Fields[i].Cell.U != want {
						t.Fatalf("value changed: %+v", value.Fields)
					}
				}
				if tc.values == nil && (value.Fields[0].Elems[0].U != 1 || value.Fields[0].Elems[1].U != 2) {
					t.Fatal("keyed values changed")
				}
			}
		})
	}
}

func shapeRaw(m *tabletext.Model, put func(*bitEncoder, *bitWriter)) []byte {
	e := &bitEncoder{m: m, slots: MessageVocabularySlots(m.Unit), refBits: ir.TableMessageRefBits(len(MessageVocabulary(m.Unit)))}
	w := &bitWriter{}
	w.put(0, 8)
	put(e, w)
	w.put(0, e.refBits)
	w.align()
	return append([]byte{ir.TableWireMessageForm}, w.b...)
}

func TestMessageMatchingShapeStrict(t *testing.T) {
	scalar := covModel(t, "package strictshape\nfixed table Root { value uint8 | min = 0, max = 5 }\n")
	order := covModel(t, "package strictshape\nfixed table Root { a uint8 | min = 0, max = 5\n b uint8 | min = 0, max = 5 }\n")
	keyed := covModel(t, "package strictshape\nenum Key { First, Second }\nfixed table Root { counts [Key]uint8 | min = 0, max = 5 }\n")
	emit := func(m *tabletext.Model, e *bitEncoder, w *bitWriter, i int, value uint64) {
		entry := ir.TableFieldEntry(m.Lookup("Root").Fields[i])
		e.ref(w, entry)
		w.put(value, int(entry.Shape.Bits))
	}
	cases := []struct {
		name string
		m    *tabletext.Model
		wire []byte
	}{
		{"default", scalar, shapeRaw(scalar, func(e *bitEncoder, w *bitWriter) { emit(scalar, e, w, 0, 0) })},
		{"order", order, shapeRaw(order, func(e *bitEncoder, w *bitWriter) { emit(order, e, w, 1, 2); emit(order, e, w, 0, 1) })},
	}
	for _, badDefault := range []bool{false, true} {
		wire := shapeRaw(keyed, func(e *bitEncoder, w *bitWriter) {
			f := keyed.Lookup("Root").Fields[0]
			entry := ir.TableFieldEntry(f)
			e.ref(w, entry)
			if badDefault {
				w.put(1, ir.TableMessageBitsRequired(0, entry.Shape.Max))
				e.name(w, ir.TableWireId(f.KeyEnumRef.VariantWireName(0)))
				w.put(0, int(entry.Shape.Inner.Bits))
				return
			}
			w.put(2, ir.TableMessageBitsRequired(0, entry.Shape.Max))
			for _, i := range []int{1, 0} {
				e.name(w, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
				w.put(uint64(i+1), int(entry.Shape.Inner.Bits))
			}
		})
		name := "keyed order"
		if badDefault {
			name = "keyed default"
		}
		cases = append(cases, struct {
			name string
			m    *tabletext.Model
			wire []byte
		}{name, keyed, wire})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := shapeVocabulary(t, tc.m, tc.m, false)
			var rep tabletext.Report
			ok, err := DecodeMessage(tc.m, tc.m.New(tc.m.Lookup("Root")), tc.wire, &v, &rep)
			if ok || RefusalReason(err) != ReasonWireDamaged {
				t.Fatalf("matching shape %s read: ok=%v err=%v", tc.name, ok, err)
			}
		})
	}
}

func TestMessageShapeIndependentStrict(t *testing.T) {
	writer := covModel(t, "package independent\nfixed table Root { value uint8 = 1 | min = 0, max = 15\n extra uint8 }\n")
	reader := covModel(t, "package independent\nfixed table Root { value uint8 | min = 0, max = 5 }\n")
	duplicate := shapeRaw(writer, func(e *bitEncoder, w *bitWriter) {
		entry := ir.TableFieldEntry(writer.Lookup("Root").Fields[0])
		for _, n := range []uint64{2, 3} {
			e.ref(w, entry)
			w.put(n, int(entry.Shape.Bits))
		}
	})
	member, err := EncodeMessage(writer, covPlace(t, writer, "Root", `{"value":2,"extra":3}`))
	if err != nil {
		t.Fatal(err)
	}
	valid, err := EncodeMessage(writer, covPlace(t, writer, "Root", `{"value":2}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		wire []byte
	}{{"duplicate", duplicate}, {"membership", member}, {"extent", valid[:len(valid)-1]}} {
		t.Run(tc.name, func(t *testing.T) {
			v := shapeVocabulary(t, writer, reader, true)
			var rep tabletext.Report
			ok, err := DecodeMessage(reader, reader.New(reader.Lookup("Root")), tc.wire, &v, &rep)
			if ok || RefusalReason(err) != ReasonWireDamaged {
				t.Fatalf("different shape %s read: ok=%v err=%v", tc.name, ok, err)
			}
		})
	}
}

func TestMessageShapeNegativeControls(t *testing.T) {
	for _, tc := range []struct{ from, to, run, want string }{
		{"exact := ownBuild && entry.Key() == mine.Key()", "exact := ownBuild", "^TestMessageReadDifferentShape$", "changed-shape synthetic=true announcement refused"},
		{"if seen[entry.Id] {", "if false && seen[entry.Id] {", "^TestMessageShapeIndependentStrict$", "different shape duplicate read: ok=true"},
	} {
		out, failed := slowtest.RunSabotaged(t, "../..", "./internal/tablewire", tc.run, slowtest.Sabotage{File: "internal/tablewire/messagedecode.go", From: tc.from, To: tc.to})
		if !failed || !strings.Contains(out, tc.want) {
			t.Errorf("control %s did not fail at intended check:\n%s", tc.run, out)
		}
	}
}

// Exactness does not make projected values evidence of their stored spelling.
func TestMessageKeyedOriginalSpelling(t *testing.T) {
	for _, tc := range []struct{ name, writer, reader, input string }{
		{"quantized", "enum Key { First }\nfixed table Root { counts [Key]float32 | min = 0, max = 1, resolution = 0.1 }", "enum Key { First }\nfixed table Root { counts [Key]float32 | min = 0, max = 1, resolution = 0.1 }", `{"counts":{"First":0.01}}`},
		{"deprecated", "enum Key { First }\nenum Mode { Idle, Run, Fly }\nfixed table Root { counts [Key]Mode }", "enum Key { First }\nenum Mode { Idle\n Run | deprecated\n Fly }\nfixed table Root { counts [Key]Mode }", `{"counts":{"First":"Run"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer, reader := covModel(t, "package rawkeyed\n"+tc.writer+"\n"), covModel(t, "package rawkeyed\n"+tc.reader+"\n")
			wire, err := EncodeMessage(writer, covPlace(t, writer, "Root", tc.input))
			if err != nil {
				t.Fatal(err)
			}
			v := shapeVocabulary(t, writer, reader, true)
			var report tabletext.Report
			out := reader.New(reader.Lookup("Root"))
			ok, err := DecodeMessage(reader, out, wire, &v, &report)
			if !ok || err != nil || report.Refused {
				t.Fatalf("projected default was mistaken for stored default: %v %+v", err, report)
			}
			if len(out.Fields[0].Elems) != 1 {
				t.Fatal("missing keyed slot")
			}
			if tc.name == "quantized" && out.Fields[0].Elems[0].F != 0 {
				t.Fatal("quantized value changed")
			}
			if tc.name == "deprecated" && (report.Unknown != 0 || out.Fields[0].Elems[0].U != reader.FieldDefaultCell(reader.Lookup("Root").Fields[0]).U) {
				t.Fatal("deprecated substitution changed")
			}
		})
	}
}
