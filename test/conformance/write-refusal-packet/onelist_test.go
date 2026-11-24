package wrpacket

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// root is the repository root seen from this package's directory.
const root = "../../.."

// compiledDriver is one assert leg's driver: it reads rows.txt and keys a
// setter table by row name. key matches one table entry and captures its name;
// the table sits between the two marker lines. owed is the leg's exact ledger of
// rows it names without a setter, each with its reason; the driver must carry
// the same names in its own not-representable list (ledger, a regexp
// capturing the list's quoted names), and an empty owed needs no ledger.
type compiledDriver struct {
	leg, path string
	key       *regexp.Regexp
	owed      map[string]string
	ledger    *regexp.Regexp
}

var compiledDrivers = []compiledDriver{
	{leg: "c", path: "test/packet-write-refusal/c/driver.c", key: regexp.MustCompile(`\{ "([a-z0-9_]+)", set_`)},
	{leg: "cpp", path: "test/packet-write-refusal/cpp/main.cpp", key: regexp.MustCompile(`m\["([a-z0-9_]+)"\] = `)},
	{leg: "cs", path: "test/packet-write-refusal/cs/Program.cs", key: regexp.MustCompile(`\["([a-z0-9_]+)"\] = \(ref `)},
	{leg: "unity", path: "test/packet-write-refusal/unity/Program.cs", key: regexp.MustCompile(`\{ "([a-z0-9_]+)", \(ref `)},
	{leg: "java", path: "test/packet-write-refusal/java/Main.java", key: regexp.MustCompile(`SETTERS\.put\("([a-z0-9_]+)", `)},
	{leg: "dart", path: "test/packet-write-refusal/dart/main.dart", key: regexp.MustCompile(`(?m)^\s*'([a-z0-9_]+)': \(v\)`)},
	{
		leg: "rust", path: "test/packet-write-refusal/rust/src/main.rs",
		key: regexp.MustCompile(`\("([a-z0-9_]+)", "[a-z_]+", \|v\|`),
		owed: map[string]string{
			"union_tag_high": "Shot is a native Rust enum: a tag past its arms cannot be built",
		},
		ledger: regexp.MustCompile(`const NOT_REPRESENTABLE: &\[&str\] = &\[([^\]]*)\];`),
	},
}

const (
	tableBegin = "write-refusal-packet setters: begin"
	tableEnd   = "write-refusal-packet setters: end"
)

var quoted = regexp.MustCompile(`"([a-z0-9_]+)"`)

func names(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

// TestCompiledDriversKeyTheOneList holds every compiled assert leg's setter
// table to rows.txt: the table's names plus the leg's owed ledger are exactly
// the rows, each once, so a driver that drops a row, keeps a row of its own or
// grows a second copy of the list goes red here before any toolchain runs.
func TestCompiledDriversKeyTheOneList(t *testing.T) {
	rows := Rows(t, root)
	want := names(rows)
	for _, d := range compiledDrivers {
		t.Run(d.leg, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, d.path))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Count(text, tableBegin) != 1 || strings.Count(text, tableEnd) != 1 {
				t.Fatalf("FAILED: %s: the setter table must sit between one %q line and one %q line", d.path, tableBegin, tableEnd)
			}
			table := text[strings.Index(text, tableBegin):strings.Index(text, tableEnd)]
			var got []string
			seen := map[string]bool{}
			for _, m := range d.key.FindAllStringSubmatch(table, -1) {
				if seen[m[1]] {
					t.Errorf("FAILED: %s: setter %s appears twice", d.leg, m[1])
				}
				seen[m[1]] = true
				got = append(got, m[1])
			}
			var ledger []string
			if d.ledger != nil {
				m := d.ledger.FindStringSubmatch(text)
				if m == nil {
					t.Fatalf("FAILED: %s: no not-representable list matching %s", d.path, d.ledger)
				}
				for _, q := range quoted.FindAllStringSubmatch(m[1], -1) {
					ledger = append(ledger, q[1])
				}
			}
			var owed []string
			for name := range d.owed {
				owed = append(owed, name)
			}
			sort.Strings(owed)
			sort.Strings(ledger)
			if !slices.Equal(owed, ledger) {
				t.Errorf("FAILED: %s: the driver's not-representable list %v is not the owed ledger %v", d.leg, ledger, owed)
			}
			for _, name := range owed {
				if seen[name] {
					t.Errorf("FAILED: %s: %s is owed (%s) and also has a setter; drop the owed line", d.leg, name, d.owed[name])
				}
			}
			all := append(slices.Clone(got), owed...)
			sort.Strings(all)
			if !slices.Equal(all, want) {
				t.Errorf("FAILED: %s: %d setters + %d owed = %d names, rows.txt holds %d rows\n missing: %v\n extra:   %v",
					d.leg, len(got), len(owed), len(all), len(want), minus(want, all), minus(all, want))
			}
			t.Logf("%s: %d setters, %d owed, rows.txt %d", d.leg, len(got), len(owed), len(want))
		})
	}
}

