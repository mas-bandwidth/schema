package roadmap

import (
	"strings"
	"testing"
)

func TestDoneEvidenceNamesExistingTests(t *testing.T) {
	t.Parallel()

	root := "../.."
	nodes, order, err := loadDoneNodes(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkDoneEvidence(root, nodes, order); err != nil {
		t.Fatal(err)
	}

	const leaf = "vt/wide-scalars/rust"
	node := nodes[leaf]
	original := append([]string(nil), node.evidence...)
	node.evidence = append(node.evidence, "internal/codegen/rusttable/rusttable_test.go: TestDoneEvidenceMissingName")
	err = checkDoneEvidence(root, nodes, order)
	node.evidence = original
	if err == nil || !strings.Contains(err.Error(), "TestDoneEvidenceMissingName") {
		t.Fatalf("negative control: a planted missing test name was not refused: %v", err)
	}
	t.Logf("negative control: %v", err)
}
