package pythontable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"), the recipe in test/tables/FixedSlack.schema's header. A Python
// counted array is a list of exactly `count` elements, so the rule here is
// that after fixed_load_into A and then B into the SAME targets every list
// holds exactly B's count and no element left over from A, the same as a
// fresh load of B. The leg holds the rule already (fixed_load_into copies
// freshly loaded instances into the targets, runtime/_schema/fixedform.py);
// this test guards it.
func TestPythonFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (they run the generated fixed load over FixedSlack.schema)")
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FixedSlack.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := pythontable.Generate(fixedproof.ParseCheckUnit(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cells 1 nums 1",
		"cell 0 77 subs 1 77",
		"num 77",
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		stdout, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pythonSlackProbe)})
		if err != nil {
			t.Fatalf("%s: %v\n%s%s", pin, err, stdout, stderr)
		}
		out := strings.TrimSpace(string(stdout))
		for _, tag := range []string{"fresh", "reused"} {
			if got := pythonSlackLines(out, tag); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("%s %s: after loading B the value holds\n%s\nwant exactly B's live elements\n%s\nprobe output:\n%s",
					pin, tag, strings.Join(got, "\n"), strings.Join(want, "\n"), out)
			}
		}
		if !strings.Contains(out, "refused 0") {
			t.Fatalf("%s: the truncated file read:\n%s", pin, out)
		}
		newborn := pythonSlackLines(out, "newborn")
		if strings.Join(newborn, "\n") != "cells 0 nums 0\nnum" {
			t.Fatalf("%s: a fresh SlackRoot dumps %q", pin, newborn)
		}
		if got := pythonSlackLines(out, "refused"); strings.Join(got, "\n") != strings.Join(newborn, "\n") {
			t.Errorf("%s: a refused load left %q, want a fresh value %q\nprobe output:\n%s", pin, got, newborn, out)
		}
	}
}

func pythonSlackLines(out, tag string) []string {
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, tag+" "); ok && rest != "0" {
			got = append(got, rest)
		}
	}
	return got
}

const pythonSlackProbe = pyrun.ImportPrelude + `
from tblslack import SlackRoot, SlackCell, SlackSub


def sub(v):
    s = SlackSub()
    s.v = v
    return s


def cell(n):
    c = SlackCell()
    c.id = 77
    c.subs = [sub(77) for _ in range(n)]
    return c


def dump(tag, r):
    print(tag, 'cells', len(r.cells), 'nums', len(r.nums))
    for i, c in enumerate(r.cells):
        print(tag, 'cell', i, c.id, 'subs', len(c.subs), *[s.v for s in c.subs])
    print(tag, 'num', *r.nums)


a = SlackRoot()
a.cells = [cell(2) for _ in range(3)]
a.nums = [77] * 4
b = SlackRoot()
b.cells = [cell(1)]
b.nums = [77]
A = SlackRoot.fixed_save([a])
B = SlackRoot.fixed_save([b])
dump('fresh', SlackRoot.fixed_load(B)[0])
targets = [SlackRoot()]
SlackRoot.fixed_load_into(targets, A)
SlackRoot.fixed_load_into(targets, B)
dump('reused', targets[0])
SlackRoot.fixed_load_into(targets, A)
print('refused', SlackRoot.fixed_load_into(targets, B[:-1]))
dump('refused', targets[0])
dump('newborn', SlackRoot())
`
