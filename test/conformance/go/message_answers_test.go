package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMessageMissingBindingIsNotARefusal(t *testing.T) {
	old := messageCodecs
	messageCodecs = nil
	t.Cleanup(func() { messageCodecs = old })
	for _, tc := range []struct {
		unit, root string
		variable   bool
	}{
		{"backenddemo", "LoginRequest", false},
		{"tabledemo", "RootConfig", false},
		{"graphdemo", "Scene", true},
		{"missing", "Root", false},
	} {
		t.Run(tc.unit+"/"+tc.root, func(t *testing.T) {
			out := t.TempDir()
			lines := []line{{"connection", "conn", tc.unit, "0", "unused"}, {"message", "answer", "conn", tc.root, "unused", "unused"}}
			if err := surfaceMessage(lines, out); err != nil {
				t.Fatal(err)
			}
			name, want := "answer.absent", ""
			if tc.variable {
				name, want = "answer", "refused\n"
			}
			got, err := os.ReadFile(filepath.Join(out, name))
			if err != nil || string(got) != want {
				t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
			}
		})
	}
}

func TestMessageAnswersPreserveTheReadVerdict(t *testing.T) {
	old := messageCodecs
	t.Cleanup(func() { messageCodecs = old })
	for _, tc := range []struct {
		name     string
		accepted bool
		answer   []byte
	}{
		{"refused", false, nil},
		{"accepted", true, []byte{2, 17}},
		{"writer failure", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			input := filepath.Join(out, "input")
			if err := os.WriteFile(input, []byte{1}, 0600); err != nil {
				t.Fatal(err)
			}
			messageCodecs = []messageCodec{{unit: "probe", root: "Root", run: func([]byte, []byte, bool) ([]byte, report, bool) {
				return tc.answer, report{unknown: 3}, tc.accepted
			}}}
			err := surfaceMessage([]line{{"connection", "conn", "probe", "0", input}, {"message", "answer", "conn", "Root", input, input}}, out)
			if tc.accepted && tc.answer == nil {
				if err == nil {
					t.Fatal("accepted read with failed writer did not fail")
				}
				for _, name := range []string{"answer", "answer.absent"} {
					if _, err := os.Stat(filepath.Join(out, name)); !os.IsNotExist(err) {
						t.Fatalf("writer failure published %s: %v", name, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := string(tc.answer)
			if !tc.accepted {
				want = "refused\n"
			}
			got, err := os.ReadFile(filepath.Join(out, "answer"))
			if err != nil || string(got) != want {
				t.Fatalf("answer = %q, %v; want %q", got, err, want)
			}
		})
	}
}
