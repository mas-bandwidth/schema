package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOptVerifyVMWidenedCoverage verifies that every one of the 9 VM legs in
// make/verify-vm.mk wires both verify-deep-<lang> (all-27-feature packet wire,
// hostile bit-mutations, stateful reuse, &node, descriptors, UnitView) and
// verify-cards-<lang> (all 9 cards surfaces), tracks schema.lock dependencies,
// and registers all 4 optimization-sabotage negative-control classes.
func TestOptVerifyVMWidenedCoverage(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	vmBytes, err := os.ReadFile(filepath.Join(root, "make", "verify-vm.mk"))
	if err != nil {
		t.Fatalf("read make/verify-vm.mk: %v", err)
	}
	vmText := string(vmBytes)

	if !strings.Contains(vmText, "build/vmverify:") {
		t.Errorf("make/verify-vm.mk missing build/vmverify rule")
	}
	if !strings.Contains(vmText, "CONFORMANCE_UNIT_LOCKS") {
		t.Errorf("make/verify-vm.mk missing CONFORMANCE_UNIT_LOCKS schema.lock dependency")
	}
	if !strings.Contains(vmText, "VERIFY_CARDS_SURFACES := load,save,render,parse,bounds,upgrade,edit,taskhash,view") {
		t.Errorf("make/verify-vm.mk missing full 9-surface VERIFY_CARDS_SURFACES definition")
	}

	for _, lang := range vmOptVerifyLangs {
		for _, rule := range []string{
			fmt.Sprintf("verify-deep-%s:", lang),
			fmt.Sprintf("verify-cards-%s:", lang),
		} {
			if !strings.Contains(vmText, rule) {
				t.Errorf("make/verify-vm.mk missing %q", rule)
			}
		}
		benchLine := fmt.Sprintf("_verify-%s-bench:", lang)
		var foundBench string
		for line := range strings.SplitSeq(vmText, "\n") {
			if strings.HasPrefix(line, benchLine) {
				foundBench = line
				break
			}
		}
		if foundBench == "" {
			t.Errorf("make/verify-vm.mk missing %q", benchLine)
			continue
		}
		for _, dep := range []string{"verify-deep-" + lang, "verify-cards-" + lang} {
			if !strings.Contains(foundBench, dep) {
				t.Errorf("%s missing prerequisite %q in %q", benchLine, dep, foundBench)
			}
		}
	}

	ncBytes, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.d", "verify-negative-controls.json"))
	if err != nil {
		t.Fatalf("read verify-negative-controls.json: %v", err)
	}
	ncText := string(ncBytes)
	for _, nc := range []string{
		"verify-sabotage-negative-control",
		"verify-sabotage-packet-negative-control",
		"verify-sabotage-fixed-negative-control",
		"verify-sabotage-variable-negative-control",
		"verify-sabotage-retain-negative-control",
	} {
		if !strings.Contains(vmText, nc+":") {
			t.Errorf("make/verify-vm.mk missing target %q", nc)
		}
		if !strings.Contains(ncText, `"`+nc+`"`) {
			t.Errorf("verify-negative-controls.json missing %q", nc)
		}
	}
}
