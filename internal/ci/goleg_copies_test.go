// Package ci. TestGoLegHasNoCopiedIdioms holds the Go leg to a single helper
// where the same hand-written idiom was once copied from function to function
// (schema-go-contract-one-helper-for-copied-idioms): sorted map keys built by
// hand, and the table-closure member lookup that falls back from Tables to
// Structs. Each is one line or one helper now; a copy that comes back is
// named with its file and line.
package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// goLegIdiomFiles are the files the gate scans; they are the files the card
// is allowed to touch.
var goLegIdiomFiles = []string{
	"internal/codegen/gotable/gotable.go",
	"internal/codegen/gotable/cook.go",
	"internal/codegen/gotable/wire.go",
	"internal/codegen/gotable/block.go",
	"internal/codegen/gotable/codecs.go",
	"compiler/generate.go",
	"compiler/builtin.go",
	"ir/ir.go",
}

var sortedKeysByHand = regexp.MustCompile(`^\s+names := make\(\[\]string, 0, len\(`)
var sortNames = regexp.MustCompile(`^\s+sort\.Strings\(names\)`)
var closureLookup = regexp.MustCompile(`^\s+st := u\.Tables\[name\]\s*$`)

// TestGoLegHasNoCopiedIdioms fails, naming file:line for each, when a Go-leg
// file still builds sorted map keys by hand or looks a closure member up by
// hand. Both read the same at BASE; the card replaced them with
// slices.Sorted(maps.Keys(m)) and ir.ClosureMember(u, name).
func TestGoLegHasNoCopiedIdioms(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var findings []string
	for _, rel := range goLegIdiomFiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		lines := strings.Split(string(data), "\n")
		for i := 0; i < len(lines); i++ {
			where := rel + ":" + strconv.Itoa(i+1)
			if sortedKeysByHand.MatchString(lines[i]) {
				sortedSoon := false
				for j := i + 1; j < len(lines) && j <= i+6; j++ {
					if sortNames.MatchString(lines[j]) {
						sortedSoon = true
						break
					}
				}
				if sortedSoon {
					findings = append(findings, where+": sorted map keys built by hand")
				}
			}
			if closureLookup.MatchString(lines[i]) &&
				i+2 < len(lines) &&
				strings.TrimSpace(lines[i+1]) == "if st == nil {" &&
				strings.TrimSpace(lines[i+2]) == "st = u.Structs[name]" {
				findings = append(findings, where+": closure member lookup by hand")
			}
		}
	}
	if len(findings) > 0 {
		t.Errorf("Go leg still copies hand-written idioms:\n%s", strings.Join(findings, "\n"))
	}
}
