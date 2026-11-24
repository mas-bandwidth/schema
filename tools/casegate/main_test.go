package main

import (
	"strings"
	"testing"
)

func TestRunRefusesMissingFlags(t *testing.T) {
	var out, errs strings.Builder
	for _, args := range [][]string{
		{"-leg", "lua", "-cases", "c", "-owed", "o", "--", "true"},
		{"-name", "x", "-cases", "c", "-owed", "o", "--", "true"},
		{"-name", "x", "-leg", "lua", "-owed", "o", "--", "true"},
		{"-name", "x", "-leg", "lua", "-cases", "c", "--", "true"},
		{"-name", "x", "-leg", "lua", "-cases", "c", "-owed", "o"},
	} {
		if code := run(args, &out, &errs); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}
