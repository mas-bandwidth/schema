package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func luauDamageRead(t *testing.T, file, sabotage string) string {
	t.Helper()
	units, _ := readManifest(t)
	dir, mods, files := buildMessageUnit(t, units["tblr1"])
	if sabotage != "" {
		src := string(files["schema_load.luau"])
		var patched string
		if sabotage == "final" {
			const anchor = "\tif report.malformed then return nil end\n\treturn v\nend\n"
			if strings.Count(src, anchor) != 1 {
				t.Fatal("final discard anchor not unique")
			}
			patched = strings.Replace(src, anchor, "\treturn v\nend\n", 1)
		} else {
			const anchor = "if walk.at ~= o.e then report.malformed = true; return nil end"
			if strings.Count(src, anchor) != 1 {
				t.Fatal("early discard anchor not unique")
			}
			patched = strings.Replace(src, anchor, "if walk.at ~= o.e then report.malformed = true; return v end", 1)
		}
		if err := os.WriteFile(filepath.Join(dir, "schema_load.luau"), []byte(patched), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	b.WriteString("local L = require(\"./schema_load\")\nlocal R = require(\"./schema_retain\")\nlocal inputs = require(\"./inputs\")\n")
	for _, mod := range mods {
		fmt.Fprintf(&b, "require(%q)\n", "./"+mod)
	}
	b.WriteString(`for _, retain in {false, true} do
 local report = {malformed=false, refused=0, unknown=0, kindMismatch=0, clamped=0, widened=0, duplicate=0}
 local value = if retain then R.LoadRetainTolerant("Cfg", inputs.wire, report) else L.loadTolerant("Cfg", inputs.wire, report)
 print(tostring(value == nil) .. " " .. tostring(report.malformed))
end
`)
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", file+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire})))
}

func TestLuauDamageDiscardsFileAndRetainingLoads(t *testing.T) {
	slowtest.Gate(t, "Luau (it runs the emitted readers)")
	t.Parallel()
	for _, file := range []string{"frame_slack_after_terminator", "nested_terminator_early"} {
		t.Run(file, func(t *testing.T) {
			if got := luauDamageRead(t, file, ""); got != "true true\ntrue true" {
				t.Fatalf("damage exposed a value: %s", got)
			}
		})
	}
}

func TestLuauDamageDiscardNegativeControls(t *testing.T) {
	slowtest.Gate(t, "Luau (it runs the emitted readers)")
	t.Parallel()
	for _, row := range []struct{ file, sabotage string }{{"frame_slack_after_terminator", "early"}, {"nested_terminator_early", "final"}} {
		t.Run(row.sabotage, func(t *testing.T) {
			if got := luauDamageRead(t, row.file, ""); got != "true true\ntrue true" {
				t.Fatalf("control needs a clean refusal: %s", got)
			}
			if got := luauDamageRead(t, row.file, row.sabotage); got != "false true\nfalse true" {
				t.Fatalf("runtime sabotage did not expose the damaged values: %s", got)
			}
		})
	}
	t.Run("message batch", func(t *testing.T) {
		units, conns, _ := messageManifest(t)
		conn := conns["backend_conn"]
		dir, mods, files := buildMessageUnit(t, units[conn.unit])
		ann, err := os.ReadFile(filepath.Join(root, conn.announcement))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/backend_batch_damaged_second_message.bin"))
		if err != nil {
			t.Fatal(err)
		}
		probe := strings.Replace(messageProbe(mods, "LoginRequest", "LoadMessagesTolerant"), "if count < 0 then return end", "if report.malformed then print(\"bodies \" .. tostring(#bodies)) return end\nif count < 0 then return end", 1)
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0644); err != nil {
			t.Fatal(err)
		}
		run := func() string {
			return strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"ann": ann, "batch": wire})))
		}
		if got := run(); got != "count 0 malformed true\nbodies 0" {
			t.Fatalf("control needs whole-batch refusal: %s", got)
		}
		src := string(files["schema_message.luau"])
		const anchor = "if malformed then\n\t\treport.malformed = true\n\t\treturn 0, {}\n"
		if strings.Count(src, anchor) != 1 {
			t.Fatal("batch discard anchor not unique")
		}
		if err := os.WriteFile(filepath.Join(dir, "schema_message.luau"), []byte(strings.Replace(src, anchor, "if malformed then\n\t\treport.malformed = true\n\t\treturn got, bodies\n", 1)), 0644); err != nil {
			t.Fatal(err)
		}
		if got := run(); got != "count 1 malformed true\nbodies 1" {
			t.Fatalf("runtime sabotage did not expose the partial batch: %s", got)
		}
	})
}
