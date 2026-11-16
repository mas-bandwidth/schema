package tablewire_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
