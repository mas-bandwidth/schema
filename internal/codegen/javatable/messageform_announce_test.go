package javatable

// messageform_announce — the Java leg's MESSAGE FORM announcement surface
// (docs/SPEC-TABLES.md §3.3, form byte 2), which the leg carries none of today.
// One subtest per clause of the card:
//
//   2.1 Announce and AnnounceMeasure are a compile-time constant byte array and
//       its length, equal byte for byte to the manifest's announcement file for
//       the units backenddemo, vocabdemo and vocab9demo.
//   2.2 AnnounceRead accepts each of those files and fills TableVocabulary with
//       the entries the page names, the build version kept.
//   2.3 the refusals second_announcement (and refusal is terminal) and
//       vocabulary_too_large (both bounds), and the two strict checks answered
//       `malformed`.
//
// The rows are the reference's own bytes, pinned by
// testdata/conformance/tables/MANIFEST.txt's `connection` lines.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// announceRow is one `connection` line: the unit, and the announcement file its
// generated announce must equal byte for byte.
type announceRow struct {
	unit   string
	dir    string
	golden string
}

func announceRows(t *testing.T) []announceRow {
	t.Helper()
	return []announceRow{
		{unit: "backenddemo", dir: "tables/backend", golden: "testdata/wire/tables/backend_conn.bin"},
		{unit: "vocabdemo", dir: "tables/vocab", golden: "testdata/wire/tables/vocab_conn.bin"},
		{unit: "vocab9demo", dir: "tables/vocab9", golden: "testdata/wire/tables/vocab9_conn.bin"},
	}
}

// loadUnitDir parses one directory's *.schema files into one unit.
func loadUnitDir(t *testing.T, root, dir string) *ir.Unit {
	t.Helper()
	abs := filepath.Join(root, dir)
	entries, err := os.ReadDir(abs)
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") || len(e.Name()) <= len(".schema") {
			continue
		}
		name := filepath.Join(abs, e.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(name, data)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		files = append(files, check.SourceFile{Path: name, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: data, AST: f})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}

