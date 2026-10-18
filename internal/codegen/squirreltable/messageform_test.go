package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type squirrelMessageConnection struct{ unit, file string }
type squirrelMessageRow struct{ name, connection, root, file, batch string }

func readMessageRows(t *testing.T, root string) (map[string]squirrelMessageConnection, []squirrelMessageRow) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]squirrelMessageConnection{}
	var rows []squirrelMessageRow
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "connection":
			if len(f) >= 5 {
				connections[f[1]] = squirrelMessageConnection{unit: f[2], file: f[4]}
			}
		case "message":
			if len(f) >= 6 {
				rows = append(rows, squirrelMessageRow{name: f[1], connection: f[2], root: f[3], file: f[4], batch: f[5]})
			}
		}
	}
	return connections, rows
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func squirrelMessageFiles(t *testing.T, u *ir.Unit, patch map[string][]byte) map[string][]byte {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range tables {
		if replacement, ok := patch[name]; ok {
			body = replacement
		}
		files[name] = body
	}
	return files
}

func runSquirrelMessages(t *testing.T, root string, units map[string]*ir.Unit, connections map[string]squirrelMessageConnection, rows []squirrelMessageRow, negative bool) []byte {
	t.Helper()
	dir := t.TempDir()
	patch := map[string][]byte{}
	if negative {
		runtime := squirreltable.Runtime()
		const name = "schema_table_message.nut"
		body := string(runtime[name])
		const anchor = "local count = (bytes[1]&0xff)+1;"
		if strings.Count(body, anchor) != 1 {
			t.Fatalf("negative control anchor occurs %d times, want exactly once", strings.Count(body, anchor))
		}
		patch[name] = []byte(strings.Replace(body, anchor, "local count = (bytes[1]&0xff);", 1))
	}
	inputs := map[string][]byte{}
	var script strings.Builder
	script.WriteString("local I=dofile(\"inputs.nut\",true);\n")
	script.WriteString("local same=function(a,b){if(a.len()!=b.len())return false;for(local i=0;i<a.len();i++)if(a[i]!=b[i])return false;return true;};\n")
	generated := map[string]bool{}
	for _, row := range rows {
		conn, ok := connections[row.connection]
		if !ok {
			t.Fatalf("message %s has no connection %s", row.name, row.connection)
		}
		u := units[conn.unit]
		if u == nil {
			t.Fatalf("connection %s has no unit %s", row.connection, conn.unit)
		}
		sub := filepath.Join(dir, conn.unit)
		if !generated[conn.unit] {
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, b := range squirrelMessageFiles(t, u, patch) {
				if strings.Contains(name, "/") {
					continue
				}
				writeFile(t, filepath.Join(sub, name), b)
			}
			generated[conn.unit] = true
		}
		inputs[row.name+"_ann"] = mustRead(t, filepath.Join(root, conn.file))
		inputs[row.name+"_batch"] = mustRead(t, filepath.Join(root, row.batch))
		module := squirreltable.ModuleName(u)
		fmt.Fprintf(&script, "{ local T=dofile(%q,true).require(%q); local v={}; local ar={};\n", conn.unit+"/schema_module.nut", module)
		fmt.Fprintf(&script, " if(!T.AnnounceRead(v,I[%q],ar)){print(%q+\":announce:\"+ar.reason+\"\\n\");} else {\n", row.name+"_ann", row.name)
		batch := inputs[row.name+"_batch"]
		count := int(batch[1]) + 1
		constructors := make([]string, count)
		for i := range constructors {
			constructors[i] = "T." + row.root + "()"
		}
		guard, closing := "if(n>=0){\n", " } } }"
		if st := u.Tables[row.root]; st != nil && st.FixedDeclared {
			guard, closing = "", " } }"
		}
		fmt.Fprintf(&script, " local out=[%s]; local rr={}; local n=T.%sLoadMessages(out,v,I[%q],rr); if(n!=%d) print(%q+\":load:\"+n+\":\"+rr.reason+\":\"+rr.malformed+\"\\n\"); %s\n", strings.Join(constructors, ","), row.root, row.name+"_batch", count, row.name, guard)
		fmt.Fprintf(&script, " local mr={}; local size=T.%sMeasureMessages(out,mr); local b=blob(size); local sr={}; local got=T.%sSaveMessages(out,b,sr); if(got!=size || !same(b,I[%q])) print(%q+\":save:\"+got+\":\"+size+\"\\n\"); else print(%q+\":ok\\n\");%s\n", row.root, row.root, row.name+"_batch", row.name, row.name, closing)
	}
	if units["backenddemo"] != nil {
		inputs["backend_ann"] = mustRead(t, filepath.Join(root, connections["backend_conn"].file))
		inputs["damaged"] = mustRead(t, filepath.Join(root, "testdata/wire/tables/backend_batch_damaged_second_message.bin"))
		script.WriteString(`{ local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables"); local v={}; local ar={}; T.AnnounceRead(v,I["backend_ann"],ar); local storage=[T.Envelope(),T.Envelope(),T.Envelope()]; local rr={}; local n=T.EnvelopeLoadMessages(storage,v,I["damaged"],rr); if(n==1 && rr.malformed) { print("damage:ok\n"); } else { print("damage:bad:"+n+"\n"); } }
`)
	}
	script.WriteString("print(\"done\\n\");\n")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	return sqrun.Run(t, dir, "main.nut", inputs)
}

func loadMessageUnits(t *testing.T, root string, connections map[string]squirrelMessageConnection) map[string]*ir.Unit {
	t.Helper()
	paths := map[string][]string{}
	for _, u := range manifestUnits(t, filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt")) {
		paths[u.key] = u.paths
	}
	units := map[string]*ir.Unit{}
	for _, c := range connections {
		if units[c.unit] == nil {
			units[c.unit] = loadUnit(t, root, paths[c.unit])
		}
	}
	return units
}

func TestSquirrelMessageFormMatchesTheGoldens(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	connections, rows := readMessageRows(t, root)
	if len(rows) != 13 {
		t.Fatalf("manifest has %d message rows, want 13", len(rows))
	}
	out := string(runSquirrelMessages(t, root, loadMessageUnits(t, root, connections), connections, rows, false))
	for _, row := range rows {
		if row.root == "Scene" {
			if !strings.Contains(out, row.name+":load:-1:message_form_plain_table:Scene:false") {
				t.Errorf("%s plain table refusal missing: %s", row.name, out)
			}
			continue
		}
		if !strings.Contains(out, row.name+":ok") {
			t.Errorf("%s failed message round trip: %s", row.name, out)
		}
	}
	if !strings.Contains(out, "damage:ok") {
		t.Errorf("damage in second body was not terminal for the batch: %s", out)
	}
	if !strings.Contains(out, "done") {
		t.Fatal("probe did not finish")
	}
	t.Logf("%d manifest message rows inspected: 12 fixed-root byte comparisons, 1 named plain-table refusal", len(rows))
}

func TestSquirrelMessageFormNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]squirrelMessageConnection{"backend_conn": {unit: "backenddemo", file: "testdata/wire/tables/backend_conn.bin"}}
	units := loadMessageUnits(t, root, connections)
	rows := []squirrelMessageRow{{name: "login_full", connection: "backend_conn", root: "LoginRequest", batch: "testdata/wire/tables/login_full_message.bin"}}
	out := string(runSquirrelMessages(t, root, units, connections, rows, true))
	if !strings.Contains(out, "login_full:load:-1:") || !strings.Contains(out, "login_full:save:") {
		t.Fatalf("one-short batch-count negative control did not make load fail and byte comparison red: %s", out)
	}
}
