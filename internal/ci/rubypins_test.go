package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestRubyPins holds the Ruby leg's toolchain pins, its analyzer lock and its
// CI action to each other (docs/PORT-RUBY.md, THE PIN; docs/plans/ruby-leg.md,
// R1). The pins live in make/ruby-toolchain.mk alone; the lock names each
// analyzer and its sha256; the action restores dist/ under a key built from
// the same pins and the same lock; and no leg file starts an interpreter from
// PATH.
func TestRubyPins(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mkPath := filepath.Join(root, "make", "ruby-toolchain.mk")
	mkBytes, err := os.ReadFile(mkPath)
	if err != nil {
		t.Fatalf("read make/ruby-toolchain.mk: %v", err)
	}
	mk := string(mkBytes)

	if !regexp.MustCompile(`(?m)^TOOLCHAIN_LEGS\s*\+=\s*ruby\s*$`).MatchString(mk) {
		t.Error("make/ruby-toolchain.mk does not append ruby to TOOLCHAIN_LEGS")
	}

	pinsMatch := regexp.MustCompile(`(?m)^TOOLCHAIN_PINS_ruby\s*:=\s*(.+)$`).FindStringSubmatch(mk)
	if pinsMatch == nil {
		t.Fatal("make/ruby-toolchain.mk does not define TOOLCHAIN_PINS_ruby")
	}
	pinsFields := strings.Fields(pinsMatch[1])
	wantPins := []string{"RUBY", "RUBY_FLOOR", "RUBOCOP", "STEEP", "RBS"}
	if strings.Join(pinsFields, " ") != strings.Join(wantPins, " ") {
		t.Errorf("TOOLCHAIN_PINS_ruby = %v, want %v", pinsFields, wantPins)
	}
	for _, pin := range wantPins {
		probe := `$(call toolchain_probe,ruby,` + pin + `,`
		if !strings.Contains(mk, probe) {
			t.Errorf("make/ruby-toolchain.mk does not probe %s via %s", pin, probe)
		}
	}

	def := func(name string) string {
		t.Helper()
		m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:=\s*(\S+)\s*$`).FindStringSubmatch(mk)
		if m == nil {
			t.Fatalf("make/ruby-toolchain.mk does not define %s", name)
		}
		return m[1]
	}

	floor := def("RUBY_FLOOR_VERSION")
	current := def("RUBY_CURRENT_VERSION")
	if floor != "3.4.12" {
		t.Errorf("RUBY_FLOOR_VERSION = %q, want CRuby 3.4.12", floor)
	}
	if current != "4.0.9" {
		t.Errorf("RUBY_CURRENT_VERSION = %q, want CRuby 4.0.9", current)
	}

	shaRE := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, ver := range []string{floor, current} {
		sm := regexp.MustCompile(`(?m)^RUBY_SHA256_` + regexp.QuoteMeta(ver) + `\s*:=\s*(\S+)\s*$`).FindStringSubmatch(mk)
		if sm == nil {
			t.Errorf("make/ruby-toolchain.mk is missing RUBY_SHA256_%s", ver)
			continue
		}
		if !shaRE.MatchString(sm[1]) {
			t.Errorf("RUBY_SHA256_%s = %q is not a 64-hex sha256 digest", ver, sm[1])
		}
	}

	// The analyzer versions are named once in the fragment and once in the
	// lock; the check reads the lock, so the two must agree here.
	wantAnalyzers := map[string]string{
		"rubocop": "1.92.2",
		"steep":   "2.1.0",
		"rbs":     "4.2.0",
	}
	for name, want := range wantAnalyzers {
		got := def("RUBY_" + strings.ToUpper(name) + "_VERSION")
		if got != want {
			t.Errorf("RUBY_%s_VERSION = %q, want %q", strings.ToUpper(name), got, want)
		}
	}

	lockPath := filepath.Join(root, "make", "ruby-gems.lock")
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read make/ruby-gems.lock: %v", err)
	}
	lockLineRE := regexp.MustCompile(`^([A-Za-z0-9_.-]+)==([0-9A-Za-z_.-]+) sha256:([0-9a-f]{64})$`)
	locked := map[string]string{}
	reqCount := 0
	for i, line := range strings.Split(strings.TrimSpace(string(lockBytes)), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		reqCount++
		m := lockLineRE.FindStringSubmatch(trimmed)
		if m == nil {
			t.Errorf("make/ruby-gems.lock:%d %q is not pinned with name==version and sha256:<64hex>", i+1, trimmed)
			continue
		}
		if _, dup := locked[m[1]]; dup {
			t.Errorf("make/ruby-gems.lock names %s twice", m[1])
		}
		locked[m[1]] = m[2]
	}
	if reqCount == 0 {
		t.Error("make/ruby-gems.lock contains no hash-locked gem lines")
	}
	for name, want := range wantAnalyzers {
		if got := locked[name]; got != want {
			t.Errorf("make/ruby-gems.lock pins %s %q, want %q", name, got, want)
		}
	}

	// The action restores the pins under a key naming every pin and the lock,
	// and it checks the restored tree before it is used.
	actionPath := filepath.Join(root, ".github", "actions", "ruby", "action.yml")
	actionBytes, err := os.ReadFile(actionPath)
	if err != nil {
		t.Fatalf("read .github/actions/ruby/action.yml: %v", err)
	}
	action := string(actionBytes)
	for _, want := range []string{
		"RUBY_FLOOR_VERSION",
		"RUBY_CURRENT_VERSION",
		"ruby-gems.lock",
		"make -j2 dist-ruby",
		"make ruby-toolchain-check",
	} {
		if !strings.Contains(action, want) {
			t.Errorf(".github/actions/ruby/action.yml does not name %q", want)
		}
	}

	workflowPath := filepath.Join(root, ".github", "workflows", "ruby-leg.yml")
	workflowBytes, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read .github/workflows/ruby-leg.yml: %v", err)
	}
	workflow := string(workflowBytes)
	for _, want := range []string{
		"./.github/actions/ruby",
		"make ruby-toolchain-check",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf(".github/workflows/ruby-leg.yml does not name %q", want)
		}
	}

	checkPath := filepath.Join(root, "make", "ruby-toolchain-check.sh")
	if _, err := os.Stat(checkPath); err != nil {
		t.Errorf("missing make/ruby-toolchain-check.sh: %v", err)
	}
	if !regexp.MustCompile(`(?m)^ruby-toolchain-negative-control:`).MatchString(mk) {
		t.Error("make/ruby-toolchain.mk does not define ruby-toolchain-negative-control")
	}

	manifest, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"ruby-toolchain-negative-control"`) {
		t.Error("make/negative-controls.json does not register ruby-toolchain-negative-control")
	}

	var legFiles []string
	for _, pattern := range []string{
		"make/ruby*.mk",
		"make/ruby*.sh",
		".github/actions/ruby/*.yml",
		".github/workflows/ruby-leg.yml",
	} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		legFiles = append(legFiles, matches...)
	}
	if len(legFiles) == 0 {
		t.Fatal("found no make/ruby* files to check")
	}

	bareRubyRE := regexp.MustCompile(`(^[ \t]*ruby([ \t]|$)|[;&|(]ruby([ \t]|$))`)
	envRubyRE := regexp.MustCompile(`/usr/bin/env\s+ruby`)
	execRubyRE := regexp.MustCompile(`(Command|LookPath)\("ruby`)

	for _, f := range legFiles {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.ToSlash(rel) == "make/ruby-toolchain-check.sh" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if bareRubyRE.MatchString(line) || envRubyRE.MatchString(line) || execRubyRE.MatchString(line) {
				t.Errorf("%s:%d runs an interpreter from PATH: %s", rel, i+1, trimmed)
			}
		}
	}
}
