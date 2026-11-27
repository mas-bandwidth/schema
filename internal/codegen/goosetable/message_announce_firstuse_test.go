package goosetable

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func announceHeaderExtra(t *testing.T, source, extra []byte, ids ...uint64) []byte {
	t.Helper()
	count := int(binary.LittleEndian.Uint64(source[len(source)-8:]))
	first := len(source) - 8 - count*8
	if count != 2 || source[first-1] != 0 {
		t.Fatal("unexpected announcement framing")
	}
	out := append(slices.Clone(source[:first-1]), extra...)
	out = append(out, 0)
	out = append(out, source[first:len(source)-8]...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(count+len(ids)))
}

func announcementFirstUseCases(t *testing.T) ([][]byte, []string) {
	t.Helper()
	source := tablewire.Announce(gooseMsgUnitDir(t, "tables/backend"))
	count := binary.LittleEndian.Uint64(source[len(source)-8:])
	unused := slices.Clone(source[:len(source)-8])
	unused = binary.LittleEndian.AppendUint64(unused, ir.TableWireId("unused_trailer"))
	unused = binary.LittleEndian.AppendUint64(unused, count+1)
	reversed := slices.Clone(source)
	if count != 2 || reversed[1] != 1 || reversed[11] != 2 {
		t.Fatal("unexpected header references")
	}
	reversed[1], reversed[11] = 2, 1
	first := len(reversed) - 24
	a, b := binary.LittleEndian.Uint64(reversed[first:]), binary.LittleEndian.Uint64(reversed[first+8:])
	binary.LittleEndian.PutUint64(reversed[first:], b)
	binary.LittleEndian.PutUint64(reversed[first+8:], a)
	extra, detail, other := ir.TableWireId("extra_header"), ir.TableWireId("header_detail"), ir.TableWireId("header_other")
	cases := [][]byte{source, unused, reversed,
		announceHeaderExtra(t, source, []byte{3, 6, 1}, extra),
		announceHeaderExtra(t, source, []byte{3, 13, 4, 4, 6, 1, 0}, extra, detail),
		announceHeaderExtra(t, source, []byte{3, 30, 4}, extra, detail),
		announceHeaderExtra(t, source, []byte{3, 15, 4, 6, 1, 1}, extra, detail),
		announceHeaderExtra(t, source, []byte{3, 16, 5, 6, 1, 4, 1, 1}, extra, detail),
		announceHeaderExtra(t, source, []byte{3, 13, 4, 5, 6, 1, 0}, extra, detail, other),
		announceHeaderExtra(t, source, []byte{3, 6, 1, 3, 6, 1}, extra),
	}
	return cases, []string{"canonical", "unused", "order", "unknown scalar", "unknown nested", "unknown enum", "unknown union", "unknown keyed", "nested order", "unknown duplicate"}
}

func announcementFirstUseRed(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	cases, names := announcementFirstUseCases(t)
	got := gooseAnnounceProbe(t, gooseMsgUnitDir(t, "tables/backend"), cases, 4096, override)
	if len(got) != len(cases) {
		t.Fatal("missing announcement answers")
	}
	var red []string
	for i, c := range cases {
		want := goAnnounceVerdict(c, 4096)
		// The ordinary form-1 rule, not an expected reason shared across ports.
		read := strings.Fields(want)[0] == "1"
		forbidden := names[i] == "unused" || names[i] == "order" || names[i] == "nested order" || names[i] == "unknown duplicate"
		if read == forbidden {
			t.Fatalf("oracle %s unexpected: %s", names[i], want)
		}
		if strings.Fields(got[i])[0] != strings.Fields(want)[0] || read && got[i] != want {
			red = append(red, names[i])
		}
	}
	return red
}

func TestGooseAnnouncementFirstUse(t *testing.T) {
	if red := announcementFirstUseRed(t, nil); len(red) != 0 {
		t.Fatalf("announcement verdicts differ: %v", red)
	}
}

func TestGooseAnnouncementFirstUseNegativeControl(t *testing.T) {
	src := string(Runtime()["message.goose"])
	from := "    let check, non_finite = wire_check(data, 1, first, first, ids_n as! i64, true);\n"
	if strings.Count(src, from) != 1 {
		t.Fatal("control anchor missing")
	}
	override := map[string][]byte{"message.goose": []byte(strings.Replace(src, from, "    let check: i64 = 0; let non_finite = false; // planted: skip announcement wire check\n", 1))}
	got := announcementFirstUseRed(t, override)
	want := []string{"unused", "order", "nested order", "unknown duplicate"}
	if !slices.Equal(got, want) {
		t.Fatalf("control got %v, want exactly %v", got, want)
	}
}
