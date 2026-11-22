package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElixirMessageStrictMode holds the OPT-IN STRICT MODE of the
// announcement read (docs/SPEC-TABLES.md §3.3, THE LANGUAGE-NEUTRAL SHAPE) to
// the Go reference's verdicts (internal/codegen/gotable/messageform_strict_test.go):
// with `strict` unset an endpoint of another build version reads clean; with
// it set the unit's own version reads clean and another is refused
// `:build_version_mismatch`, setting no vocabulary, keeping the peer's build
// version, naming both versions (the vocabulary's `remote` and `own`), never
// `:malformed`, and terminal: the next announcement is
// `:second_announcement` and a batch is `:no_vocabulary`.
func TestElixirMessageStrictMode(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")
	if out, err := elixirMessageStrictMode(t, nil); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestElixirMessageStrictModeNegativeControl plants the old reader back (no
// strict clause) in TableMessage.ex and requires the probe to go red.
func TestElixirMessageStrictModeNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")
	const anchor = "when vocab.strict and build_version != @build_version ->"
	out, err := elixirMessageStrictMode(t, func(src string) string {
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("TableMessage.ex carries the strict clause %d times, want 1", strings.Count(src, anchor))
		}
		return strings.Replace(src, anchor, "when false and vocab.strict and build_version != @build_version ->", 1)
	})
	if err == nil {
		t.Fatal("the reader without the strict clause still passes the strict probe")
	}
	if !strings.Contains(out, "FAIL: strict other-version read") {
		t.Fatalf("the planted reader failed for another reason:\n%s", out)
	}
}

func elixirMessageStrictMode(t *testing.T, patch func(string) string) (string, error) {
	t.Helper()
	u := unitFrom(t, "package strictprobe\n\nfixed table Root {\n a int32\n b bool\n c uint8\n}\n")
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	module, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("the unit grows no %s.ex; files are %v", TableMessageModule, keysOf(tables))
	}
	if patch != nil {
		module = []byte(patch(string(module)))
	}
	own := ir.BuildVersion(u)
	probe := fmt.Sprintf(`defmodule Probe do
  alias Strictprobe.TableMessage, as: M
  import Bitwise

  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  # the unit's own announcement with its build version word replaced: byte 0
  # is the form, byte 1 the version's reference, byte 2 its kind (9)
  def endpoint(version) do
    <<head::binary-size(3), _::little-unsigned-64, tail::binary>> = M.announce()
    <<head::binary, version::little-unsigned-64, tail::binary>>
  end

  def run do
    own = %d
    remote = bxor(own, 0xFFFFFFFFFFFFFFFF)
    <<_, _, kind, word::little-unsigned-64, _::binary>> = M.announce()
    check(kind == 9 and word == own, "the announcement does not open with the build version")

    # default_tolerant
    case M.announce_read(endpoint(remote), M.new()) do
      {:ok, v} -> check(v.announced and v.build_version == remote, "tolerant read: " <> inspect(v))
      other -> check(false, "the tolerant read refused another build version: " <> inspect(other))
    end

    # strict_same_version
    check(M.new().strict == false, "a new vocabulary is strict")
    case M.announce_read(endpoint(own), %%{M.new() | strict: true}) do
      {:ok, v} -> check(v.announced and v.remote == nil, "strict same-version read: " <> inspect(v))
      other -> check(false, "the strict read refused its own build version: " <> inspect(other))
    end

    # strict_other_version, typed refusal
    refused =
      case M.announce_read(endpoint(remote), %%{M.new() | strict: true}) do
        {:error, :build_version_mismatch, v} ->
          check(v.remote == remote and v.own == own, "the refusal does not carry both versions")
          check(not v.announced and v.count == 0 and v.entries == [], "the strict refusal set a vocabulary")
          check(v.build_version == remote, "the peer's build version was not kept")
          v

        other ->
          check(false, "strict other-version read: " <> inspect(other))
          %%{M.new() | refused: true}
      end

    # terminal
    case M.announce_read(endpoint(own), refused) do
      {:error, :second_announcement, _} -> :ok
      other -> check(false, "after a strict refusal: " <> inspect(other))
    end

    case M.load_messages(refused, <<2, 0>>) do
      {:error, :no_vocabulary} -> :ok
      other -> check(false, "a batch on the refused connection: " <> inspect(other))
    end
  end
end

Probe.run()
if Process.get(:failed), do: System.halt(1)
`, own)
	return runElixirModuleProbe(t, module, probe)
}
