package ctable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCSkippedValueIsValidated runs every row of the shared skipped-value
// vectors through the C leg's reads (docs/SPEC-TABLES.md §4, §16.3;
// docs/rulings.json skipped-value-is-validated): the JSON rows through the
// reader root's text read, and the message rows through its batch load under
// the newer writer unit's announcement. A value the reader SKIPS is held to
// what a read of it would hold, and damage inside it refuses the whole read.
// The verdict is the contract; a refusal's name is the leg's own diagnostic.
func TestCSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "cc (it compiles and runs the generated skipped-value probe)")

	root := filepath.Join("..", "..", "..")
	dir := filepath.Join(root, "testdata", "wire", "skipped-value")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct{ file, form, verdict string }
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifest, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifest, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifest, i+1, f[2])
		}
		rows = append(rows, row{f[0], f[1], f[2]})
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no rows", manifest)
	}

	schemaBytes, err := os.ReadFile(filepath.Join(root, "test", "skipped-value", "reader", "Reader.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := cUnitOf(t, string(schemaBytes))
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)

	annIdx := -1
	for i, r := range rows {
		if r.form == "message" && r.file == "announcement.bin" {
			annIdx = i
		}
	}
	if annIdx < 0 {
		t.Fatalf("%s names no announcement.bin message row", manifest)
	}

	var src strings.Builder
	src.WriteString("#include <stdint.h>\n#include <stdio.h>\n#include <string.h>\n#include \"ProbeTable.h\"\n")
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		fmt.Fprintf(&src, "static const uint8_t row_%d[] = {%s};\n", i, cMessageBytes(data))
	}
	src.WriteString("int main(void)\n{\n")
	src.WriteString("\tTableMessageEntry entries[8192];\n\tTableVocabulary v = table_vocabulary(entries, 8192);\n\tTableReport arep; memset(&arep, 0, sizeof(arep));\n")
	fmt.Fprintf(&src, "\tint announced = announce_read(&v, row_%d, (int64_t)sizeof(row_%d), &arep);\n", annIdx, annIdx)
	for i, r := range rows {
		switch r.form {
		case "json":
			src.WriteString("\t{\n\t\tRoot value; TableReport rep; int ok;\n\t\tmemset(&rep, 0, sizeof(rep)); root_reset(&value);\n")
			fmt.Fprintf(&src, "\t\tok = root_from_json(&value, (const char *)row_%d, (int64_t)sizeof(row_%d), &rep);\n", i, i)
			fmt.Fprintf(&src, "\t\tprintf(\"%d %%s\\n\", ok ? \"read\" : \"refused\");\n\t}\n", i)
		case "message":
			if i == annIdx {
				fmt.Fprintf(&src, "\tprintf(\"%d %%s\\n\", announced ? \"read\" : \"refused\");\n", i)
				continue
			}
			src.WriteString("\t{\n\t\tRoot storage; int64_t count = 1; TableReport rep; int ok;\n\t\tmemset(&rep, 0, sizeof(rep)); root_reset(&storage);\n")
			fmt.Fprintf(&src, "\t\tok = root_load_messages(&storage, &count, &v, row_%d, (int64_t)sizeof(row_%d), &rep);\n", i, i)
			fmt.Fprintf(&src, "\t\tprintf(\"%d %%s\\n\", ok && !rep.refused ? \"read\" : \"refused\");\n\t}\n", i)
		}
	}
	src.WriteString("\treturn 0;\n}\n")

	cc, err := exec.LookPath("cc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and cc is not on PATH: %v", err)
		}
		t.Skipf("cc is not on PATH: %v", err)
	}
	build := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(build, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(build, "skippedvalue.c")
	if err := os.WriteFile(main, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(build, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", build, main}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(build, name))
		}
	}
	args = append(args, "-lm", "-o", binary)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	got := map[int]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		var index int
		var verdict string
		if _, err := fmt.Sscanf(line, "%d %s", &index, &verdict); err != nil {
			t.Fatalf("the probe answered %q: %v", line, err)
		}
		got[index] = verdict
	}
	if len(got) != len(rows) {
		t.Fatalf("the probe answered %d rows of %d:\n%s", len(got), len(rows), out)
	}
	for i, r := range rows {
		if got[i] != r.verdict {
			t.Errorf("%s (%s): verdict %s, want %s", r.file, r.form, got[i], r.verdict)
		}
	}
}
