package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// fixedRootSaveLoop replaces the <Root>FixedSave loop of fixedSaveRefusalProbe:
// the same rows go through <Root>Save, the whole-file save a fixed root also
// answers (runtime/save.nut, saveFixed). A refusal is a null answer; a lawful
// twin's file must read back through <Root>FixedLoad unchanged.
const fixedRootSaveLoop = `foreach (row in rows) {
  local verdict = "";
  try {
    local r = T.Rec();
    row[2](r);
    local file = T.RecSave(r);
    if (file == null) {
      verdict = "refused";
    } else if (!row[1]) {
      verdict = "wrote";
    } else {
      local back = T.Rec();
      local got = T.RecFixedLoad(file, [back], null);
      if (got != 1) verdict = "unreadable:" + T.FixedReason;
      else if (!same(row[3](r), row[3](back))) verdict = "changed";
      else verdict = "wrote";
    }
  } catch (e) {
    verdict = "raised:" + e;
  }
  print(row[0] + "=" + verdict + "\n");
}
`

// TestSquirrelFixedRootSaveRefusesOffContract holds docs/SPEC.md §5.1 for the
// Squirrel save of a fixed root, an every-build leg: <Root>Save refuses every
// row <Root>FixedSave refuses (TestSquirrelFixedSaveRefusesOffContract's
// rows: a ranged integer, a bits(N), a count past its bound or under its Min,
// a string, an enum, a union, a non-finite or ranged float, a bare integer
// past its width, a value of the wrong type) without raising, and writes the
// lawful twin at each boundary so that it reads back exactly. Both saves are
// public, so a value one refuses the other may not write.
func TestSquirrelFixedRootSaveRefusesOffContract(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u := fixedSynthetic(t, fixedSaveRefusalUnit)
	head, _, ok := strings.Cut(fixedSaveRefusalProbe, "foreach (row in rows) {")
	if !ok {
		t.Fatal("fixedSaveRefusalProbe has no row loop to replace")
	}
	probe := head + fixedRootSaveLoop
	dir := t.TempDir()
	writeGenerated(t, dir, u)
	if err := os.WriteFile(filepath.Join(dir, "probe.nut"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "probe.nut", nil)), "\n"), "\n")
	wantLines := strings.Count(probe, "\n  [\"")
	if wantLines == 0 || len(lines) != wantLines {
		t.Fatalf("the probe answered %d rows, want %d:\n%s", len(lines), wantLines, strings.Join(lines, "\n"))
	}
	for _, line := range lines {
		name, verdict, _ := strings.Cut(line, "=")
		want := "refused"
		if strings.Contains(probe, "[\""+name+"\", true,") {
			want = "wrote"
		}
		if verdict != want {
			t.Errorf("%s: RecSave answered %q, want %q", name, verdict, want)
		}
	}
}
