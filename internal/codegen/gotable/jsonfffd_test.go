package gotable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestJsonFffdPerMaximalSubpart pins docs/SPEC-TABLES.md §16.3 (Definition
// D93b): the generated Go text writer replaces ill-formed UTF-8 in narrow
// string storage with one U+FFFD per maximal subpart, while the reader keeps
// refusing every ill-formed UTF-8 sequence with string_not_utf8 and Malformed.
func TestJsonFffdPerMaximalSubpart(t *testing.T) {
	t.Parallel()

	files, err := Generate(goUnit(t, jsonFffdSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["fffd_test.go"] = []byte(jsonFffdProbe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-v", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}

const jsonFffdSchema = `package probe

fixed table Root {
	s string(16)
}
`

const jsonFffdProbe = `package probe

import (
	"bytes"
	"strings"
	"testing"
)

func TestReaderRefusesIllFormedUTF8(t *testing.T) {
	illFormed := []struct {
		name string
		raw  []byte
	}{
		{"E2_82_41", []byte{0xe2, 0x82, 0x41}},
		{"F0_80_80_80", []byte{0xf0, 0x80, 0x80, 0x80}},
		{"ED_A0_80", []byte{0xed, 0xa0, 0x80}},
		{"C0_AF", []byte{0xc0, 0xaf}},
		{"F4_90_80_80", []byte{0xf4, 0x90, 0x80, 0x80}},
		{"FF", []byte{0xff}},
	}
	for _, tc := range illFormed {
		text := append(append([]byte("{\"s\":\""), tc.raw...), []byte("\"}")...)
		var dst Root
		var rep TableReport
		if RootFromJson(&dst, text, &rep) {
			t.Fatalf("reader %s: RootFromJson accepted ill-formed UTF-8", tc.name)
		}
		if !rep.Malformed || !strings.HasPrefix(rep.Reason, "string_not_utf8") {
			t.Fatalf("reader %s: report = %+v, want Malformed with string_not_utf8", tc.name, rep)
		}
	}

	// Well-formed E2 82 AC (U+20AC) reads cleanly.
	var dst Root
	var rep TableReport
	if !RootFromJson(&dst, []byte("{\"s\":\"\xe2\x82\xac\"}"), &rep) || rep.Malformed {
		t.Fatalf("reader E2_82_AC: RootFromJson refused well-formed UTF-8: %+v", rep)
	}
	if got := dst.S[:dst.SLength]; !bytes.Equal(got, []byte{0xe2, 0x82, 0xac}) {
		t.Fatalf("reader E2_82_AC: got %x, want e282ac", got)
	}
}

func TestWriterReplacesOneFffdPerMaximalSubpart(t *testing.T) {
	const fffd = "\xef\xbf\xbd"
	cases := []struct {
		name      string
		raw       []byte
		wantCount int
		wantField string
	}{
		{"E2_82_41", []byte{0xe2, 0x82, 0x41}, 1, fffd + "A"},
		{"F0_80_80_80", []byte{0xf0, 0x80, 0x80, 0x80}, 4, fffd + fffd + fffd + fffd},
		{"ED_A0_80", []byte{0xed, 0xa0, 0x80}, 3, fffd + fffd + fffd},
		{"C0_AF", []byte{0xc0, 0xaf}, 2, fffd + fffd},
		{"F4_90_80_80", []byte{0xf4, 0x90, 0x80, 0x80}, 4, fffd + fffd + fffd + fffd},
		{"FF", []byte{0xff}, 1, fffd},
		{"E2_82_AC", []byte{0xe2, 0x82, 0xac}, 0, "\xe2\x82\xac"},
	}
	for _, tc := range cases {
		var src Root
		copy(src.S[:], tc.raw)
		src.SLength = int32(len(tc.raw))
		var buf [256]byte
		n := RootToJson(&src, buf[:])
		out := buf[:n]
		if got := bytes.Count(out, []byte(fffd)); got != tc.wantCount {
			t.Fatalf("writer %s: got %d U+FFFD in %q, want %d", tc.name, got, out, tc.wantCount)
		}
		wantSub := "\"s\": \"" + tc.wantField + "\""
		if !bytes.Contains(out, []byte(wantSub)) {
			t.Fatalf("writer %s: output %q missing %q", tc.name, out, wantSub)
		}
	}
}
`
