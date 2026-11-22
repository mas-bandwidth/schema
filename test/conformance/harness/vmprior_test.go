package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE MESSAGE-PRIOR ANSWER THROUGH THE VM SHIMS (FORMAT.md `message-prior`;
// scorePrior). The Luau, MOO and Lobster legs have no io library, so a shell
// driver turns the program's stdout into one file per case. A prior row may
// owe a refusal, and a refusal the leg RAN is `<case>.refused`, never the
// `<case>.absent` that counts a missing codec, so each shim must write the
// program's `<case> refused <reason>` line as that file, beside the bytes of a
// row it read. Each driver runs in a scratch tree under a stand-in
// interpreter that prints those two lines, so no conformance run's files are
// touched; without the refused branch the luau shim writes the word as bytes
// and the moo and lobster shims refuse the line.

const priorManifest = "prior-connection p_past tblprior 0x01 ann.bin\n" +
	"prior-message p_past p_past Prior file.bin past.bin\n" +
	"prior-connection p_inside tblprior 0x01 ann.bin\n" +
	"prior-message p_inside p_inside Prior file.bin inside.bin\n"

const priorStandIn = "printf 'p_past refused out_of_range\\np_inside 0a0b\\n'\n"

// priorTree is a scratch repository root holding the committed driver of leg
// (and the extra committed files named), the manifest, and the fixtures it
// names.
func priorTree(t *testing.T, leg string, extra ...string) (dir, manifest, out string) {
	t.Helper()
	for _, tool := range []string{"sh", "perl", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on PATH", tool)
		}
	}
	root := filepath.Join("..", "..", "..")
	dir = t.TempDir()
	for _, rel := range append([]string{"driver"}, extra...) {
		src, err := os.ReadFile(filepath.Join(root, "test", "conformance", leg, rel))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "test", "conformance", leg, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"ann.bin", "file.bin", "past.bin", "inside.bin"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte{1}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest = filepath.Join(dir, "manifest.txt")
	if err := os.WriteFile(manifest, []byte(priorManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, manifest, filepath.Join(dir, "out")
}

func standIn(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func runPriorDriver(t *testing.T, dir, leg, manifest, out string, env ...string) {
	t.Helper()
	cmd := exec.Command("sh", filepath.Join("test", "conformance", leg, "driver"), manifest, "message-prior", out)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the %s driver failed on the message-prior surface: %v\n%s", leg, err, b)
	}
}

// mustPriorAnswers holds the two answers: the refusal as <case>.refused with
// the leg's reason and no bytes or absence beside it, the read as its bytes.
func mustPriorAnswers(t *testing.T, leg, out string) {
	t.Helper()
	reason, err := os.ReadFile(filepath.Join(out, "p_past.refused"))
	if err != nil {
		t.Fatalf("%s: the refused read wrote no p_past.refused: %v", leg, err)
	}
	if string(reason) != "out_of_range" {
		t.Errorf("%s: p_past.refused holds %q, want the leg's reason", leg, reason)
	}
	for _, stray := range []string{"p_past", "p_past.absent"} {
		if _, err := os.Stat(filepath.Join(out, stray)); err == nil {
			t.Errorf("%s: a refused read also wrote %s", leg, stray)
		}
	}
	got, err := os.ReadFile(filepath.Join(out, "p_inside"))
	if err != nil || string(got) != "\x0a\x0b" {
		t.Errorf("%s: p_inside holds %x (%v), want 0a0b beside the refusal", leg, got, err)
	}
}

func TestLuauDriverWritesPriorRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "sh and perl (it runs the Luau conformance driver)")
	dir, manifest, out := priorTree(t, "luau", "main.luau")
	luau := standIn(t, dir, "luau", priorStandIn)
	runPriorDriver(t, dir, "luau", manifest, out, "LUAU="+luau)
	mustPriorAnswers(t, "luau", out)
}

func TestMooDriverWritesPriorRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "sh and perl (it runs the MOO conformance driver)")
	dir, manifest, out := priorTree(t, "moo", "main.moo", "stage.pl", "split.pl")
	if err := os.MkdirAll(filepath.Join(dir, "gen", "tblprior"), 0o755); err != nil {
		t.Fatal(err)
	}
	moo := standIn(t, dir, "moo", priorStandIn)
	runPriorDriver(t, dir, "moo", manifest, out, "MOO="+moo, "SCHEMA_MOO_GENERATED=gen", "MOO_DRIVER_STAGE=stage")
	mustPriorAnswers(t, "moo", out)
	// the stager hands the surface the files its prior lines name
	inputs, err := os.ReadFile(filepath.Join(dir, "stage", "message-prior", "inputs.moo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"ann.bin", "past.bin", "inside.bin"} {
		if !strings.Contains(string(inputs), `"`+f+`"`) {
			t.Errorf("moo: the message-prior stage holds no %s:\n%s", f, inputs)
		}
	}
}

func TestLobsterDriverWritesPriorRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "sh, awk and perl (it runs the Lobster conformance driver)")
	dir, manifest, out := priorTree(t, "lobster", "main.lobster")
	unit := filepath.Join(dir, "units", "tblprior")
	if err := os.MkdirAll(unit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unit, "schema_tables.lobster"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// the stand-in answers `list` with the surface and every unit run with the
	// two lines; the driver joins each prior-message to its unit
	lobster := standIn(t, dir, "lobster", `for a; do last=$prev; prev=$a; done
if [ "$last" = list ]; then echo message-prior; exit 0; fi
[ "$prev" = tblprior ] || { echo "stand-in: ran unit $prev" >&2; exit 1; }
`+priorStandIn)
	runPriorDriver(t, dir, "lobster", manifest, out, "LOBSTER="+lobster, "LOBSTER_CONFORMANCE_UNITS=units")
	mustPriorAnswers(t, "lobster", out)
}
