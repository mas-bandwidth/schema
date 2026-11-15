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
<<<<<<< HEAD
	// C, C#, Java, Dart and Rust are not here: an assert leg's storage check
	// is a debug assert (docs/SPEC.md §5.1), held by
	// write-check-storage-debug-c, write-refusal-storage-debug-cs,
	// write-refusal-storage-debug-java, write-refusal-storage-debug-dart and
	// tables-rust-message-storage-assert.
	for _, lang := range []string{"cpp"} {
=======
	// C, C++, C#, Java and Dart are not here: an assert leg's storage check is a
	// debug assert (docs/SPEC.md §5.1), held by write-check-storage-debug-c,
	// write-check-storage-debug-cpp, write-refusal-storage-debug-cs,
	// write-refusal-storage-debug-java and write-refusal-storage-debug-dart.
	for _, lang := range []string{"rust"} {
>>>>>>> 1d9869f043032919396b75bda10e18d6ddbb0938
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
