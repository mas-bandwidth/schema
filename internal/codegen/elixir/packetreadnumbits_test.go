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

// TestElixirPacketReadRefusesNegativeBitCount holds the packet read's @spec:
// num_bits is a non_neg_integer(), so a negative count is refused through both
// read_<name>_bits/2 and the delegating read_<name>/2. It runs the committed
// generated/elixir unit, and still reads each valid payload at its exact bit
// count so the refusal is the count's and not the read's.
func TestElixirPacketReadRefusesNegativeBitCount(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	names, err := committedElixirModules()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ebin := filepath.Join(dir, "ebin")
	if err := os.MkdirAll(ebin, 0o755); err != nil {
		t.Fatal(err)
	}
	compile := append([]string{"-o", ebin}, names...)
	cmd := exec.Command(elixircBinary(t), compile...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile committed generated/elixir: %v\n%s", err, out)
	}
	probe := filepath.Join(dir, "probe.exs")
	if err := os.WriteFile(probe, []byte(packetReadNumBitsProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(elixirBinary(t), "-pa", ebin, probe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("negative bit count probe: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("negative bit count probe did not report ok:\n%s", out)
	}
}

// committedElixirModules lists the committed generated/elixir files as absolute
// paths, sorted so a run reads the same files in the same order.
func committedElixirModules() ([]string, error) {
	dir, err := filepath.Abs(committedElixir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ex") {
			continue
		}
		names = append(names, filepath.Join(dir, e.Name()))
	}
	sort.Strings(names)
	return names, nil
}

// elixirBinary is the leg's own toolchain: $ELIXIR when it names one, the
// pinned dist/ tree, and the Elixir on PATH. A bench with no Elixir skips
// without the corpus promise, and FAILS BY NAME under SCHEMA_REQUIRE_CORPUS.
func elixirBinary(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ELIXIR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		if bin, err := exec.LookPath(env); err == nil {
			return bin
		}
	}
	bin, err := filepath.Abs("../../../dist/elixir-1.20.4/bin/elixir")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	if found, err := exec.LookPath("elixir"); err == nil {
		return found
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no Elixir is on this bench: not at %s and not on PATH", bin)
	}
	t.Skip("no Elixir: unpack it in dist/ or put it on PATH — see make/elixir.mk")
	return ""
}

// elixircBinary resolves the compiler beside elixirBinary the same way:
// $ELIXIRC, then the binary next to the leg's elixir, then PATH.
func elixircBinary(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ELIXIRC"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		if bin, err := exec.LookPath(env); err == nil {
			return bin
		}
	}
	sibling := filepath.Join(filepath.Dir(elixirBinary(t)), "elixirc")
	if _, err := os.Stat(sibling); err == nil {
		return sibling
	}
	if found, err := exec.LookPath("elixirc"); err == nil {
		return found
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no elixirc is on this bench: not at %s and not on PATH", sibling)
	}
	t.Skip("no elixirc: unpack it in dist/ or put it on PATH — see make/elixir.mk")
	return ""
}

// otpBin is the OTP beside the Elixir `elixir` needs on PATH: $ERL_BIN, the
// pinned dist/ tree, then the `erl` already on PATH.
func otpBin(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ERL_BIN"); env != "" {
		return env
	}
	p, err := filepath.Abs("../../../dist/otp-29.0.5/bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, "erl")); err == nil {
		return p
	}
	if found, err := exec.LookPath("erl"); err == nil {
		return filepath.Dir(found)
	}
	return p
}

// packetReadNumBitsProbe exercises a zero-bit type and a type with fields
// through both read forms. A negative count is :error; the valid payload still
// reads at its exact bit count.
const packetReadNumBitsProbe = `
defmodule PacketReadProbe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def refused(result, label) do
    check(result == :error, label <> ": got " <> inspect(result))
  end
end

for bits <- [-1, -5] do
  PacketReadProbe.refused(
    Example.Wire.read_heartbeat_bits(<<>>, bits),
    "read_heartbeat_bits/2 with num_bits #{bits}"
  )

  PacketReadProbe.refused(
    Example.Wire.read_heartbeat(<<>>, bits),
    "read_heartbeat/2 with num_bits #{bits}"
  )
end

case Example.Wire.read_heartbeat_bits(<<>>, 0) do
  {:ok, %Example.Heartbeat{}, 0} -> :ok
  other -> PacketReadProbe.check(false, "read_heartbeat_bits/2 at 0 bits: " <> inspect(other))
end

case Example.Wire.read_heartbeat(<<>>, 0) do
  {:ok, %Example.Heartbeat{}} -> :ok
  other -> PacketReadProbe.check(false, "read_heartbeat/2 at 0 bits: " <> inspect(other))
end

payload = Example.Wire.write_test!(%Example.Test{test_a: 1, test_b: 2, test_c: 3, test_d: 4})

for bits <- [-1, -5] do
  PacketReadProbe.refused(
    Example.Wire.read_test_bits(payload, bits),
    "read_test_bits/2 with num_bits #{bits}"
  )

  PacketReadProbe.refused(
    Example.Wire.read_test(payload, bits),
    "read_test/2 with num_bits #{bits}"
  )
end

case Example.Wire.read_test_bits(payload, 46) do
  {:ok, %Example.Test{test_a: 1, test_b: 2, test_c: 3, test_d: 4}, 46} -> :ok
  other -> PacketReadProbe.check(false, "read_test_bits/2 at 46 bits: " <> inspect(other))
end

case Example.Wire.read_test(payload, 46) do
  {:ok, %Example.Test{test_a: 1, test_b: 2, test_c: 3, test_d: 4}} -> :ok
  other -> PacketReadProbe.check(false, "read_test/2 at 46 bits: " <> inspect(other))
end

IO.puts("ok")
`
