package casegate

import (
	"strings"
	"testing"
)

func TestVerdict(t *testing.T) {
	cases := []string{"a", "b"}
	for _, c := range []struct {
		name   string
		exit   int
		stdout string
		owed   map[string]string
		red    string
	}{
		{"green", 0, "case a ok\ncase b ok\n", nil, ""},
		{"green with crlf", 0, "case a ok\r\ncase b ok\r\n", nil, ""},
		{"owed and failing is green", 0, "case a ok\ncase b FAILED: no\n", map[string]string{"b": "the port"}, ""},
		{"owed and silent is green", 0, "case a ok\n", map[string]string{"b": "the port"}, ""},
		{"a missing answer is red", 0, "case a ok\n", nil, "x: lua b: no answer"},
		{"a failed answer is red", 0, "case a ok\ncase b FAILED: bytes\n", nil, "x: lua b: FAILED: bytes"},
		{"an owed case answered ok is red", 0, "case a ok\ncase b ok\n", map[string]string{"b": "the port"}, "and the leg answers it ok"},
		{"an unknown answer is red", 0, "case a ok\ncase b ok\ncase c ok\n", nil, "x: lua c: answered, and the case file has no such case"},
		{"a stale ledger line is red", 0, "case a ok\ncase b ok\n", map[string]string{"z": "gone"}, "x: lua z owed: the case file has no such case"},
		{"two answers are red", 0, "case a ok\ncase a ok\ncase b ok\n", nil, "answered 2 times"},
		{"a non-zero exit is red", 3, "case a ok\ncase b ok\n", nil, "the driver exited 3"},
		{"a whole-leg line with a driver is red", 0, "case a ok\ncase b ok\n", map[string]string{"all": "no driver"}, "but the leg has a driver"},
		{"an answer must stand alone", 0, "the case a ok\ncase b ok\n", nil, "x: lua a: no answer"},
	} {
		red, _ := Verdict("x", "lua", c.exit, []byte(c.stdout), cases, c.owed)
		got := strings.Join(red, "\n")
		if c.red == "" && got != "" || c.red != "" && !strings.Contains(got, c.red) {
			t.Errorf("%s: red = %q, want %q", c.name, got, c.red)
		}
	}
	if red, _ := Verdict("x", "lua", 0, nil, nil, nil); len(red) == 0 {
		t.Errorf("an empty case file: green, want red")
	}
}

func TestReadCases(t *testing.T) {
	got, err := ReadCases("# head\n\nmixed-none Mixed accept 2 None\n  tags-pong Tags accept 2 pong\n")
	if err != nil || strings.Join(got, ",") != "mixed-none,tags-pong" {
		t.Fatalf("ReadCases = %v, %v", got, err)
	}
	if _, err := ReadCases("a x\na y\n"); err == nil {
		t.Fatalf("a case named twice was accepted")
	}
}

func TestReadOwed(t *testing.T) {
	got, err := ReadOwed("# head\nlua b owed: the port\nrust a owed: other\nlua all owed: none\n", "lua")
	if err != nil || len(got) != 2 || got["b"] != "the port" || got["all"] != "none" {
		t.Fatalf("ReadOwed = %v, %v", got, err)
	}
	for _, bad := range []string{"lua b the port\n", "lua b owed:\n", "lua b owed: x\nlua b owed: y\n"} {
		if _, err := ReadOwed(bad, "lua"); err == nil {
			t.Errorf("ReadOwed(%q) accepted", bad)
		}
	}
}
