package elixirtable

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestElixirJsonCommentIsWellFormedUTF8 holds the Elixir leg's JSON read to
// docs/SPEC-TABLES.md §16.2's comment text rule: the bytes inside a comment,
// line or block, are well-formed UTF-8 and hold no NUL, so a comment holding an
// ill-formed sequence or a NUL refuses the read. Every vector under
// testdata/wire/json-comment-text-rules/ is read at tables/examples' root
// ProfileConfig, and so is each comment payload at a placed position and inside
// an unknown key's value. Each row is compared on the verdict alone (read or
// refused) with the compiler's own engine, internal/tabletext, after the engine
// is first held to the manifest's verdict.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestElixirJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "examples", "Tables.schema"))
	model := tabletext.NewModel(u)
	st := model.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("tables/examples has no table ProfileConfig")
	}
	engineReads := func(text []byte) bool {
		var report tabletext.Report
		ok := model.Read(model.New(st), text, &report)
		return ok && !report.Refused
	}

	type row struct {
		name string
		text []byte
		read bool
	}
	var rows []row

	dir := filepath.Join(root, "testdata", "wire", "json-comment-text-rules")
	manifest, err := os.Open(filepath.Join(dir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 || (cols[1] != "read" && cols[1] != "refused") {
			t.Fatalf("bad manifest line %q", line)
		}
		text, err := os.ReadFile(filepath.Join(dir, cols[0]))
		if err != nil {
			t.Fatal(err)
		}
		want := cols[1] == "read"
		if got := engineReads(text); got != want {
			t.Fatalf("the engine reads %s as read=%t, the manifest says %s", cols[0], got, cols[1])
		}
		rows = append(rows, row{cols[0], text, want})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no vectors")
	}

	payloads := []struct{ name, bytes string }{
		{"utf8", "café 日本 \U0001F600"},
		{"lone-continuation", "a\x80b"},
		{"overlong", "a\xc0\xafb"},
		{"truncated-three-byte", "a\xe2\x82"},
		{"encoded-surrogate", "a\xed\xa0\x80b"},
		{"nul", "a\x00b"},
	}
	for _, p := range payloads {
		placed := map[string]string{
			"placed-line":   "{\"name\": // " + p.bytes + "\n\"a\"}\n",
			"placed-block":  "{\"name\": /* " + p.bytes + " */ \"a\"}\n",
			"unknown-line":  "{\"name\": \"a\", \"zz_unknown\": [1, // " + p.bytes + "\n2]}\n",
			"unknown-block": "{\"name\": \"a\", \"zz_unknown\": {\"k\": /* " + p.bytes + " */ 2}}\n",
		}
		for _, where := range []string{"placed-line", "placed-block", "unknown-line", "unknown-block"} {
			text := []byte(placed[where])
			want := p.name == "utf8"
			if got := engineReads(text); got != want {
				t.Fatalf("the engine reads %s %s as read=%t, the rule says read=%t", where, p.name, got, want)
			}
			rows = append(rows, row{where + "-" + p.name, text, want})
		}
	}

	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def row(text, read, name) do
  got =
   try do
    case Tabledemo.Table.from_json("ProfileConfig", text) do
     {:ok, _value, _report} -> :read
     {:error, _reason, _report} -> :refused
     other -> {:unexpected, other}
    end
   rescue
    e -> {:raised, e}
   end
  want = if read, do: :read, else: :refused
  if got != want do
   IO.puts("FAIL " <> name <> ": want " <> inspect(want) <> ", got " <> inspect(got))
   Process.put(:failures, Process.get(:failures, 0) + 1)
  end
 end
end
`)
	for _, r := range rows {
		fmt.Fprintf(&probe, "Probe.row(Base.decode16!(%q, case: :mixed), %t, %q)\n", hex.EncodeToString(r.text), r.read, r.name)
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"json comment verdicts differ from the engine\")\nIO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBin, generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("json comment rows: %v\n%s", err, out)
	}
}
