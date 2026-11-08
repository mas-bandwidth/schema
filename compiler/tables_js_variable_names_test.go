package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Scan actual module bindings, including private helpers, rather than mention
// counts in comments. A private function still collides with a declared class.
var jsVariableBinding = regexp.MustCompile(`(?m)^(?:export )?(?:async )?(?:function|class|const|let|var) ([A-Za-z_$][A-Za-z0-9_$]*)`)

func jsVariableRuntimeBindings(files map[string][]byte) map[string]bool {
	result := map[string]bool{}
	for file, data := range files {
		if !strings.HasSuffix(file, "Variable.js") {
			continue
		}
		for _, m := range jsVariableBinding.FindAllStringSubmatch(string(data), -1) {
			if !strings.HasPrefix(m[1], "Name") {
				result[m[1]] = true
			}
		}
	}
	return result
}
func jsVariableNameCorpus(t *testing.T) map[string][]byte {
	t.Helper()
	u := unitFromSource(t, `package names
 enum NameMode { A, B }
 flags NameMask { A, B }
 union NameChoice { number int32
 text string(8)
 }
 table NameLeaf { value int32 }
 table NameProbe | integrity="crc32c" {
 stamp timestamp | required
 child *NameLeaf
 rows []NameLeaf
 labels []?string(8) | total=16
 unbounded []string
 mapping map[string]NameLeaf
 optional ?map[uint32]int32
 choice NameChoice
 mode NameMode
 mask NameMask
 }
 `)
	files, err := New().Generate(u, "js", nil)
	if err != nil {
		t.Fatal(err)
	}
	return files
}
func TestJavaScriptVariableRuntimeNamesAreClaimed(t *testing.T) {
	emitted := jsVariableRuntimeBindings(jsVariableNameCorpus(t))
	if len(emitted) < 100 {
		t.Fatalf("runtime inventory unexpectedly small: %d", len(emitted))
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if tablenames.By(name)&tablenames.Js == 0 {
			t.Errorf("UNCLAIMED_JS_VARIABLE %s", name)
			continue
		}
		errs := checkErrors(t, "package probe\nenum "+name+" { A, B }\ntable Holder { value "+name+" }\n")
		collision := false
		for _, err := range errs {
			if strings.Contains(err.Error(), "TABLE-wire runtime") {
				collision = true
			}
		}
		if !collision {
			t.Errorf("runtime binding %s accepted or refused for the wrong reason: %v", name, errs)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Js) {
		if strings.HasPrefix(name, "TableVariable") || strings.HasPrefix(name, "TableIntegrity") || strings.HasPrefix(name, "tableRetain") || name == "TableRetainKnown" {
			if !emitted[name] {
				t.Errorf("claimed JavaScript variable binding %s is never emitted", name)
			}
		}
	}
	t.Logf("checked %d actual variable-module runtime bindings", len(emitted))
}
func TestJavaScriptVariableRuntimeNameScanNegativeControl(t *testing.T) {
	files := map[string][]byte{"ProbeVariable.js": []byte("function TableVariableUnclaimedProbe() {}\n// function TableVariableComment() {}\n  function TableVariableNested() {}\nexport class NameProbe {}\n")}
	got := jsVariableRuntimeBindings(files)
	if len(got) != 1 || !got["TableVariableUnclaimedProbe"] || tablenames.Registered("TableVariableUnclaimedProbe") {
		t.Fatalf("blind or overbroad binding scan: %v", got)
	}
}
