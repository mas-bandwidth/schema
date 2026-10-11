package rusttable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormAnnounceRust holds the MESSAGE FORM's announcement surface
// (form byte 2, docs/SPEC-TABLES.md §3.3) to the reference: the units
// backenddemo, vocabdemo and vocab9demo announce the manifest's own bytes.
func TestMessageFormAnnounceRust(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	conns := manifestConnections(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))

	t.Run("AnnounceIsTheReferenceBytes", func(t *testing.T) {
		t.Parallel()
		for _, key := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
			conn, ok := conns[key]
			if !ok {
				t.Fatalf("manifest names no connection for unit %q", key)
			}
			u := loadUnitDir(t, root, units[key])
			out, err := Generate(u)
			if err != nil {
				t.Fatalf("%s: generate: %v", key, err)
			}
			src, ok := out[AnnouncementModule+".rs"]
			if !ok {
				t.Fatalf("%s emitted no %s.rs; got %v", key, AnnouncementModule, keysOf(out))
			}
			want, err := os.ReadFile(filepath.Join(root, conn))
			if err != nil {
				t.Fatal(err)
			}
			got, length := announcementConstant(t, string(src))
			if string(got) != string(want) {
				t.Errorf("%s: the announcement is not the reference's bytes:\n got %x\nwant %x", key, got, want)
			}
			if length != len(want) {
				t.Errorf("%s: announce_measure length %d != reference %d", key, length, len(want))
			}
		}
	})

	t.Run("AnnounceReadFillsVocabulary", func(t *testing.T) {
		t.Parallel()
		u := loadUnitDir(t, root, units["backenddemo"])
		out, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		src := string(out[AnnouncementModule+".rs"])
		for _, want := range []string{
			"pub struct TableVocabulary",
			"pub fn announce_read(",
			"BuildVersion",
		} {
			if !strings.Contains(src, want) {
				t.Errorf("the announcement surface is missing %q (docs/SPEC-TABLES.md §3.3)", want)
			}
		}
	})

	t.Run("AnnounceReadRefusals", func(t *testing.T) {
		t.Parallel()
		u := loadUnitDir(t, root, units["backenddemo"])
		out, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		src := string(out[AnnouncementModule+".rs"])
		for _, want := range []string{
			"second_announcement",
			"vocabulary_too_large",
			"malformed",
		} {
			if !strings.Contains(src, want) {
				t.Errorf("the announcement reader does not name %q (docs/SPEC-TABLES.md §3.3)", want)
			}
		}
	})
}

// announcementConstant parses the emitted TABLE_ANNOUNCEMENT array and its
// length out of the module source.
func announcementConstant(t *testing.T, src string) ([]byte, int) {
	t.Helper()
	const marker = "TABLE_ANNOUNCEMENT: [u8; "
	i := strings.Index(src, marker)
	if i < 0 {
		t.Fatalf("no TABLE_ANNOUNCEMENT constant in the module")
	}
	rest := src[i+len(marker):]
	close := strings.Index(rest, "]")
	n, err := strconv.Atoi(rest[:close])
	if err != nil {
		t.Fatalf("TABLE_ANNOUNCEMENT length: %v", err)
	}
	open := strings.Index(rest, "[")
	end := strings.Index(rest[open:], "];")
	if open < 0 || end < 0 {
		t.Fatalf("TABLE_ANNOUNCEMENT array is not closed")
	}
	body := rest[open+1 : open+end]
	out := make([]byte, 0, n)
	for _, f := range strings.Split(body, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("byte literal %q: %v", f, err)
		}
		out = append(out, byte(v))
	}
	if len(out) != n {
		t.Fatalf("TABLE_ANNOUNCEMENT carries %d bytes, not the declared %d", len(out), n)
	}
	return out, n
}

// manifestConnections maps a unit key to its announcement wire file.
func manifestConnections(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, fields := range manifestFields(t, path) {
		if fields[0] == "connection" && len(fields) >= 5 {
			out[fields[2]] = fields[4]
		}
	}
	return out
}

// manifestUnits maps a unit key to the schema paths that declare it.
func manifestUnits(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, fields := range manifestFields(t, path) {
		if fields[0] == "unit" && len(fields) >= 3 {
			out[fields[1]] = fields[2]
		}
	}
	return out
}

func manifestFields(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		rows = append(rows, fields)
	}
	return rows
}

// loadUnitDir parses every *.schema file directly under a manifest unit path.
func loadUnitDir(t *testing.T, root, rel string) *ir.Unit {
	t.Helper()
	dir := filepath.Join(root, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		name := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(name, data)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: name, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"),
			Bytes: data, AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", rel, cerrs[0])
	}
	return u
}
