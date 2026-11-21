package elixir_test

// THE NAME CORPUS, ELIXIR LEG: every case of testdata/name-buildability-emit
// that cases.tsv names for elixir generates and compiles through the strict
// gate (make/elixir-strict.sh: warnings are errors), and a packet case's
// Holder then round-trips. The names are declarations spelled lower case
// (`type memcpy`, `enum kind`), which once lowered to `Probe.memcpy`: not an
// Elixir module name.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/nameemit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// nameCorpusRoundTrip writes the case's Holder at its zero value, reads it
// back and writes it again; the file module of Other.schema carries its codecs.
const nameCorpusRoundTrip = `v = struct(Probe.Holder)
wire = Probe.Other.write_holder!(v)
{:ok, r, _} = Probe.Other.read_holder_bits(wire, bit_size(wire))
^wire = Probe.Other.write_holder!(r)
IO.puts("ok")
`

func TestNameCorpusBuildsElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin, elixircBin := nameCorpusElixir(t)
	gate, err := filepath.Abs(filepath.Join("..", "..", "..", "make", "elixir-strict.sh"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Dir(elixirBin) + string(os.PathListSeparator) + os.Getenv("PATH")
	env := append(os.Environ(), "PATH="+path)
	for _, c := range nameemit.For(t, "elixir") {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			files := nameemit.Generate(t, c, "elixir")
			dir := t.TempDir()
			ebin := filepath.Join(dir, "ebin")
			if err := os.MkdirAll(ebin, 0o700); err != nil {
				t.Fatal(err)
			}
			names := nameemit.Write(t, files, dir, ".ex")
			cmd := testproc.Command(t, "sh", append([]string{gate, elixircBin, "-o", ebin}, names...)...)
			cmd.Dir, cmd.Env = dir, env
			out, err := cmd.CombinedOutput()
			if err != nil || strings.Contains(string(out), "warning:") {
				t.Fatalf("%s (%s): the generated Elixir does not compile clean: %v\n%s", c.Name, c.Note, err, out)
			}
			if _, holder := files["Other.ex"]; !holder || strings.HasPrefix(c.Name, "table_") {
				return
			}
			if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(nameCorpusRoundTrip), 0o600); err != nil {
				t.Fatal(err)
			}
			run := testproc.Command(t, elixirBin, "-pa", ebin, "probe.exs")
			run.Dir, run.Env = dir, env
			if out, err := run.CombinedOutput(); err != nil {
				t.Fatalf("%s (%s): the Holder does not round-trip: %v\n%s", c.Name, c.Note, err, out)
			}
		})
	}
}

// nameCorpusElixir finds elixir and elixirc the way the package's other
// toolchain tests do: $ELIXIR, the pinned dist/ tree, then PATH.
func nameCorpusElixir(t *testing.T) (elixirBin, elixircBin string) {
	t.Helper()
	candidates := []string{os.Getenv("ELIXIR")}
	if abs, err := filepath.Abs(filepath.Join("..", "..", "..", "dist", "elixir-1.20.4", "bin", "elixir")); err == nil {
		candidates = append(candidates, abs)
	}
	candidates = append(candidates, "elixir")
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if bin, err := exec.LookPath(c); err == nil {
			elixirBin = bin
			break
		}
	}
	if elixirBin != "" {
		if bin, err := exec.LookPath(filepath.Join(filepath.Dir(elixirBin), "elixirc")); err == nil {
			elixircBin = bin
		} else if bin, err := exec.LookPath("elixirc"); err == nil {
			elixircBin = bin
		}
	}
	if elixirBin == "" || elixircBin == "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal("SCHEMA_REQUIRE_CORPUS is set and no Elixir toolchain is on this bench: not in dist/ and not on PATH")
		}
		t.Skip("no Elixir toolchain: unpack it in dist/ or put it on PATH — see make/elixir.mk")
	}
	return elixirBin, elixircBin
}
