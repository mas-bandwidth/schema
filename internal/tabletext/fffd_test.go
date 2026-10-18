package tabletext_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTextFffdPerMaximalSubpart holds the pack text reader and writer to
// docs/SPEC-TABLES.md §16.3's maximal-subpart rule: the READER refuses an
// ill-formed sequence by name (`string_not_utf8`) rather than replacing it, and
// the WRITER, answering for storage a program built, emits one U+FFFD per
// maximal subpart — the worked examples E2 82 41, F0 80 80 80, ED A0 80,
// C0 AF, F4 90 80 80 and FF, with a well-formed E2 82 AC passing unchanged.
func TestTextFffdPerMaximalSubpart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want int
	}{
		{"E2 82 41", []byte{0xe2, 0x82, 0x41}, 1},
		{"F0 80 80 80", []byte{0xf0, 0x80, 0x80, 0x80}, 4},
		{"ED A0 80", []byte{0xed, 0xa0, 0x80}, 3},
		{"C0 AF", []byte{0xc0, 0xaf}, 2},
		{"F4 90 80 80", []byte{0xf4, 0x90, 0x80, 0x80}, 4},
		{"FF", []byte{0xff}, 1},
	}
	valid := []byte{0xe2, 0x82, 0xac}

	// READER: §16.3 refuses ill-formed text by name at every string site.
	for _, c := range cases {
		for _, site := range []struct{ table, key string }{
			{"GunnerSettings", "callsign"},
			{"GlobalSettings", "build_note"},
		} {
			text := `{ "` + site.key + `": "` + string(c.in) + `" }`
			_, _, r := read(t, site.table, text)
			if !r.Malformed || !strings.HasPrefix(r.Reason, "string_not_utf8") {
				t.Fatalf("%s on %s.%s: report %+v", c.name, site.table, site.key, r)
			}
		}
	}
	// A well-formed sequence passes through the reader unchanged.
	_, inst, r := read(t, "GunnerSettings", `{ "callsign": "`+string(valid)+`" }`)
	if !r.Silent() {
		t.Fatalf("valid narrow refused: %+v", r)
	}
	if got := field(t, inst, "callsign").Cell.Str; !bytes.Equal(got, valid) {
		t.Fatalf("valid narrow read % x, want % x", got, valid)
	}

	// WRITER: one U+FFFD per maximal subpart for storage built in code, and
	// the string it writes is valid UTF-8.
	for _, c := range cases {
		m, inst, _ := read(t, "GunnerSettings", `{ "callsign": "ok" }`)
		fv := field(t, inst, "callsign")
		fv.Cell.Str = append([]byte(nil), c.in...)
		out, err := m.WriteValue(fv)
		if err != nil {
			t.Fatalf("%s: write: %v", c.name, err)
		}
		if got := bytes.Count(out, []byte("\xef\xbf\xbd")); got != c.want {
			t.Fatalf("%s: %d U+FFFD, want %d: %s", c.name, got, c.want, out)
		}
		if !utf8.Valid(out) || len(out) < 2 || out[0] != '"' || out[len(out)-1] != '"' {
			t.Fatalf("%s: writer emitted not one UTF-8 string: %s", c.name, out)
		}
	}
	// A well-formed sequence passes through the writer unchanged.
	m, inst, _ := read(t, "GunnerSettings", `{ "callsign": "ok" }`)
	fv := field(t, inst, "callsign")
	fv.Cell.Str = append([]byte(nil), valid...)
	out, err := m.WriteValue(fv)
	if err != nil {
		t.Fatalf("valid write: %v", err)
	}
	if !bytes.Contains(out, valid) || bytes.Contains(out, []byte("\xef\xbf\xbd")) {
		t.Fatalf("valid changed: %s", out)
	}
}
