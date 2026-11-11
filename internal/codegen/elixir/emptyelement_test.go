package elixir

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirPacketEmptyElementCompilesClean holds the rule that every legal
// schema's packet output compiles with no warning: an array whose element
// rides no wire bits still emits a loop clause, and that clause must not bind
// a local nothing reads. It compiles the emitted files through the strict
// gate, then round-trips three elements through the empty element's array.
func TestElixirPacketEmptyElementCompilesClean(t *testing.T) {
	t.Parallel()
	elixirBin, elixircBin := elixirToolchain(t)

	out, err := emitUnit(t, map[string]string{
		"Shapes.schema": `package Emptypacket

type Nothing {}

type Crowd
{
    people [..1000000000]Nothing
}

type Locked
{
    values [..1000000000]uint8 | min = 0, max = 0
}

type Tag
{
    const(0xAB, 8)
}

type Tags
{
    items [..1000000000]Tag
}
`,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	dir := t.TempDir()
	ebin := filepath.Join(dir, "ebin")
	if err := os.MkdirAll(ebin, 0o700); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name, data := range out {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	gate, err := filepath.Abs("../../../make/elixir-strict.sh")
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{gate, elixircBin, "-o", ebin}, names...)
	cmd := exec.Command("sh", args...)
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	compiled, err := cmd.CombinedOutput()
	if strings.Contains(string(compiled), "warning:") {
		t.Fatalf("strict compile printed a warning:\n%s", compiled)
	}
	if err != nil {
		t.Fatalf("strict compile: %v\n%s", err, compiled)
	}

	probe := `crowd = %Emptypacket.Crowd{
  people: [%Emptypacket.Nothing{}, %Emptypacket.Nothing{}, %Emptypacket.Nothing{}]
}

wire = Emptypacket.Shapes.write_crowd!(crowd)

case Emptypacket.Shapes.read_crowd_bits(wire, Emptypacket.Shapes.crowd_max_bits()) do
  {:ok, back, _bits_read} ->
    if length(back.people) != 3 do
      IO.puts(:stderr, "FAIL: Crowd read back #{length(back.people)} elements, want 3")
      System.halt(1)
    end

  other ->
    IO.puts(:stderr, "FAIL: Crowd read back #{inspect(other)}")
    System.halt(1)
end
`
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(elixirBin, "-pa", ebin, "probe.exs")
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	if run, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Crowd of three empty elements: %v\n%s", err, run)
	}
}

// elixirToolchain locates the leg's own Elixir and elixirc: $ELIXIR when it
// names one, the pinned dist/ tree, and the tools on PATH. A bench with
// neither fails by name under SCHEMA_REQUIRE_CORPUS and skips by name
// otherwise.
func elixirToolchain(t *testing.T) (elixirBin, elixircBin string) {
	t.Helper()
	slowtest.Gate(t, "the Elixir toolchain")

	if env := os.Getenv("ELIXIR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			elixirBin = env
		} else if bin, err := exec.LookPath(env); err == nil {
			elixirBin = bin
		}
	}
	if elixirBin == "" {
		pinned := "../../../dist/elixir-1.20.4/bin/elixir"
		if abs, err := filepath.Abs(pinned); err == nil {
			if _, err := os.Stat(abs); err == nil {
				elixirBin = abs
			}
		}
	}
	if elixirBin == "" {
		if bin, err := exec.LookPath("elixir"); err == nil {
			elixirBin = bin
		}
	}
	if elixirBin == "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no Elixir is on this bench: not in dist/ and not on PATH")
		}
		t.Skip("no Elixir: unpack it in dist/ or put it on PATH — see make/elixir.mk")
	}

	sibling := filepath.Join(filepath.Dir(elixirBin), "elixirc")
	if _, err := os.Stat(sibling); err == nil {
		elixircBin = sibling
	} else if bin, err := exec.LookPath("elixirc"); err == nil {
		elixircBin = bin
	}
	if elixircBin == "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no elixirc is on this bench: not beside %s and not on PATH", elixirBin)
		}
		t.Skip("no elixirc: unpack it in dist/ or put it on PATH — see make/elixir.mk")
	}
	return elixirBin, elixircBin
}

// toolchainEnv puts the Elixir launcher's own bin directory first so elixir
// and elixirc find erl the way the pinned invocations do.
func toolchainEnv(elixirBin string) []string {
	path := filepath.Dir(elixirBin) + string(os.PathListSeparator) + os.Getenv("PATH")
	return append(os.Environ(), "PATH="+path)
}
