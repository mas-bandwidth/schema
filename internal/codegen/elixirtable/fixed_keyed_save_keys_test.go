package elixirtable

import (
	"strings"
	"testing"
)

func TestElixirFixedKeyedSaveKeyForms(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package fixedkeyforms

enum Slot { HTTPReady, Fallback }

fixed table Root { slots [Slot]int32 }
`)
	out := runCompleteUnitProbe(t, u, `
alias Fixedkeyforms.Table, as: T

string_slots = %{"HTTPReady" => 7, "Fallback" => 9}
{:ok, bytes} = T.save("Root", %{"slots" => string_slots})
{:ok, ^bytes} = T.save("Root", %{"slots" => %{http_ready: 7, fallback: 9}})
{:ok, ^bytes} = T.save("Root", %{"slots" => Map.put(string_slots, :http_ready, 99)})
{:ok, %{"slots" => ^string_slots}, _report} = T.load("Root", bytes)
{:error, :invalid_value} = T.save("Root", %{"slots" => %{"HTTPReady" => 7}})
IO.puts("FIXED_KEYED_SAVE_KEYS_OK")
`)
	if !strings.Contains(out, "FIXED_KEYED_SAVE_KEYS_OK") {
		t.Fatal(out)
	}
}
