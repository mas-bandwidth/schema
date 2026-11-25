package main

import (
	"strings"
	"testing"
)

func TestMessageFuzzDeadRootsAreRed(t *testing.T) {
	roots := []*messageFuzzRoot{{unit: "a", root: "X"}, {unit: "b", root: "Y"}, {unit: "c", root: "Z"}}
	dead := deadMessageRoots(roots, []bool{true, false, true})
	if len(dead) != 1 || dead[0] != "b.Y" {
		t.Fatalf("dead = %v", dead)
	}
	// The dead root is red and named.
	err := checkDeadMessageRoots(dead)
	if err == nil || !strings.Contains(err.Error(), "b.Y") {
		t.Fatalf("a dead root passed: %v", err)
	}
	// Every root answered: green.
	if err := checkDeadMessageRoots(nil); err != nil {
		t.Fatal(err)
	}
}
