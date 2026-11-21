package ci

import (
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"testing"
)

// EVERY COMPILER BINARY MAKE BUILDS REBUILDS ON WHAT IT EMBEDS. A make target
// whose recipe is `go build ... -o $@ ./cmd/schema` is a schema compiler, and
// the runtimes it emits verbatim are go:embed files, not .go files: a target
// that names only $(GO_SOURCES) stays up to date after a runtime edit, and
// every conformance tree generated from it keeps the old runtime
// (build/schema-squirrel and internal/codegen/squirrel/serialize/*.nut,
// 2026-11-21). Each such target must take every file `go list` says the
// compiler embeds, and every SERIALIZE_PIN beside a runtime it embeds (the
// vendored serializer's pin, which a bump changes with the files): in the
// Makefile, $(EMBED_SOURCES). bin-schema-embeds holds bin/schema the same way
// by touching files; this reads make's own printed database.

// compilerTargets are the non-phony targets of make's printed database whose
// recipe builds ./cmd/schema into $@, with each one's prerequisites.
func compilerTargets(db string) map[string][]string {
	out := map[string][]string{}
	phony := map[string]bool{}
	target, prereqs := "", []string(nil)
	for line := range strings.SplitSeq(db, "\n") {
		if rest, ok := strings.CutPrefix(line, ".PHONY:"); ok {
			for p := range strings.FieldsSeq(rest) {
				phony[p] = true
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			target = ""
			name, rest, ok := strings.Cut(line, ":")
			if ok && !strings.HasPrefix(rest, "=") && !strings.HasPrefix(rest, ":") && !strings.Contains(name, " ") && !strings.Contains(name, "=") {
				target, prereqs = name, strings.Fields(rest)
			}
			continue
		}
		if target != "" && strings.Contains(line, "go build") && strings.Contains(line, "-o $@") && strings.Contains(line, "./cmd/schema") {
			out[target] = prereqs
		}
	}
	for t := range out {
		if phony[t] {
			delete(out, t)
		}
	}
	return out
}

// compilerEmbedErrors names every compiler target that does not rebuild on an
// embedded file.
func compilerEmbedErrors(targets map[string][]string, embeds []string) []string {
	var names []string
	for t := range targets {
		names = append(names, t)
	}
	sort.Strings(names)
	var errs []string
	for _, t := range names {
		var missing []string
		for _, f := range embeds {
			if !slices.Contains(targets[t], f) {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			shown := strings.Join(missing[:min(len(missing), 5)], " ")
			if len(missing) > 5 {
				shown += " ..."
			}
			errs = append(errs, t+" builds ./cmd/schema and does not rebuild when what it embeds changes (add $(EMBED_SOURCES) to its prerequisites): "+shown)
		}
	}
	return errs
}

func TestEveryCompilerTargetRebuildsOnWhatItEmbeds(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	list := exec.Command("go", "list", "-deps", "-f", `{{$d := .Dir}}{{range .EmbedFiles}}{{$d}}/{{.}}{{"\n"}}{{end}}`, "./cmd/schema")
	list.Dir = root
	out, err := list.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var embeds []string
	for f := range strings.FieldsSeq(string(out)) {
		rel := strings.TrimPrefix(f, root+"/")
		// a .go file go:embedded as data is already in $(GO_SOURCES)
		if !strings.HasSuffix(rel, ".go") {
			embeds = append(embeds, rel)
		}
		dir := rel[:strings.LastIndex(rel, "/")]
		for ; strings.Contains(dir, "/runtime"); dir = dir[:strings.LastIndex(dir, "/")] {
			if strings.HasSuffix(dir, "/runtime") {
				pin := dir + "/SERIALIZE_PIN"
				if _, err := os.Stat(root + "/" + pin); err == nil && !slices.Contains(embeds, pin) {
					embeds = append(embeds, pin)
				}
				break
			}
		}
	}
	if len(embeds) < 20 || !slices.Contains(embeds, "internal/codegen/squirrel/serialize/serialize.nut") {
		t.Fatalf("go list named %d embedded files, without the squirrel serializer: this gate would pass over an empty set", len(embeds))
	}
	mk := exec.Command("make", "-pq", "--no-print-directory", "build/schema-squirrel")
	mk.Dir = root
	db, _ := mk.Output() // -q exits non-zero when out of date; the database is printed either way
	targets := compilerTargets(string(db))
	for _, want := range []string{"bin/schema", "build/schema-squirrel", "build/schema-python", "build/schema-ruby", "build/schema-moo"} {
		if _, ok := targets[want]; !ok {
			t.Errorf("make's database names no compiler target %s: the reader has drifted from the recipes", want)
		}
	}
	for _, e := range compilerEmbedErrors(targets, embeds) {
		t.Error(e)
	}
	// the control: the squirrel serializer dropped from build/schema-squirrel
	planted := map[string][]string{}
	for k, v := range targets {
		planted[k] = slices.DeleteFunc(slices.Clone(v), func(p string) bool {
			return k == "build/schema-squirrel" && strings.HasPrefix(p, "internal/codegen/squirrel/serialize/")
		})
	}
	if errs := compilerEmbedErrors(planted, embeds); !strings.Contains(strings.Join(errs, "\n"), "build/schema-squirrel") {
		t.Error("NEGATIVE CONTROL FAILED: build/schema-squirrel without its serializer left the gate green")
	}
}
