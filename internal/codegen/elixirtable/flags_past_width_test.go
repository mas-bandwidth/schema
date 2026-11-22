package elixirtable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElixirFlagsPastWidthRefuses holds a flags mask to its reader's width W
// (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the newer
// reader reads old data, and never the other way. A newer build's variable
// file with its appended bit set, a narrower unsigned field widened into the
// mask with a bit past W, and a fixed record with a stored bit past W each
// refuse the whole load out_of_range, and a newer build's message batch with
// the appended bit refuses; the twins inside W read. The variable and fixed
// saves refuse a mask past W in every build, so a same-build round trip never
// writes what its own reader refuses. Under the old rule (a mask's range was
// the u64's own) every refusal here read.
func TestElixirFlagsPastWidthRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	const reader = "package flagspast\nflags Cap { Trace, Verbose }\ntable Root { cap Cap }\nfixed table Fx { cap Cap }\n"
	const newer = "package flagspast\nflags Cap { Trace, Verbose, Extra }\ntable Root { cap Cap }\nfixed table Fx { cap Cap }\n"
	const narrow = "package flagspast\ntable Root { cap uint8 }\n"
	place := func(u *ir.Unit, root, text string) (*tabletext.Model, *tabletext.Instance) {
		t.Helper()
		m := tabletext.NewModel(u)
		inst := m.New(m.Lookup(root))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep.Refused {
			t.Fatalf("the Go engine does not read %s: %+v", text, rep)
		}
		return m, inst
	}
	must := func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	newerUnit, narrowUnit, readerUnit := unitFrom(t, newer), unitFrom(t, narrow), unitFrom(t, reader)
	variable := func(u *ir.Unit, text string) string {
		return elixirBitsWire(must(tablewire.Encode(place(u, "Root", text))))
	}
	messages := func(text string) string {
		m, inst := place(newerUnit, "Fx", text)
		return elixirBitsWire(must(tablewire.EncodeMessages(m, []*tabletext.Instance{inst})))
	}
	fixed := func(text string) []byte { return must(tablewire.EncodeFixed(place(readerUnit, "Fx", text))) }
	low, both := fixed(`{"cap":["Trace"]}`), fixed(`{"cap":["Trace","Verbose"]}`)
	at := -1
	for i := range low {
		if i < len(both) && low[i] != both[i] {
			if at >= 0 {
				t.Fatalf("more than one mask byte between %x and %x", low, both)
			}
			at = i
		}
	}
	if at < 0 || len(low) != len(both) {
		t.Fatalf("no mask byte between %x and %x", low, both)
	}
	forged := bytes.Clone(low)
	forged[at] |= 0b100
	probe := strings.NewReplacer(
		"PAST_WIRE", variable(newerUnit, `{"cap":["Trace","Extra"]}`),
		"INSIDE_WIRE", variable(newerUnit, `{"cap":["Trace","Verbose"]}`),
		"WIDE_PAST", variable(narrowUnit, `{"cap":5}`),
		"WIDE_INSIDE", variable(narrowUnit, `{"cap":3}`),
		"FIXED_PAST", elixirBitsWire(forged),
		"FIXED_INSIDE", elixirBitsWire(both),
		"ANNOUNCEMENT", elixirBitsWire(tablewire.Announce(newerUnit)),
		"MESSAGE_PAST", messages(`{"cap":["Trace","Extra"]}`),
		"MESSAGE_INSIDE", messages(`{"cap":["Trace","Verbose"]}`),
	).Replace(flagsPastWidthProbeSource)
	if out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, readerUnit), probe); err != nil || !strings.HasSuffix(strings.TrimSpace(out), "ok") {
		t.Fatalf("flags past-width probe: %v\n%s", err, out)
	}
}

const flagsPastWidthProbeSource = `
defmodule FlagsProbe do
  alias Flagspast.Table, as: T
  alias Flagspast.TableMessage, as: M

  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def reads(root, wire, label) do
    result = T.load(root, wire)
    check(match?({:ok, %{"cap" => 3}, _}, result), label <> ": got " <> inspect(result))
    {:ok, value, _} = result
    value
  end

  def refused(root, wire, label) do
    result = T.load(root, wire)
    check(match?({:error, :out_of_range, _}, result), label <> ": got " <> inspect(result))
  end

  def save_refused(root, value, label) do
    result =
      try do
        T.save(root, value)
      rescue
        e in ArgumentError -> {:raised, e}
      end

    check(not match?({:ok, _}, result), label <> ": wrote " <> inspect(result))
  end

  def message(wire, ok, label) do
    {:ok, announced} = M.announce_read(ANNOUNCEMENT)
    {:ok, vocab} = M.for_root(announced, "Fx")
    result = M.load_messages(vocab, wire, 2)
    want = if ok, do: match?({:ok, [_]}, result), else: result == {:malformed, 0, []}
    check(want, label <> ": got " <> inspect(result))
  end
end

FlagsProbe.refused("Root", PAST_WIRE, "a newer build's appended bit")
FlagsProbe.refused("Root", WIDE_PAST, "a widened uint8 with a bit past W")
FlagsProbe.refused("Fx", FIXED_PAST, "a fixed record with a bit past W")
FlagsProbe.message(MESSAGE_PAST, false, "a newer build's message with the appended bit")
FlagsProbe.message(MESSAGE_INSIDE, true, "a newer build's message inside W")
FlagsProbe.reads("Root", WIDE_INSIDE, "a widened uint8 inside W")
variable = FlagsProbe.reads("Root", INSIDE_WIRE, "a newer build's file inside W")
fixed = FlagsProbe.reads("Fx", FIXED_INSIDE, "a fixed record inside W")
FlagsProbe.save_refused("Root", Map.put(variable, "cap", 5), "the variable save past W")
FlagsProbe.save_refused("Fx", Map.put(fixed, "cap", 5), "the fixed save past W")
IO.puts("ok")
`
