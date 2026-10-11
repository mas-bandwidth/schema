package jstable

import (
	"encoding/binary"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormAnnounceJavaScript holds the ANNOUNCEMENT half of the message
// form (docs/SPEC-TABLES.md §3.3) on the JavaScript leg, one subtest per
// clause, against the announcement files the tables manifest pins. The bytes
// ARE the C++ reference's; the manifest names the units.
func TestMessageFormAnnounceJavaScript(t *testing.T) {
	t.Parallel()

	type unitCase struct {
		name string
		path string
	}
	units := []unitCase{
		{"backenddemo", "../../../tables/backend/Backend.schema"},
		{"vocabdemo", "../../../tables/vocab/Vocab.schema"},
		{"vocab9demo", "../../../tables/vocab9/Vocab9.schema"},
	}
	announcementOf := manifestAnnouncements(t)

	t.Run("constant_bytes", func(t *testing.T) {
		t.Parallel()
		// "THE WHOLE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT ...
		// a backend emits `Announce` and `AnnounceMeasure` as a constant byte
		// array and its length rather than as a walk." (docs/SPEC-TABLES.md §3.3)
		for _, uc := range units {
			uc := uc
			t.Run(uc.name, func(t *testing.T) {
				u := loadUnit(t, uc.path)
				src := homeModule(t, u)
				for _, want := range []string{"export function Announce(", "export function AnnounceMeasure(", "export class TableVocabulary"} {
					if !strings.Contains(src, want) {
						t.Fatalf("the emitted unit does not carry %q", want)
					}
				}
				got := emittedAnnouncement(t, src)
				want := announcementOf[uc.name]
				if len(want) == 0 {
					t.Fatalf("the manifest carries no announcement file for %s", uc.name)
				}
				if string(got) != string(want) {
					t.Fatalf("Announce bytes = %d, the manifest's %s = %d — they disagree", len(got), uc.name, len(want))
				}
				if want := strconv.Itoa(len(got)); !strings.Contains(src, "AnnounceMeasure") || !strings.Contains(src, "TableMessageAnnouncement.length") {
					t.Fatal("AnnounceMeasure must answer the constant array's length")
				} else {
					_ = want
				}
			})
		}
	})

	t.Run("announce_read_fills", func(t *testing.T) {
		t.Parallel()
		// "The receiver resolves once. It reads the announcement, resolves
		// every entry against its own descriptors ... THE ANNOUNCEMENT READS
		// TOLERANTLY ... The vocabulary is a pure function of the build
		// version." (docs/SPEC-TABLES.md §3.3)
		for _, uc := range units {
			uc := uc
			t.Run(uc.name, func(t *testing.T) {
				u := loadUnit(t, uc.path)
				entries := ir.TableVocabulary(u)
				v := &jsTableVocabulary{
					Entries:         make([]jsTableMessageEntry, len(entries)),
					BuildVersionOwn: ir.BuildVersion(u),
					MaxBytes:        64 * 1024,
				}
				var report jsTableReport
				if !jsAnnounceRead(v, announcementOf[uc.name], &report) {
					t.Fatalf("AnnounceRead refused a well-formed announcement: %+v", report)
				}
				if !v.Announced || v.Refused {
					t.Fatalf("the announcement did not set the vocabulary: %+v", v)
				}
				if v.Count != int64(len(entries)) {
					t.Fatalf("resolved %d entries, the unit's closure names %d", v.Count, len(entries))
				}
				if v.BuildVersion != ir.BuildVersion(u) {
					t.Fatalf("build version = %#x, the unit's own %#x", v.BuildVersion, ir.BuildVersion(u))
				}
				if v.RefBits != int64(ir.TableMessageRefBits(len(entries))) {
					t.Fatalf("reference width = %d, the closure needs %d", v.RefBits, ir.TableMessageRefBits(len(entries)))
				}
				for i, want := range entries {
					if v.Entries[i].Id != want.Id || v.Entries[i].Shape.Kind != want.Kind {
						t.Fatalf("entry %d = id %#x kind %d, the page names id %#x kind %d",
							i, v.Entries[i].Id, v.Entries[i].Shape.Kind, want.Id, want.Kind)
					}
				}
			})
		}
	})

	t.Run("second_announcement", func(t *testing.T) {
		t.Parallel()
		// "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does
		// not replace or amend anything." and "REFUSAL IS TERMINAL, and a
		// refused FIRST announcement is the one the connection gets ... every
		// announcement after it is refused as `second_announcement`."
		// (docs/SPEC-TABLES.md §3.3)
		ann := announcementOf["backenddemo"]
		n := len(ir.TableVocabulary(loadUnit(t, units[0].path)))
		v := &jsTableVocabulary{Entries: make([]jsTableMessageEntry, n), BuildVersionOwn: ir.BuildVersion(loadUnit(t, units[0].path))}
		var first jsTableReport
		if !jsAnnounceRead(v, ann, &first) {
			t.Fatalf("first announcement: %+v", first)
		}
		before := v.Count
		var second jsTableReport
		if jsAnnounceRead(v, ann, &second) || second.Verdict != jsOpenRefused || second.Reason != "second_announcement" || second.Malformed {
			t.Fatalf("a second announcement was not refused by name: %+v", second)
		}
		if v.Count != before {
			t.Fatal("a second announcement replaced the vocabulary")
		}
		w := &jsTableVocabulary{Entries: make([]jsTableMessageEntry, n)}
		var rejected jsTableReport
		if jsAnnounceRead(w, []byte{9, 9, 9}, &rejected) || rejected.Verdict != jsOpenRefused || rejected.Reason != "newer_form" || w.Announced {
			t.Fatalf("a refused first announcement was not refused: %+v", rejected)
		}
		var later jsTableReport
		if jsAnnounceRead(w, ann, &later) || later.Reason != "second_announcement" || w.Announced {
			t.Fatalf("an announcement after a refused first is not second_announcement: %+v", later)
		}
	})

	t.Run("vocabulary_too_large", func(t *testing.T) {
		t.Parallel()
		// "`vocabulary_too_large` covers two bounds: ... THE ENTRY BOUND IS
		// THAT CAPACITY, and an announcement above it is refused by name
		// `vocabulary_too_large` before an entry is touched" and the byte
		// bound's "64 KiB does not move" (docs/SPEC-TABLES.md §3.3)
		ann := announcementOf["backenddemo"]
		entries := &jsTableVocabulary{Entries: make([]jsTableMessageEntry, 1)}
		var report jsTableReport
		if jsAnnounceRead(entries, ann, &report) || report.Verdict != jsOpenRefused || report.Reason != "vocabulary_too_large" || report.Malformed || entries.Announced {
			t.Fatalf("entry count bound: %+v", report)
		}
		bytes := &jsTableVocabulary{Entries: make([]jsTableMessageEntry, 64), MaxBytes: 4}
		var byteReport jsTableReport
		if jsAnnounceRead(bytes, ann, &byteReport) || byteReport.Verdict != jsOpenRefused || byteReport.Reason != "vocabulary_too_large" || byteReport.Malformed || bytes.Announced {
			t.Fatalf("vocabulary byte bound: %+v", byteReport)
		}
	})

	t.Run("announcement_strict_checks", func(t *testing.T) {
		t.Parallel()
		// "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS:
		// the build version present, exactly once, kind `9`, eight bytes wide,
		// and the vocabulary present, exactly once, kind `14`, element kind
		// `6` ... A FAILED STRICT CHECK IS MALFORMED, not a refusal ... The
		// build version is kept the moment it is read." (docs/SPEC-TABLES.md §3.3)
		const version = 0x1122334455667788
		twice := append(versionField(version), versionField(version)...)
		twice = append(twice, vocabField(6, nil)...)
		short := append([]byte{1, 9}, 0, 0, 0, 0)
		cases := []struct {
			name string
			data []byte
		}{
			{"build version missing", announcement(vocabField(6, nil))},
			{"build version twice", announcement(twice)},
			{"build version not kind 9", announcement(append([]byte{1, 5, 0, 0, 0, 0, 0, 0, 0, 0}, vocabField(6, nil)...))},
			{"build version fewer than eight bytes", announcement(short)},
			{"vocabulary missing", announcement(versionField(version))},
			{"vocabulary not kind 14", announcement(append(versionField(version), vocabFieldBadKind(13)...))},
			{"vocabulary element not kind 6", announcement(append(versionField(version), vocabField(9, nil)...))},
		}
		for _, tc := range cases {
			v := &jsTableVocabulary{Entries: make([]jsTableMessageEntry, 8)}
			var report jsTableReport
			if jsAnnounceRead(v, tc.data, &report) || !report.Malformed || report.Verdict == jsOpenRefused || v.Announced {
				t.Fatalf("%s: want malformed and not a refusal, got %+v", tc.name, report)
			}
		}
		v := &jsTableVocabulary{Entries: make([]jsTableMessageEntry, 8)}
		var report jsTableReport
		if jsAnnounceRead(v, announcement(append(versionField(version), 2, 13)), &report) || !report.Malformed || v.BuildVersion != version || v.Announced {
			t.Fatalf("the refused announcement did not keep the build version: %+v version=%#x", report, v.BuildVersion)
		}
	})
}

// manifestAnnouncements reads the tables manifest's `connection` lines and
// returns each unit's announcement file bytes, keyed by unit (docs/
// SPEC-TABLES.md §3.3; testdata/conformance/tables/MANIFEST.txt).
func manifestAnnouncements(t *testing.T) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/tables/MANIFEST.txt")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	out := map[string][]byte{}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "connection" {
			continue
		}
		wire, err := os.ReadFile("../../../" + f[4])
		if err != nil {
			t.Fatalf("read announcement %s: %v", f[4], err)
		}
		out[f[2]] = wire
	}
	return out
}

