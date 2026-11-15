package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseStorageGatesAreDefined holds the two targets of each release
// storage gate (docs/SPEC.md §5.1) to the language fragment that owns it. A
// gate whose positive target is missing never runs; a negative control with no
// positive target controls nothing.
func TestReleaseStorageGatesAreDefined(t *testing.T) {
	root := conformanceRoot(t)
	// Rust is not here: its message storage check is a debug assert
	// (docs/SPEC.md §5.1), held by tables-rust-message-storage-assert-negative-control.
	for _, lang := range []string{"cpp", "cs", "dart"} {
		t.Run(lang, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "make", lang+".mk"))
			if err != nil {
				t.Fatal(err)
			}
			target := "write-refusal-storage-release-" + lang
			for _, name := range []string{target, target + "-negative-control"} {
				if !strings.Contains(string(body), "\n"+name+":") {
					t.Errorf("make/%s.mk does not define %s", lang, name)
				}
			}
		})
	}
}
