package darttable

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// wireFuzzPlainTableMessages are the form-2 roster entries whose root is a
// plain `table`: docs/SPEC-TABLES.md §3.3 says "a plain `table T` may not"
// ride the message form, so no leg carries message verbs for them and the
// harness counts their seeds absent on every leg, the Go reference's included.
var wireFuzzPlainTableMessages = map[string]bool{
	"graphdemo Scene":  true,
	"blobdemo Catalog": true,
}

// TestWireFuzzAllTargetDart pins the Dart leg's wire fuzzer over EVERY ROOT
// (docs/SPEC-TABLES.md §4.2). make/dart.mk's tables-dart-wire-fuzz-all runs the
// harness with no --manifest, so it reads the default manifest and its pinned
// vectors as the Makefile's tables-wire-fuzz does, holds the three greps
// cards-dart-wire-fuzz holds, and rides test-dart. And because a root the
// driver cannot open is only counted absent by the harness, the test holds
// test/conformance/dart/main.dart to binding every form-1 root the manifest
// and the vectors name, and every form-2 root but the §3.3 plain tables.
func TestWireFuzzAllTargetDart(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	mk := read("make/dart.mk")
	recipe, ok := makeRecipe(mk, "tables-dart-wire-fuzz-all")
	if !ok {
		t.Fatal("make/dart.mk has no tables-dart-wire-fuzz-all target")
	}
	for _, want := range []string{
		"./build/conformance-harness wire-fuzz",
		"--driver 'build/conformance-dart wire-fuzz'",
		"--seed ",
		"--n ",
		"@grep -q '0 divergences'",
		"@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots'",
		"@grep -qE '[1-9][0-9]* mutants'",
	} {
		if !strings.Contains(recipe, want) {
			t.Errorf("tables-dart-wire-fuzz-all does not hold %q:\n%s", want, recipe)
		}
	}
	for _, refuse := range []string{"--manifest", "--vectors", "--retain", "--replay"} {
		if strings.Contains(recipe, refuse) {
			t.Errorf("tables-dart-wire-fuzz-all names %s, so it does not fuzz every root of the default manifest:\n%s", refuse, recipe)
		}
	}
	if !prerequisiteOf(mk, "test-dart", "tables-dart-wire-fuzz-all") {
		t.Error("test-dart does not run tables-dart-wire-fuzz-all")
	}

	driver := read("test/conformance/dart/main.dart")
	files, messages := wireFuzzRoster(t, read("testdata/conformance/tables/MANIFEST.txt"), read("testdata/wire/tables/fuzz-vectors/INDEX.txt"))
	if len(files) == 0 || len(messages) == 0 {
		t.Fatalf("the default manifest names %d form-1 and %d form-2 roots; the roster parse is wrong", len(files), len(messages))
	}
	for _, key := range files {
		if key == "cards Card" {
			continue // wireFuzz's own codec, with the retention arm
		}
		if !strings.Contains(driver, "'"+key+"': _fuzzFile(") {
			t.Errorf("main.dart binds no wire-fuzz codec for the form-1 root %s: the gate would count its seeds absent", key)
		}
	}
	for _, key := range messages {
		bound := strings.Contains(driver, "'"+key+"': _messageRow<")
		if wireFuzzPlainTableMessages[key] {
			if bound {
				t.Errorf("main.dart binds message verbs for %s, a plain table (§3.3): drop it from wireFuzzPlainTableMessages", key)
			}
			continue
		}
		if !bound {
			t.Errorf("main.dart binds no message codec for the form-2 root %s: the gate would count its seeds absent", key)
		}
	}
}

// makeRecipe returns the recipe lines of target in a make file: the rule line
// and every tab-led line after it.
func makeRecipe(mk, target string) (string, bool) {
	lines := strings.Split(mk, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, target+":") {
			continue
		}
		var b strings.Builder
		b.WriteString(line + "\n")
		for _, next := range lines[i+1:] {
			if !strings.HasPrefix(next, "\t") {
				break
			}
			b.WriteString(next + "\n")
		}
		return b.String(), true
	}
	return "", false
}

// prerequisiteOf reports whether any `target:` rule line names prereq.
func prerequisiteOf(mk, target, prereq string) bool {
	for _, line := range strings.Split(mk, "\n") {
		if !strings.HasPrefix(line, target+":") {
			continue
		}
		for _, f := range strings.Fields(strings.TrimPrefix(line, target+":")) {
			if f == prereq {
				return true
			}
		}
	}
	return false
}

// wireFuzzRoster is the harness's roster for a run with no --manifest
// (test/conformance/harness/wirefuzz.go): the instance and report rows and the
// non-message vectors are form-1 roots; the message rows, under their
// connection's unit, and the message vectors are form-2 roots.
func wireFuzzRoster(t *testing.T, manifest, vectors string) (files, messages []string) {
	t.Helper()
	fileSet, messageSet := map[string]bool{}, map[string]bool{}
	connections := map[string]string{}
	ws := regexp.MustCompile(`[ \t]+`)
	rows := func(text string) [][]string {
		var out [][]string
		for _, raw := range strings.Split(text, "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			out = append(out, ws.Split(line, -1))
		}
		return out
	}
	m := rows(manifest)
	for _, f := range m {
		if f[0] == "connection" && len(f) >= 3 {
			connections[f[1]] = f[2]
		}
	}
	for _, f := range m {
		switch f[0] {
		case "instance", "report":
			if len(f) < 4 {
				t.Fatalf("manifest row %v is short", f)
			}
			fileSet[f[2]+" "+f[3]] = true
		case "message":
			if len(f) < 4 {
				t.Fatalf("manifest message row %v is short", f)
			}
			unit, ok := connections[f[2]]
			if !ok {
				t.Fatalf("manifest message row %v names no connection", f)
			}
			messageSet[unit+" "+f[3]] = true
		}
	}
	for _, f := range rows(vectors) {
		if len(f) < 4 {
			t.Fatalf("vector row %v is short", f)
		}
		if len(f) > 4 && f[4] == "message" {
			messageSet[f[1]+" "+f[2]] = true
		} else {
			fileSet[f[1]+" "+f[2]] = true
		}
	}
	for k := range fileSet {
		files = append(files, k)
	}
	for k := range messageSet {
		messages = append(messages, k)
	}
	sort.Strings(files)
	sort.Strings(messages)
	return files, messages
}
