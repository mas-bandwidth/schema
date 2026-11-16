package tablewire_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A present array is held to the reader's count range. Absence and a short
// fixed array retain their separate construction and padding rules.
func TestReadersHoldCountedArrayMinimum(t *testing.T) {
	t.Parallel()
	for _, arm := range []bool{false, true} {
		t.Run(map[bool]string{false: "field", true: "arm"}[arm], func(t *testing.T) {
			t.Parallel()
			decl := `table Root { items [..4]*Leaf }`
			prefix, suffix := `{"items":`, `}`
			if arm {
				decl = `union Pick { items [..4]*Leaf }
table Root { pick Pick }`
				prefix, suffix = `{"pick":{"items":`, `}}`
			}
			source := "package probe\nfixed table Leaf { n int32 }\n" + decl
			writer := listModel(t, source)
			reader := listModel(t, strings.Replace(source, "[..4]", "[2..4]", 1))
			for _, tc := range []struct {
				name, text string
				ok         bool
			}{{"short", `[{"n":7}]`, false}, {"minimum", `[{"n":7},null]`, true}, {"maximum", `[{"n":7},null,null,null]`, true}} {
				inst := place(t, writer, "Root", prefix+tc.text+suffix)
				wire, err := tablewire.Encode(writer, inst)
				if err != nil {
					t.Fatal(err)
				}
				for _, api := range []struct {
					name string
					read func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
				}{{"plain", tablewire.Decode}, {"variable", tablewire.DecodeVariable}, {"region", tablewire.DecodeRegion}, {"retain", func(m *tabletext.Model, v *tabletext.Instance, b []byte, r *tabletext.Report) (bool, error) {
					var retain tablewire.Retain
					return tablewire.DecodeRetain(m, v, b, &retain, r)
				}}, {"retain region", func(m *tabletext.Model, v *tabletext.Instance, b []byte, r *tabletext.Report) (bool, error) {
					var retain tablewire.Retain
					return tablewire.DecodeRegionRetain(m, v, b, &retain, r)
				}}} {
					out := reader.New(reader.Lookup("Root"))
					var report tabletext.Report
					ok, err := api.read(reader, out, wire, &report)
					if ok != tc.ok {
						t.Errorf("%s/%s admitted=%v want=%v err=%v report=%+v", tc.name, api.name, ok, tc.ok, err, report)
					}
					if !ok && (!report.Refused || !strings.Contains(report.Reason, "count_over_bound")) {
						t.Errorf("%s/%s unnamed count refusal: %v %+v", tc.name, api.name, err, report)
					}
				}
			}
		})
	}
	writer := listModel(t, "package probe\ntable Root {items [..4]int32}")
	reader := listModel(t, "package probe\ntable Root {items [2..4]int32}")
	wire, err := tablewire.Encode(writer, place(t, writer, "Root", `{}`))
	if err != nil {
		t.Fatal(err)
	}
	var report tabletext.Report
	if ok, err := tablewire.Decode(reader, reader.New(reader.Lookup("Root")), wire, &report); !ok {
		t.Fatalf("absent field refused: %v %+v", err, report)
	}
	writer = listModel(t, "package probe\ntable Root {items [1]int32}")
	reader = listModel(t, "package probe\ntable Root {items [3]int32}")
	wire, err = tablewire.Encode(writer, place(t, writer, "Root", `{"items":[7]}`))
	if err != nil {
		t.Fatal(err)
	}
	report = tabletext.Report{}
	if ok, err := tablewire.Decode(reader, reader.New(reader.Lookup("Root")), wire, &report); !ok {
		t.Fatalf("fixed padding refused: %v %+v", err, report)
	}
}

func TestMessageReaderHoldsCountedArrayMinimum(t *testing.T) {
	t.Parallel()
	for _, arm := range []bool{false, true} {
		t.Run(map[bool]string{false: "field", true: "arm"}[arm], func(t *testing.T) {
			t.Parallel()
			decl := `fixed table Root {items [..4]int32}`
			prefix, suffix := `{"items":`, `}`
			if arm {
				decl = `union Pick {items [..4]int32}
fixed table Root {pick Pick}`
				prefix, suffix = `{"pick":{"items":`, `}}`
			}
			source := "package probe\n" + decl
			writer := listModel(t, source)
			reader := listModel(t, strings.Replace(source, "[..4]", "[2..4]", 1))
			var vocab tablewire.Vocabulary
			var announceReport tabletext.Report
			if err := vocab.AnnounceRead(tablewire.Announce(writer.Unit), &announceReport); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name, text string
				ok         bool
			}{{"short", `[7]`, false}, {"minimum", `[7,8]`, true}, {"maximum", `[7,8,9,10]`, true}} {
				wire, err := tablewire.EncodeMessages(writer, []*tabletext.Instance{place(t, writer, "Root", prefix+tc.text+suffix)})
				if err != nil {
					t.Fatal(err)
				}
				out := reader.New(reader.Lookup("Root"))
				var report tabletext.Report
				ok, err := tablewire.DecodeMessage(reader, out, wire, &vocab, &report)
				if ok != tc.ok {
					t.Errorf("%s admitted=%v want=%v err=%v report=%+v", tc.name, ok, tc.ok, err, report)
				}
				if !ok && (!report.Refused || !strings.Contains(report.Reason, "count_over_bound")) {
					t.Errorf("%s unnamed count refusal: %v %+v", tc.name, err, report)
				}
			}
		})
	}
}

func TestMessageCountMinimumKeepsAbsenceAndFixedPadding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, from, to, text string }{
		{"absent", "[..4]", "[2..4]", `{}`},
		{"fixed padding", "[1]", "[3]", `{"items":[7]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := listModel(t, "package probe\nfixed table Root {items "+tc.from+"int32}")
			reader := listModel(t, "package probe\nfixed table Root {items "+tc.to+"int32}")
			wire, err := tablewire.EncodeMessages(writer, []*tabletext.Instance{place(t, writer, "Root", tc.text)})
			if err != nil {
				t.Fatal(err)
			}
			var vocab tablewire.Vocabulary
			var report tabletext.Report
			if err := vocab.AnnounceRead(tablewire.Announce(writer.Unit), &report); err != nil {
				t.Fatal(err)
			}
			report = tabletext.Report{}
			if ok, err := tablewire.DecodeMessage(reader, reader.New(reader.Lookup("Root")), wire, &vocab, &report); !ok {
				t.Fatalf("control refused: %v %+v", err, report)
			}
		})
	}
}
