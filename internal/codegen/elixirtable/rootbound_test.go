package elixirtable

// A MESSAGE READ IS BOUND TO ITS ROOT (docs/SPEC-TABLES.md §3.3). The batch
// verbs act on a vocabulary bound to one fixed root by for_root/2, or through
// the per-root <root>_load_messages, <root>_save_messages and
// <root>_measure_messages; the module's own load_messages, save_messages and
// measure_messages answer {:error, :no_root} on a vocabulary no root is bound
// to, and no unbound raw verb is exported. Under the unit's own build the
// bound write refuses a body out of declaration order, a field the root does
// not declare, and a field at the default the writer elides.
//
// RED FIRST. The module carried no for_root/2 and no per-root verbs, and
// load_messages read any body against the vocabulary alone.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestElixirMessageReadIsRootBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package rootbound

fixed table Alpha
{
    a int32
    b int32
    c string(8)
    d Delta
}

type Delta
{
    y int32
}

fixed table Beta
{
    x int32
}

table Var
{
    n int32
}
`)
	slots := tablewire.MessageVocabularySlots(u)
	ref := func(table, field string) uint64 {
		st := u.Tables[table]
		if st == nil {
			st = u.Structs[table]
		}
		for _, f := range st.Fields {
			if f.Name == field {
				if s, ok := slots[ir.TableFieldEntry(f).Key()]; ok {
					return s
				}
			}
		}
		t.Fatalf("%s.%s has no message vocabulary slot", table, field)
		return 0
	}
	a, b, c, x := ref("Alpha", "a"), ref("Alpha", "b"), ref("Alpha", "c"), ref("Beta", "x")
	d, y := ref("Alpha", "d"), ref("Delta", "y")

	// the Go engine's canonical batch of one Alpha body
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Alpha"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"a":1,"b":2,"c":"hi","d":{"y":3}}`), &rep) || !rep.Silent() {
		t.Fatalf("source: %+v", rep)
	}
	batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "batch.bin"), batch, 0o600); err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`    {:ok, vocab} = M.announce_read(M.announce())
    batch = File.read!(golden("batch.bin"))
    canonical = [[{%[1]d, 1}, {%[2]d, 2}, {%[3]d, "hi"}, {%[4]d, [{%[5]d, 3}]}]]

    # for_root binds each fixed root, and refuses any other name
    {:ok, alpha} = M.for_root(vocab, "Alpha")
    {:ok, beta} = M.for_root(vocab, "Beta")
    check(alpha.root == "Alpha" and beta.root == "Beta", "for_root did not bind the root")
    check(M.for_root(vocab, "Gamma") == {:error, :unknown_root}, "for_root bound an unknown name")
    check(M.for_root(vocab, "Var") == {:error, :unknown_root}, "for_root bound a variable table")

    # the root-less verbs answer :no_root
    check(M.load_messages(vocab, batch) == {:error, :no_root}, "the root-less load read")
    check(M.load_messages(vocab, batch, 1) == {:error, :no_root}, "the root-less load/3 read")
    check(M.save_messages(vocab, canonical) == {:error, :no_root}, "the root-less save wrote")
    check(M.measure_messages(vocab, canonical) == {:error, :no_root}, "the root-less measure measured")

    # no unbound raw verb is exported
    for {f, n} <- [raw_load_messages: 2, raw_load_messages: 3, raw_save_messages: 2, raw_measure_messages: 2] do
      check(not function_exported?(M, f, n), "#{f}/#{n} is exported")
    end

    # a canonical batch reads through the bound root and re-saves byte for byte
    check(M.alpha_load_messages(vocab, batch) == {:ok, canonical}, "alpha_load_messages did not read the canonical batch")
    check(M.load_messages(alpha, batch) == {:ok, canonical}, "the bound load did not read the canonical batch")
    check(M.alpha_save_messages(vocab, canonical) == {:ok, batch}, "alpha_save_messages did not re-save byte for byte")
    check(M.save_messages(alpha, canonical) == {:ok, batch}, "the bound save did not re-save byte for byte")
    check(M.alpha_measure_messages(vocab, canonical) == {:ok, byte_size(batch)}, "alpha_measure_messages differs from the batch")

    # the batch read under the other root names fields Beta does not declare
    check(match?({:malformed, 0, _}, M.beta_load_messages(vocab, batch)), "beta_load_messages read Alpha's body")

    # under another build version those undeclared fields, nested body included,
    # are stepped over and omitted; the declared fields still read
    other = %%{vocab | build_version: bxor(vocab.build_version, 1)}
    check(M.beta_load_messages(other, batch) == {:ok, [[]]}, "other build: beta_load_messages kept an undeclared field")
    check(M.alpha_load_messages(other, batch) == {:ok, canonical}, "other build: alpha_load_messages did not read the declared fields")

    # the bound save refuses out of order, undeclared and at-default bodies
    for {what, bad} <- [
          {"out of order", [[{%[2]d, 2}, {%[1]d, 1}]]},
          {"undeclared", [[{%[1]d, 1}, {%[6]d, 5}]]},
          {"at default", [[{%[1]d, 0}, {%[2]d, 2}]]},
          {"text at default", [[{%[1]d, 1}, {%[3]d, ""}]]}
        ] do
      check(M.save_messages(alpha, bad) == {:error, :invalid_value}, "the bound save wrote a body #{what}")
      check(M.alpha_save_messages(vocab, bad) == {:error, :invalid_value}, "alpha_save_messages wrote a body #{what}")
      check(M.alpha_measure_messages(vocab, bad) == {:error, :invalid_value}, "alpha_measure_messages measured a body #{what}")
    end
`, a, b, c, d, y, x)

	out, err := runMessageProbe(t, u, filepath.Join(dir, "announce.bin"), body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
