package main

import (
	"os"
	"testing"
)

// test files are not the gate's: internal/vacuous reads them
func TestNotRead(t *testing.T) { _ = os.Remove("t") }
