package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVerdict(t *testing.T) {
	cases := []struct {
		name   string
		exit   int
		stdout string
		want   int
		code   int
		say    string
	}{
		{"green", 0, "a\nran 7, failed 0\n", 7, 0, ""},
		{"green with a prefix", 0, "lua packet: ran 7, failed 0\n", 7, 0, ""},
		{"green with crlf", 0, "ran 7, failed 0\r\n", 7, 0, ""},
		{"non-zero exit wins", 3, "ran 7, failed 0\n", 7, 3, "exited 3"},
		{"no count line", 0, "all good\nPASS\n", 7, 1, "printed no"},
		{"the line must stand alone", 0, "we ran 7, failed 0 today\n", 7, 1, "printed no"},
		{"zero ran", 0, "ran 0, failed 0\n", 7, 1, "ran 0"},
		{"failed with exit zero", 0, "ran 7, failed 2\n", 7, 1, "reported 2 failed"},
		{"fewer than registered", 0, "ran 6, failed 0\n", 7, 1, "registers 7"},
		{"more than registered", 0, "ran 8, failed 0\n", 7, 1, "registers 7"},
		{"two count lines", 0, "ran 7, failed 0\nran 7, failed 0\n", 7, 1, "2 \"ran"},
	}
	for _, c := range cases {
		code, msg := verdict("x", c.exit, []byte(c.stdout), c.want)
		if code != c.code || !strings.Contains(msg, c.say) || (c.code == 0 && msg != "") {
			t.Errorf("%s: verdict = %d %q, want %d containing %q", c.name, code, msg, c.code, c.say)
		}
	}
}

func TestRunRefusesAnUnregisteredCount(t *testing.T) {
	for _, args := range [][]string{
		{"-name", "x", "--", "true"},
		{"-name", "x", "-count", "0", "--", "true"},
		{"-count", "3", "--", "true"},
		{"-name", "x", "-count", "3"},
	} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("run(%q) = %d, want 2 (usage)", args, code)
		}
	}
}
