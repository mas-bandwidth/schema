package cpptable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The wide-list fixture entered the generated corpus without entering the
// warning inventory. Exercise actual emission and the production scanner so
// an omitted file or unrecorded inherited warning cannot look like a pass.
func TestCppWidelistWarningInventory(t *testing.T) {
	slowtest.Gate(t, "clang++ (wide-list generated warning inventory)")
	clang := os.Getenv("CLANGXX")
	if clang == "" {
		clang = "clang++"
	}
	if _, err := exec.LookPath(clang); err != nil {
		t.Skip("clang++ is not installed")
	}
	serialize := os.Getenv("SERIALIZE")
	if serialize == "" {
		serialize = "../../../../serialize"
	}
	serialize, err := filepath.Abs(serialize)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(serialize, "serialize.h")); err != nil {
		t.Skip("serialize.h is not installed")
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(repo, "tables/widelist/Wides.schema"))
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	const tree = "build/tables-generated/widelist"
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, tree), 0700); err != nil {
		t.Fatal(err)
	}
	var expected []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(work, tree, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".h") || strings.HasSuffix(name, ".cpp") {
			expected = append(expected, tree+"/"+name)
		}
	}
	slices.Sort(expected)
	want := []string{"WidelistdemoView.cpp", "WidelistdemoView.h", "Wides.h", "WidesBlock.h", "WidesTable.cpp", "WidesTable.h", "WidesWire.h"}
	for i := range want {
		want[i] = tree + "/" + want[i]
	}
	if !slices.Equal(expected, want) {
		t.Fatalf("wide-list emission inventory: got %v, want %v", expected, want)
	}
	ratchet, err := os.ReadFile(filepath.Join(repo, "make/cpp-warn-allow.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var selected strings.Builder
	for line := range strings.SplitSeq(string(ratchet), "\n") {
		if strings.HasPrefix(line, tree+"/") {
			selected.WriteString(line + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(work, "allow.txt"), []byte(selected.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", filepath.Join(repo, "make/checks/cpp-clean-warnings.sh"), "allow.txt", "check", "build/tables-generated")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "CLANGXX="+clang, "SERIALIZE_DIR="+serialize, "JOBS=2", "OUT=build/warning-inventory")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wide-list warning inventory: %v\n%s", err, out)
	}
	scanned, err := os.ReadFile(filepath.Join(work, "build/warning-inventory/files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(scanned)); !slices.Equal(got, expected) {
		t.Fatalf("warning scanner omitted generated files: got %v, want %v", got, expected)
	}
}
