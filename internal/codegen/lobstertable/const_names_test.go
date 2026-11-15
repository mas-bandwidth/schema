package lobstertable

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/scanner"
)

var (
	tableGeneratedDefLine  = regexp.MustCompile(`(?m)^[ \t]*def[ \t]+[A-Za-z_][A-Za-z0-9_]*\(([^)]*)\)`)
	tableGeneratedDeclLine = regexp.MustCompile(`(?m)^[ \t]+(?:let|var)[ \t]+([^=\n]+)`)
	tableGeneratedForLine  = regexp.MustCompile(`(?m)^[ \t]*for\([^)]*\)[ \t]*([^:\n]+):`)
)

// TestLobsterTableConstNamesNeverShadowGeneratedLocals declares a schema const
// for every parameter and local name the table emitter writes, beside a table
// that uses every field kind, and requires the pinned Lobster to compile and
// run the module. A generated parameter or local a schema const can spell
// shadows it.
func TestLobsterTableConstNamesNeverShadowGeneratedLocals(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "examples", "Tables.schema"))
	if err != nil {
		t.Fatal(err)
	}
	baseline := t.TempDir()
	tableWriteFile(t, baseline, "Tables.schema", string(src))
	u := lobsterUnitOf(t, string(src))
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, mod := range []string{"Probe.lobster", "ProbeTable.lobster"} {
		names = append(names, tableGeneratedLocals(t, string(files[mod])+string(tables[mod]))...)
	}
	names = uniqueNames(names)
	plain, underscore := splitTableConstNames(names)
	for i, set := range [][]string{plain, underscore} {
		if i == 1 && len(set) == 0 {
			continue
		}
		su := lobsterUnitOf(t, insertTableConsts(string(src), set))
		shadow, err := lobster.Generate(su)
		if err != nil {
			t.Fatalf("consts %v: %v", set, err)
		}
		shadowTables, err := Generate(su)
		if err != nil {
			t.Fatalf("consts %v: %v", set, err)
		}
		for name, data := range shadowTables {
			shadow[name] = data
		}
		dir := t.TempDir()
		tableWriteAll(t, dir, shadow)
		tableWriteFile(t, dir, "main.lobster", tableConstNamesProbe)
		if out := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil))); out != "ok" {
			t.Fatalf("consts %v printed %q", set, out)
		}
	}
}

func tableWriteFile(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tableWriteAll(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func uniqueNames(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func tableGeneratedLocals(t *testing.T, src string) []string {
	t.Helper()
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	for _, m := range tableGeneratedDefLine.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) > 0 {
				name, _, _ := strings.Cut(fields[0], ":")
				add(name)
			}
		}
	}
	for _, m := range tableGeneratedDeclLine.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) > 0 {
				name, _, _ := strings.Cut(fields[0], ":")
				add(name)
			}
		}
	}
	for _, m := range tableGeneratedForLine.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) > 0 {
				add(fields[0])
			}
		}
	}
	return names
}

func splitTableConstNames(names []string) (plain, underscore []string) {
	for _, name := range names {
		if !tableSchemaCanDeclare(name) {
			continue
		}
		if strings.HasPrefix(name, "_") {
			underscore = append(underscore, name)
		} else {
			plain = append(plain, name)
		}
	}
	sort.Strings(plain)
	sort.Strings(underscore)
	return plain, underscore
}

func tableSchemaCanDeclare(name string) bool {
	toks, errs := scanner.Scan("generated", []byte(name))
	if len(errs) != 0 || len(toks) != 2 || toks[0].Kind != scanner.Ident || toks[0].Text != name || toks[1].Kind != scanner.EOF {
		return false
	}
	_, reserved := check.ReservedWords()[name]
	return !reserved
}

func insertTableConsts(src string, names []string) string {
	if len(names) == 0 {
		return src
	}
	var b strings.Builder
	inserted := false
	for _, line := range strings.SplitAfter(src, "\n") {
		b.WriteString(line)
		if !inserted && strings.HasPrefix(strings.TrimSpace(line), "package ") {
			b.WriteString("\n")
			for _, name := range names {
				b.WriteString("const " + name + " = 0\n")
			}
			b.WriteString("\n")
			inserted = true
		}
	}
	return b.String()
}

const tableConstNamesProbe = `import .ProbeTable

let v = tabledemo.NewRootConfig()
let wire = tabledemo.RootConfigFixedSave([v])
let back = tabledemo.RootConfigFixedDecode(wire)
assert length(back) == 1
print("ok")
`
