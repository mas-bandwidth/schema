// Package rubystrict holds the Ruby leg strict gate over every generated
// unit (RuboCop, ruby -w, Steep) and its fast form. It is its own package so
// that the whole gate, a nightly row of about ten CPU-minutes, is not part of
// a go test of ./internal/ci, whose package run it took past ten minutes.
package rubystrict

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
