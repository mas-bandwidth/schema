package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A fixed array whose bound grew since the writer (docs/SPEC-TABLES.md §21.1,
// "a grown bound") reads in the message form as the writer's elements
// followed by one fresh default element per new slot. Each row has the Go
// engine write `[2]T` and Luau read it as `[4]T` and save it again; the bytes
// must be the engine's own read of the same batch, re-encoded. Before the fix
// Luau refused the batch as clamped: the writer's count sat below the reader's
// fixed bound, which it took for a counted array's min (F-092).

type luauWidenBoundKind struct {
	name     string
	decls    string
	elem     string
	two      string
	optional bool
}

var luauWidenBoundKinds = []luauWidenBoundKind{
	{name: "bool", elem: "bool", two: `[true,true]`},
	{name: "uint8", elem: "uint8", two: `[255,6]`},
	{name: "int32", elem: "int32", two: `[-5,6]`},
	{name: "uint64", elem: "uint64", two: `["18446744073709551615","6"]`},
	{name: "float32", elem: "float32", two: `[1.5,-2.25]`},
	{name: "float64", elem: "float64", two: `[1.5,-2.25]`},
	{name: "enum", decls: "enum Mode {\n Idle\n Live\n Done\n}\n", elem: "Mode", two: `["Live","Done"]`},
	{name: "flags", decls: "flags Caps {\n Read\n Write\n}\n", elem: "Caps", two: `[["Read"],["Read","Write"]]`},
	{name: "record", decls: "fixed table Inner {\n v int32 = 7\n s string(8) = \"born\"\n}\n", elem: "Inner", two: `[{"v":1,"s":"a"},{"v":2,"s":"b"}]`},
	{name: "union", decls: "fixed table Inner {\n v int32\n}\nunion Choice {\n count int32\n nested Inner\n}\n", elem: "Choice", two: `[{"count":5},{"nested":{"v":3}}]`},
	{name: "optional-int32", elem: "int32", two: `[-5,6]`, optional: true},
	{name: "optional-enum", decls: "enum Mode {\n Idle\n Live\n Done\n}\n", elem: "Mode", two: `["Live","Done"]`, optional: true},
}

func luauWidenBoundUnit(t *testing.T, k luauWidenBoundKind, bound int) *ir.Unit {
	t.Helper()
	opt := ""
	if k.optional {
		opt = "?"
	}
	src := fmt.Sprintf("package widenbound\n%sfixed table Root {\n items %s[%d]%s\n}\n", k.decls, opt, bound, k.elem)
	path := filepath.Join(t.TempDir(), "WidenBound.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLuauMessageWidenedFixedArrayBoundPadsOneDefaultPerSlot(t *testing.T) {
	t.Parallel()
	for _, k := range luauWidenBoundKinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			older, newer := luauWidenBoundUnit(t, k, 2), luauWidenBoundUnit(t, k, 4)
			writer, reader := tabletext.NewModel(older), tabletext.NewModel(newer)

			root := writer.New(writer.Lookup("Root"))
			var rep tabletext.Report
			if !writer.Read(root, []byte(`{"items":`+k.two+`}`), &rep) || rep.Refused {
				t.Fatalf("fixture %s: %+v", k.two, rep)
			}
			peer := tablewire.Announce(older)
			batch, err := tablewire.EncodeMessages(writer, []*tabletext.Instance{root})
			if err != nil {
				t.Fatal(err)
			}

			// THE REFERENCE: the Go engine reads the writer's batch under the
			// reader's declaration and re-encodes it.
			var vocab tablewire.Vocabulary
			if err := vocab.AnnounceRead(peer, &rep); err != nil {
				t.Fatal(err)
			}
			read := reader.New(reader.Lookup("Root"))
			if _, ok, err := tablewire.DecodeMessages(reader, []*tabletext.Instance{read}, batch, &vocab, &rep); !ok || err != nil {
				t.Fatalf("the engine did not read across the grown bound: %v %+v", err, rep)
			}
			want, err := tablewire.EncodeMessages(reader, []*tabletext.Instance{read})
			if err != nil {
				t.Fatal(err)
			}

			files, err := luau.Generate(newer)
			if err != nil {
				t.Fatal(err)
			}
			tables, err := luautable.Generate(newer)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, tables)
			dir := t.TempDir()
			var mods []string
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(name, "Table.luau") {
					mods = append(mods, strings.TrimSuffix(name, ".luau"))
				}
			}
			sort.Strings(mods)
			out := messageMain(t, dir, mods, "Root", peer, batch)
			lines := strings.Split(out, "\n")
			if len(lines) < 2 || lines[0] != "count 1 malformed false" {
				t.Fatalf("LoadMessages across the grown bound: %q", out)
			}
			got, err := hex.DecodeString(lines[len(lines)-1])
			if err != nil {
				t.Fatalf("the probe printed %q: %v", out, err)
			}
			if string(got) != string(want) {
				t.Fatalf("the re-saved batch is not the engine's:\n got %x\nwant %x", got, want)
			}
		})
	}
}