// runMessageJava compiles one unit's generated TableVocabulary beside a driver
// and runs the driver, returning its combined stdout.
func runMessageJava(t *testing.T, u *ir.Unit, main string) string {
	t.Helper()
	javac, javaBin := javaTools(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src, ok := files[messageAnnouncementClass+".java"]
	if !ok {
		t.Fatalf("%s: no %s.java emitted", u.Package, messageAnnouncementClass)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, messageAnnouncementClass+".java"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := "package " + u.Package + ";\n\n" + driverPrologue + "\n" + main + "}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "Driver.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-nowarn", "-d", classes,
		filepath.Join(srcDir, messageAnnouncementClass+".java"), filepath.Join(srcDir, "Driver.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(javaBin, "-ea", "-cp", classes, u.Package+".Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	return string(out)
}

// driverPrologue is shared by every driver: the class header and helpers.
const driverPrologue = `public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static String hex(byte[] b) {
        final StringBuilder out = new StringBuilder();
        for (final byte v : b) { out.append(String.format("%02x", v)); }
        return out.toString();
    }

    static byte[] fromHex(String s) {
        final byte[] out = new byte[s.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }
`

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	var s strings.Builder
	for _, v := range b {
		s.WriteByte(digits[v>>4])
		s.WriteByte(digits[v&0xf])
	}
	return s.String()
}

// TestMessageFormAnnounceJava pins the announcement surface (docs/SPEC-TABLES.md
// §3.3). Each subtest compiles the generated TableVocabulary and a driver, runs
// it, and compares the result to the reference.
func TestMessageFormAnnounceJava(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("2.1", func(t *testing.T) {
		t.Parallel()
		for _, row := range announceRows(t) {
			row := row
			t.Run(row.unit, func(t *testing.T) {
				t.Parallel()
				// "Announce and AnnounceMeasure are a compile-time constant byte
				// array and its length, equal byte for byte to the manifest's
				// announcement file." (§3.3)
				u := loadUnitDir(t, root, row.dir)
				want, err := os.ReadFile(filepath.Join(root, row.golden))
				if err != nil {
					t.Fatal(err)
				}
				out := runMessageJava(t, u, announceMain)
				if got := strings.TrimSpace(out); got != hexOf(want) {
					t.Fatalf("%s: announce = %s, want %s", row.unit, got, hexOf(want))
				}
			})
		}
	})

	t.Run("2.2", func(t *testing.T) {
		t.Parallel()
		for _, row := range announceRows(t) {
			row := row
			t.Run(row.unit, func(t *testing.T) {
				t.Parallel()
				// "AnnounceRead accepts each of those files and fills
				// TableVocabulary with the entries the page names, the build
				// version kept." (§3.3)
				u := loadUnitDir(t, root, row.dir)
				vocab := ir.TableVocabulary(u)
				out := runMessageJava(t, u, announceReadMain(u, vocab))
				if strings.TrimSpace(out) != "ok" {
					t.Fatalf("%s: %s", row.unit, out)
				}
			})
		}
	})

	t.Run("2.3", func(t *testing.T) {
		t.Parallel()
		u := loadUnitDir(t, root, "tables/backend")
		out := runMessageJava(t, u, refusalsMain(u))
		if strings.TrimSpace(out) != "ok" {
			t.Fatalf("refusals: %s", out)
		}
	})
}

const announceMain = `    public static void main(String[] args) {
        final byte[] buffer = new byte[TableVocabulary.announceMeasure()];
        final int n = TableVocabulary.announce(buffer);
        if (n != buffer.length) { fail("length " + n); }
        System.out.println(hex(buffer));
    }
`

// announceReadMain drives the AnnounceRead clause: it announces, reads the
// announcement back into a resolved vocabulary, and asserts the count, the
// build version, the reference width and every entry id and kind.
func announceReadMain(u *ir.Unit, vocab []ir.TableVocabularyEntry) string {
	var ids, kinds strings.Builder
	for i, e := range vocab {
		if i > 0 {
			ids.WriteString(", ")
			kinds.WriteString(", ")
		}
		fmt.Fprintf(&ids, "0x%016xL", e.Id)
		fmt.Fprintf(&kinds, "%d", e.Kind)
	}
	refBits := ir.TableMessageRefBits(len(vocab))
	return fmt.Sprintf(`    public static void main(String[] args) {
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) { fail("announce"); }
        final TableVocabulary v = new TableVocabulary();
        v.reset(%d);
        final TableVocabulary.Report report = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(v, ann, report)) { fail("announceRead refused"); }
        if (report.malformed || report.refused) { fail("report " + report.reason); }
        if (v.count != %d) { fail("count " + v.count); }
        if (Long.compareUnsigned(v.buildVersion, 0x%016xL) != 0) { fail("version " + Long.toUnsignedString(v.buildVersion, 16)); }
        if (v.refBits != %d) { fail("refBits " + v.refBits); }
        final long[] wantId = {%s};
        final int[] wantKind = {%s};
        if (v.count != wantId.length) { fail("entry count " + v.count); }
        for (int i = 0; i < v.count; i++) {
            if (Long.compareUnsigned(v.entries[i].id, wantId[i]) != 0) {
                fail("id[" + i + "] " + Long.toUnsignedString(v.entries[i].id, 16));
            }
            if (v.entries[i].kind != wantKind[i]) { fail("kind[" + i + "] " + v.entries[i].kind); }
        }
        System.out.println("ok");
    }
`, len(vocab)+1, len(vocab), ir.BuildVersion(u), refBits, ids.String(), kinds.String())
}

// The refusal and strict-check forgeries, built the reference's way: an
// announcement is an ordinary form-1 file whose body carries two fields and
// whose trailer is those two reserved ids (docs/SPEC-TABLES.md §3.3).
func leb(v uint64) []byte {
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
	out := []byte{1, 9}
	return binary.LittleEndian.AppendUint64(out, version)
}

func vocabField(elem uint8, vocab []byte) []byte {
	sub := append([]byte{elem}, leb(uint64(len(vocab)))...)
	sub = append(sub, vocab...)
	field := []byte{2, 14}
	field = append(field, leb(uint64(len(sub)))...)
	return append(field, sub...)
}

func vocabFieldBadKind(kind uint8) []byte {
	sub := append([]byte{6}, leb(0)...)
	field := []byte{2, kind}
	field = append(field, leb(uint64(len(sub)))...)
	return append(field, sub...)
}

func announcement(fields []byte) []byte {
	out := append([]byte{1}, fields...)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// badElemAnnouncement patches the vocabulary field's element kind to 9, walking
// the body the way the reference's framing pre-walk does.
func badElemAnnouncement(ann []byte) []byte {
	bad := append([]byte(nil), ann...)
	off := 1
	for off < len(bad) {
		ref := uint64(0)
		shift := 0
		for {
			b := bad[off]
			off++
			ref |= uint64(b&0x7f) << shift
			shift += 7
			if b&0x80 == 0 {
				break
			}
		}
		if ref == 0 {
			break
		}
		kind := bad[off]
		off++
		if kind == 14 {
			for bad[off]&0x80 != 0 {
				off++
			}
			off++
			bad[off] = 9
			break
		}
		if kind == 9 {
			off += 8
		}
	}
	return bad
}

// refusalsMain drives the clause 2.3 refusals and the two strict checks.
func refusalsMain(u *ir.Unit) string {
	const version = 0x1122334455667788
	twice := append(versionField(version), versionField(version)...)
	twice = append(twice, vocabField(6, nil)...)
	short := append([]byte{1, 9}, 0, 0, 0, 0)
	strict := []struct {
		name string
		data []byte
	}{
		{"build version missing", announcement(vocabField(6, nil))},
		{"build version twice", announcement(twice)},
		{"build version not kind 9", announcement(append([]byte{1, 5, 0, 0, 0, 0, 0, 0, 0, 0}, vocabField(6, nil)...))},
		{"build version fewer than eight bytes", announcement(short)},
		{"vocabulary missing", announcement(versionField(version))},
		{"vocabulary not kind 14", announcement(append(versionField(version), vocabFieldBadKind(13)...))},
		{"vocabulary element not kind 6", badElemAnnouncement(ir.TableAnnouncement(u))},
	}
	var cases strings.Builder
	cases.WriteString("        final String[] strictHex = {\n")
	for _, tc := range strict {
		fmt.Fprintf(&cases, "            \"%s\",\n", hexOf(tc.data))
	}
	cases.WriteString("        };\n")
	return fmt.Sprintf(`    public static void main(String[] args) {
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) { fail("announce"); }

        // "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does not
        // replace or amend anything." and "REFUSAL IS TERMINAL" (§3.3).
        final TableVocabulary first = new TableVocabulary();
        first.reset(%d);
        final TableVocabulary.Report r1 = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(first, ann, r1)) { fail("first announce"); }
        final int before = first.count;
        final TableVocabulary.Report r2 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(first, ann, r2) || !r2.refused || !"second_announcement".equals(r2.reason) || r2.malformed) {
            fail("second announcement was not refused by name");
        }
        if (first.count != before) { fail("a second announcement replaced the vocabulary"); }
        // A refused FIRST announcement is terminal: a peer whose announcement was
        // refused holds no retry on that connection (§3.3).
        final TableVocabulary refused = new TableVocabulary();
        refused.reset(%d);
        final TableVocabulary.Report rr = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(refused, new byte[]{9, 9, 9}, rr) || !rr.refused || !"newer_form".equals(rr.reason) || refused.announced) {
            fail("a refused first announcement was not refused");
        }
        final TableVocabulary.Report later = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(refused, ann, later) || !"second_announcement".equals(later.reason) || refused.announced) {
            fail("an announcement after a refused first is not second_announcement");
        }

        // "vocabulary_too_large covers both bounds, the entry count and the
        // vocabulary's bytes." (§3.3)
        final TableVocabulary smallEntries = new TableVocabulary();
        smallEntries.reset(1);
        final TableVocabulary.Report e1 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(smallEntries, ann, e1) || !e1.refused || !"vocabulary_too_large".equals(e1.reason) || e1.malformed || smallEntries.announced) {
            fail("entry count bound was not refused by name");
        }
        final TableVocabulary smallBytes = new TableVocabulary();
        smallBytes.reset(%d);
        smallBytes.maxBytes = 4;
        final TableVocabulary.Report e2 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(smallBytes, ann, e2) || !e2.refused || !"vocabulary_too_large".equals(e2.reason) || e2.malformed || smallBytes.announced) {
            fail("vocabulary byte bound was not refused by name");
        }

        // "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS ...
        // A FAILED STRICT CHECK IS MALFORMED, not a refusal." (§3.3)
        %s        for (final String h : strictHex) {
            final TableVocabulary v = new TableVocabulary();
            v.reset(%d);
            final TableVocabulary.Report report = new TableVocabulary.Report();
            if (TableVocabulary.announceRead(v, fromHex(h), report) || !report.malformed || report.refused || v.announced) {
                fail("a failed strict check was not malformed");
            }
        }
        // The build version is kept the moment it is read, refusal or not (§3.3).
        final TableVocabulary keep = new TableVocabulary();
        keep.reset(%d);
        final TableVocabulary.Report kr = new TableVocabulary.Report();
        final byte[] keeps = fromHex("%s");
        if (TableVocabulary.announceRead(keep, keeps, kr) || !kr.malformed || Long.compareUnsigned(keep.buildVersion, 0x%016xL) != 0 || keep.announced) {
            fail("the refused announcement did not keep the build version");
        }
        System.out.println("ok");
    }
`, len(ir.TableVocabulary(u))+1, len(ir.TableVocabulary(u))+1, len(ir.TableVocabulary(u))+1, cases.String(), len(ir.TableVocabulary(u))+1, len(ir.TableVocabulary(u))+1, hexOf(announcement(append(versionField(version), 2, 13))), version)
}
