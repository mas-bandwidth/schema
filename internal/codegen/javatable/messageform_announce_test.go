package javatable

// messageform_announce — the Java leg's MESSAGE FORM announcement surface
// (docs/SPEC-TABLES.md §3.3, form byte 2), which the leg carries none of today.
// One subtest per clause of the card:
//
//   2.1 Announce and AnnounceMeasure are a compile-time constant byte array and
//       its length, equal byte for byte to the manifest's announcement file for
//       the units backenddemo, vocabdemo and vocab9demo.
//
// The rows are the reference's own bytes, pinned by
// testdata/conformance/tables/MANIFEST.txt's `connection` lines.

import (
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

// TestMessageFormAnnounceJava pins the announcement surface (docs/SPEC-TABLES.md
// §3.3). Each subtest compiles the generated TableVocabulary and a driver, runs
// it, and compares the announced bytes to the reference file.
func TestMessageFormAnnounceJava(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)

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
				files, err := Generate(u)
				if err != nil {
					t.Fatalf("generate %s: %v", row.unit, err)
				}
				src, ok := files[messageAnnouncementClass+".java"]
				if !ok {
					t.Fatalf("%s: no %s.java emitted", row.unit, messageAnnouncementClass)
				}
				want, err := os.ReadFile(filepath.Join(root, row.golden))
				if err != nil {
					t.Fatal(err)
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
				driver := "package " + u.Package + ";\n\n" + announceDriver
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
				if got := strings.TrimSpace(string(out)); got != hexOf(want) {
					t.Fatalf("%s: announce = %s, want %s", row.unit, got, hexOf(want))
				}
			})
		}
	})
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	var s strings.Builder
	for _, v := range b {
		s.WriteByte(digits[v>>4])
		s.WriteByte(digits[v&0xf])
	}
	return s.String()
}

const announceDriver = `public final class Driver {
    private Driver() {}

    public static void main(String[] args) {
        final byte[] buffer = new byte[TableVocabulary.announceMeasure()];
        final int n = TableVocabulary.announce(buffer);
        if (n != buffer.length) {
            System.out.println("length");
            System.exit(1);
        }
        final StringBuilder out = new StringBuilder();
        for (final byte b : buffer) {
            out.append(String.format("%02x", b));
        }
        System.out.println(out);
    }
}
`