// homeModule returns the unit's runtime home module — the one that carries the
// shared runtime (docs/SPEC-TABLES.md §19.2), found by the announcement surface
// it now declares.
func homeModule(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, b := range files {
		if strings.Contains(string(b), "TableMessageAnnouncement = new Uint8Array(") {
			return string(b)
		}
	}
	t.Fatal("the generated unit carries no announcement surface")
	return ""
}

var announceArrayRe = regexp.MustCompile(`TableMessageAnnouncement = new Uint8Array\(\[([^\]]*)\]\)`)

func emittedAnnouncement(t *testing.T, src string) []byte {
	t.Helper()
	m := announceArrayRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("the generated module has no TableMessageAnnouncement array")
	}
	var out []byte
	for _, f := range strings.Split(m[1], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("bad byte literal %q: %v", f, err)
		}
		out = append(out, byte(v))
	}
	return out
}

// ---- the framed announcements the strict-check clause drives, on the same
// framing the Go leg's refusals test uses (docs/SPEC-TABLES.md §3.3): form
// byte 1, the fields, the zero terminator, and the two reserved trailer ids.

func lebs(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		out = append(out, c)
		if v == 0 {
			return out
		}
	}
}

func versionField(version uint64) []byte {
	return binary.LittleEndian.AppendUint64([]byte{1, 9}, version)
}

func vocabField(elem uint8, vocab []byte) []byte {
	sub := append([]byte{elem}, lebs(uint64(len(vocab)))...)
	sub = append(sub, vocab...)
	field := []byte{2, 14}
	field = append(field, lebs(uint64(len(sub)))...)
	return append(field, sub...)
}

func vocabFieldBadKind(kind uint8) []byte {
	sub := append([]byte{6}, lebs(0)...)
	field := []byte{2, kind}
	field = append(field, lebs(uint64(len(sub)))...)
	return append(field, sub...)
}

func announcement(fields []byte) []byte {
	out := append([]byte{1}, fields...)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	return binary.LittleEndian.AppendUint64(out, 2)
}
