package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A case pattern path.Match cannot read matches no case, and the ruling then
// applies to nothing and says nothing: LoadRulings refuses it by name.
func TestLoadRulingsRefusesAPatternMatchCannotRead(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rulingsPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	const ruling = `{"rulings":[{"id":"R1","date":"2026-11-22","quotes":["q"],"source":"s","applies":[{"surface":"text","case":"%s","langs":["go"]}]}]}`
	for pattern, bad := range map[string]bool{"wire_[a-": true, "wire_*": false} {
		data := strings.Replace(ruling, "%s", pattern, 1)
		if err := os.WriteFile(filepath.Join(root, rulingsPath), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadRulings(root)
		if bad && (err == nil || !strings.Contains(err.Error(), pattern)) {
			t.Errorf("the pattern %q was not refused by name: %v", pattern, err)
		}
		if !bad && err != nil {
			t.Errorf("the pattern %q was refused: %v", pattern, err)
		}
	}
}