func minus(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}

// listThreshold is how many distinct row names one file may mention before it
// counts as a case list of its own: a negative control names the few rows it
// expects red, a list names most of them.
const listThreshold = 8

// TestNoSecondRowList walks the code, tests and make files for a second copy
// of the write-refusal-packet list: a file other than rows.txt and the
// registered drivers that names more than listThreshold rows, a second
// RefusalPacket.schema or PacketWriteRefusal declaration, or a reference to
// the retired test/packet-write-refusal/ list or schema.
func TestNoSecondRowList(t *testing.T) {
	rows := Rows(t, root)
	word := regexp.MustCompile(`\b(` + strings.Join(names(rows), "|") + `)\b`)
	allowed := map[string]bool{filepath.Join(Dir, "rows.txt"): true}
	for _, d := range compiledDrivers {
		allowed[d.path] = true
	}
	schema := filepath.Join(Dir, "RefusalPacket.schema")
	retired := []string{"packet-write-refusal/" + "cases.txt", "packet-write-refusal/" + "RefusalPacket.schema"}
	declaration := regexp.MustCompile(`(?m)^\s*type\s+PacketWriteRefusal\s*$`)
	scanned := 0
	for _, top := range []string{"test", "make", "internal", "tools", "compiler", "cmd", ".github", "Makefile"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if e.Name() == "node_modules" || e.Name() == "target" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, "_test.go") && filepath.Dir(rel) == Dir {
				return nil
			}
			if e.Name() == "RefusalPacket.schema" && rel != schema {
				t.Errorf("FAILED: %s: a second RefusalPacket.schema; the one schema is %s", rel, schema)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			text := string(data)
			for _, r := range retired {
				if strings.Contains(text, r) {
					t.Errorf("FAILED: %s: names the retired %s; the one list is %s/rows.txt", rel, r, Dir)
				}
			}
			if rel != schema && strings.Contains(text, "PacketWriteRefusal") && declaration.MatchString(text) {
				t.Errorf("FAILED: %s: declares PacketWriteRefusal; generate from %s instead", rel, schema)
			}
			// every list of this battery carries the lawful twins (the *_at_max
			// rows), so a file without that fragment is not one; the check
			// keeps the regexp off the bulk of the tree.
			if allowed[rel] || !strings.Contains(text, "_at_max") {
				return nil
			}
			distinct := map[string]bool{}
			for _, m := range word.FindAllString(text, -1) {
				distinct[m] = true
			}
			if len(distinct) > listThreshold {
				t.Errorf("FAILED: %s: names %d of the %d rows, a case list of its own; read %s/rows.txt instead", rel, len(distinct), len(rows), Dir)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if scanned == 0 {
		t.Fatal("FAILED: scanned no files")
	}
}

// TestCompiledLegsReadTheOneList holds each compiled leg's make recipe to the
// one list: it generates the shared schema and runs its driver over rows.txt.
func TestCompiledLegsReadTheOneList(t *testing.T) {
	recipes := map[string]string{
		"c": "make/c.mk", "cpp": "make/cpp-packet-write-refusal.mk", "cs": "make/cs-packet-write-refusal.mk",
		"unity": "make/unity.mk", "java": "make/java-packet-write-refusal.mk", "dart": "make/dart.mk", "rust": "make/rust.mk",
	}
	if len(recipes) != len(compiledDrivers) {
		t.Fatalf("FAILED: %d recipes for %d compiled drivers", len(recipes), len(compiledDrivers))
	}
	for _, d := range compiledDrivers {
		path, ok := recipes[d.leg]
		if !ok {
			t.Errorf("FAILED: %s: no make recipe registered", d.leg)
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, need := range []string{filepath.Join(Dir, "rows.txt"), filepath.Join(Dir, "RefusalPacket.schema")} {
			if !strings.Contains(text, need) {
				t.Errorf("FAILED: %s: %s does not use %s", d.leg, path, need)
			}
		}
	}
}
