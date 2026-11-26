// Package squirreldiff holds the Squirrel all-port differential and its
// negative control. It is its own package so that the nightly differential,
// which needs the legs make schema-squirrel-all-port-differential-legs builds
// (test/registry/go-needs.txt), is not part of a go test of ./internal/ci.
package squirreldiff

import (
	"path/filepath"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
