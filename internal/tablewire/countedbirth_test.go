package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestCountedBirthDefaultsElideAndKeepLiveData(t *testing.T) {
	t.Parallel()
	for _, fixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "variable", true: "message"}[fixed], func(t *testing.T) {
			class := "table"
			if fixed {
				class = "fixed table"
			}
			source := "package birth\ntype Child { n int32 = 55\n sub [1..2]int32 }\n" + class + ` Root {items [2..3]int32
 rows [1..2]Child
 maybe ?[2..3]int32}`
			m := listModel(t, source)
			control := listModel(t, strings.ReplaceAll(strings.ReplaceAll(source, "[2..3]", "[..3]"), "[1..2]", "[..2]"))
			encode := func(model *tabletext.Model, v *tabletext.Instance) []byte {
				t.Helper()
				var out []byte
				var err error
				if fixed {
					out, err = tablewire.EncodeMessages(model, []*tabletext.Instance{v})
				} else {
					out, err = tablewire.Encode(model, v)
				}
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			want := encode(control, control.New(control.Lookup("Root")))
			fresh := m.New(m.Lookup("Root"))
			if wire := encode(m, fresh); !bytes.Equal(wire, want) {
				t.Fatalf("birth defaults do not elide: %x want %x", wire, want)
			}
			changed := place(t, m, "Root", `{"items":[0,7],"rows":[{"n":56}]}`)
			wire := encode(m, changed)
			if bytes.Equal(wire, want) {
				t.Fatal("live element change elided")
			}
			back := m.New(m.Lookup("Root"))
			var report tabletext.Report
			if fixed {
				var vocab tablewire.Vocabulary
				if err := vocab.AnnounceRead(tablewire.Announce(m.Unit), &report); err != nil {
					t.Fatal(err)
				}
				report = tabletext.Report{}
				if ok, err := tablewire.DecodeMessage(m, back, wire, &vocab, &report); !ok {
					t.Fatalf("message live read: %v %+v", err, report)
				}
			} else if ok, err := tablewire.Decode(m, back, wire, &report); !ok {
				t.Fatalf("variable live read: %v %+v", err, report)
			}
			have, err := m.Write(back)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := m.Write(changed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(have, expected) {
				t.Fatalf("live data changed: %s want %s", have, expected)
			}
		})
	}
}

func TestRequiredCountedBirthIsCarried(t *testing.T) {
	t.Parallel()
	for _, fixed := range []bool{false} {
		class := "table"
		source := "package birth\n" + class + " Root {items [2..3]int32 | required}"
		m := listModel(t, source)
		plain := listModel(t, strings.Replace(source, " | required", "", 1))
		v := m.New(m.Lookup("Root"))
		p := plain.New(plain.Lookup("Root"))
		var wire, empty []byte
		var err error
		if fixed {
			wire, err = tablewire.EncodeMessages(m, []*tabletext.Instance{v})
			if err == nil {
				empty, err = tablewire.EncodeMessages(plain, []*tabletext.Instance{p})
			}
		} else {
			wire, err = tablewire.Encode(m, v)
			if err == nil {
				empty, err = tablewire.Encode(plain, p)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(wire, empty) {
			t.Fatalf("required birth array elided, fixed=%v", fixed)
		}
	}
}

func TestPresentOptionalCountedBirthIsCarried(t *testing.T) {
	t.Parallel()
	for _, fixed := range []bool{false, true} {
		class := "table"
		if fixed {
			class = "fixed table"
		}
		m := listModel(t, "package birth\n"+class+" Root {items ?[2..3]int32}")
		absent := m.New(m.Lookup("Root"))
		present := place(t, m, "Root", `{"items":[0,0]}`)
		var a, b []byte
		var err error
		if fixed {
			a, err = tablewire.EncodeMessages(m, []*tabletext.Instance{absent})
			if err == nil {
				b, err = tablewire.EncodeMessages(m, []*tabletext.Instance{present})
			}
		} else {
			a, err = tablewire.Encode(m, absent)
			if err == nil {
				b, err = tablewire.Encode(m, present)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(a, b) {
			t.Fatalf("optional presence elided, fixed=%v", fixed)
		}
	}
}

// A valid optional sender supplies explicit defaults without depending on the
// receiver's writer. Claiming the receiver build makes those spellings illegal
// only where their original value is proved to be its birth default.
func TestMessageReaderJudgesOriginalBirthSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, writer, reader, text string
		ok                         bool
		unknown                    int
	}{
		{"stored default", `package birth
fixed table Root {items ?[2..3]int8}`, `package birth
fixed table Root {items [2..3]int8}`, `{"items":[0,0]}`, false, 0},
		{"live value", `package birth
fixed table Root {items ?[2..3]int8}`, `package birth
fixed table Root {items [2..3]int8}`, `{"items":[0,7]}`, true, 0},
		{"optional presence", `package birth
fixed table Root {items ?[2..3]int8}`, `package birth
fixed table Root {items ?[2..3]int8}`, `{"items":[0,0]}`, true, 0},
		{"sender minimum differs", `package birth
fixed table Root {items [..3]int8}`, `package birth
fixed table Root {items [2..3]int8}`, `{"items":[0,0]}`, true, 0},
		{"unknown enum", `package birth
enum E {Known,Newer}
fixed table Root {items [2..3]E}`, `package birth
enum E {Known}
fixed table Root {items [2..3]E}`, `{"items":["Newer","Newer"]}`, true, 2},
		{"nested unknown enum", `package birth
enum E {Known,Newer}
type Child {value E}
fixed table Root {items [1..2]Child}`, `package birth
enum E {Known}
type Child {value E}
fixed table Root {items [1..2]Child}`, `{"items":[{"value":"Newer"}]}`, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := listModel(t, tc.writer)
			reader := listModel(t, tc.reader)
			wire, err := tablewire.EncodeMessages(writer, []*tabletext.Instance{place(t, writer, "Root", tc.text)})
			if err != nil {
				t.Fatal(err)
			}
			announcement := tablewire.Announce(writer.Unit)
			var before, after [8]byte
			binary.LittleEndian.PutUint64(before[:], ir.BuildVersion(writer.Unit))
			binary.LittleEndian.PutUint64(after[:], ir.BuildVersion(reader.Unit))
			if bytes.Count(announcement, before[:]) != 1 {
				t.Fatal("announcement build hook drift")
			}
			announcement = bytes.Replace(announcement, before[:], after[:], 1)
			var vocab tablewire.Vocabulary
			var report tabletext.Report
			if err := vocab.AnnounceRead(announcement, &report); err != nil {
				t.Fatal(err)
			}
			report = tabletext.Report{}
			ok, err := tablewire.DecodeMessage(reader, reader.New(reader.Lookup("Root")), wire, &vocab, &report)
			if ok != tc.ok || (ok && report.Unknown != tc.unknown) {
				t.Fatalf("admitted=%v want=%v err=%v report=%+v", ok, tc.ok, err, report)
			}
			if !ok && (!report.Refused || !strings.Contains(report.Reason, "wire_damaged")) {
				t.Fatalf("unnamed birth-default refusal: %v %+v", err, report)
			}
		})
	}
}
