package luautable_test

// THE MESSAGE FORM'S ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE, so every field
// reference and every length in it is a CANONICAL unsigned LEB128
// (docs/SPEC-TABLES.md §3): one value has one spelling, and `0x80 0x00` is
// not zero's spelling. This test re-spells the first field reference and the
// vocabulary length of the message form's golden announcement and holds the
// Luau announcement read to the same verdict tablewire gives the same bytes.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// announceGolden reads the message form's golden announcement, the unit that
// writes it, and the two offsets this test re-spells: the first field
// reference and the vocabulary field's own length. Both are found by walking
// the body, not typed in.
func announceGolden(t *testing.T) (ann []byte, unitPaths []string, refOff, vocabLenOff int) {
	t.Helper()
	units, conns, rows := messageManifest(t)
	var row messageRow
	for _, r := range rows {
		if r.name == "login_full" {
			row = r
			break
		}
	}
	if row.name == "" {
		t.Fatal("the manifest names no login_full message row")
	}
	conn, ok := conns[row.connection]
	if !ok {
		t.Fatalf("message %s names connection %s with no connection row", row.name, row.connection)
	}
	ann, err := os.ReadFile(filepath.Join(root, conn.announcement))
	if err != nil {
		t.Fatal(err)
	}
	if len(ann) < 2 || ann[0] != 1 {
		t.Fatalf("%s is not a form-1 announcement", conn.announcement)
	}
	vocabLenOff = -1
	at := 1
	first := true
	for {
		off := at
		ref, next, ok := readCanonicalLeb(ann, at)
		if !ok {
			t.Fatalf("%s: field reference at %d is not canonical", conn.announcement, at)
		}
		if first {
			refOff = off
			first = false
		}
		at = next
		if ref == 0 {
			break
		}
		if at >= len(ann) {
			t.Fatalf("%s: field reference %d runs off the body", conn.announcement, ref)
		}
		kind := ann[at]
		at++
		switch kind {
		case 9:
			at += 8
		case 14:
			vocabLenOff = at
			n, nnext, ok := readCanonicalLeb(ann, at)
			if !ok {
				t.Fatalf("%s: vocabulary length at %d is not canonical", conn.announcement, at)
			}
			at = nnext + int(n)
		default:
			t.Fatalf("%s: field reference %d has kind %d, which this test does not walk", conn.announcement, ref, kind)
		}
	}
	if vocabLenOff < 0 {
		t.Fatalf("%s carries no vocabulary field", conn.announcement)
	}
	return ann, units[conn.unit], refOff, vocabLenOff
}

// readCanonicalLeb is the oracle's own rule read out for offset finding: the
// golden is canonical, so a redundant continuation or a tenth byte above one
// is a test fixture error here.
func readCanonicalLeb(b []byte, off int) (uint64, int, bool) {
	var v uint64
	var s uint
	for i := 0; ; i++ {
		if off >= len(b) || i >= 10 {
			return 0, off, false
		}
		x := b[off]
		off++
		if i == 9 && x > 1 {
			return 0, off, false
		}
		v |= uint64(x&0x7f) << s
		if x&0x80 == 0 {
			if i > 0 && x == 0 {
				return 0, off, false
			}
			return v, off, true
		}
		s += 7
	}
}

func minimalLeb(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// nonMinimalLeb spells v with one redundant continuation: the minimal
// spelling's last byte gains its continuation bit and a zero follows it.
func nonMinimalLeb(v uint64) []byte {
	out := minimalLeb(v)
	out[len(out)-1] |= 0x80
	return append(out, 0x00)
}

// overlongLeb spells v in exactly n bytes, every byte before the last carrying
// a continuation, so a reader that ignores canonicity still ends on v.
func overlongLeb(v uint64, n int) []byte {
	out := minimalLeb(v)
	out[len(out)-1] |= 0x80
	for len(out) < n-1 {
		out = append(out, 0x80)
	}
	return append(out, 0x00)
}

// respell replaces the canonical LEB at off with spelled, returning a fresh
// announcement.
func respell(t *testing.T, ann []byte, off int, spelled []byte) []byte {
	t.Helper()
	_, next, ok := readCanonicalLeb(ann, off)
	if !ok {
		t.Fatalf("no canonical LEB at %d", off)
	}
	out := make([]byte, 0, len(ann)-(next-off)+len(spelled))
	out = append(out, ann[:off]...)
	out = append(out, spelled...)
	return append(out, ann[next:]...)
}

// goAnnounce is the oracle: schema-go's own announcement read on the same
// bytes, answering the two facts this test compares.
func goAnnounce(data []byte) (announced, malformed bool) {
	var v tablewire.Vocabulary
	var report tabletext.Report
	_ = v.AnnounceRead(data, &report)
	return v.Announced(), report.Malformed
}

const announceProbe = `local M = require("./schema_message")
local inputs = require("./inputs")
local v = M.NewVocabulary()
local report = { malformed = false, verdict = 0, reason = "", unknown = 0 }
local ok = M.AnnounceRead(v, inputs.ann, report)
print(tostring(ok) .. " " .. tostring(report.malformed))
`

func luauAnnounce(t *testing.T, dir string, data []byte) (announced, malformed bool) {
	t.Helper()
	out := string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"ann": data}))
	var ok, bad bool
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%t %t", &ok, &bad); err != nil {
		t.Fatalf("the announce probe printed %q: %v", out, err)
	}
	return ok, bad
}

// TestLuauMessageAnnouncementRefusesNonMinimalLeb holds §3's one spelling for
// the announcement's first field reference and vocabulary length: a redundant
// continuation is malformed, an overlong eleven-byte spelling is malformed,
// and the golden still announces. The expected verdicts are tablewire's on the
// same bytes, not constants.
func TestLuauMessageAnnouncementRefusesNonMinimalLeb(t *testing.T) {
	t.Parallel()
	ann, unitPaths, refOff, vocabLenOff := announceGolden(t)
	dir, mods, _ := buildMessageUnit(t, unitPaths)
	var probe strings.Builder
	probe.WriteString(`require("./schema_message")
require("./inputs")
`)
	for _, m := range mods {
		fmt.Fprintf(&probe, "require(\"./%s\")\n", m)
	}
	probe.WriteString(announceProbe)
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	refValue, _, _ := readCanonicalLeb(ann, refOff)
	vocabValue, _, _ := readCanonicalLeb(ann, vocabLenOff)

	cases := []struct {
		name string
		data []byte
	}{
		{"golden", ann},
		{"first_reference_nonminimal", respell(t, ann, refOff, nonMinimalLeb(refValue))},
		{"vocabulary_length_nonminimal", respell(t, ann, vocabLenOff, nonMinimalLeb(vocabValue))},
		{"first_reference_eleven_byte_overlong", respell(t, ann, refOff, overlongLeb(refValue, 11))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantAnnounced, wantMalformed := goAnnounce(c.data)
			gotAnnounced, gotMalformed := luauAnnounce(t, dir, c.data)
			if gotAnnounced != wantAnnounced || gotMalformed != wantMalformed {
				t.Fatalf("Luau AnnounceRead = announced %t malformed %t, tablewire = announced %t malformed %t",
					gotAnnounced, gotMalformed, wantAnnounced, wantMalformed)
			}
		})
	}
	if _, bad := goAnnounce(ann); bad {
		t.Fatal("the golden announcement is malformed to the oracle")
	}
	for _, c := range cases[1:] {
		if announced, bad := goAnnounce(c.data); announced || !bad {
			t.Fatalf("%s is not malformed to the oracle: announced %t malformed %t", c.name, announced, bad)
		}
	}
}
