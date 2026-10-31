package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const squirrelFileDiscard = `  if (report.malformed) {
    // The strict witness distinguishes body damage from an unopened file,
    // without requiring this tolerant API to hand it a partial record.
    local frame = ::getstackinfos(2);
    if (frame != null && ("_strictFrame" in frame.locals)) frame.locals.store.walkDamaged = true;
    return null;
  }
`
const squirrelBatchDiscard = `  if(walked!=count || r.nonFinite!=null) {
    for(local i=0;i<count;i++)storage[i]=d.make();
    if(r.nonFinite!=null)return refuse(report,"non_finite_float: "+r.nonFinite);
    return 0;
  }
`

func squirrelDamageProbe(t *testing.T, patchFile, anchor string) string {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "messages", loadUnit(t, root, []string{"tables/messages"}))
	back := emitUnit(t, dir, "backend", loadUnit(t, root, []string{"tables/backend"}))
	if patchFile != "" {
		path := filepath.Join(dir, patchFile)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(data), anchor) != 1 {
			t.Fatalf("discard anchor in %s must occur once", patchFile)
		}
		writeFile(t, path, []byte(strings.Replace(string(data), anchor, "", 1)))
	}
	program := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local S=dofile("messages/schema_module.nut",true); local T=S.require(%q); local L=S.require("schema_table_load");
local fr={unknown=0,kindMismatch=0,widened=0,clamped=0,duplicate=0,malformed=false,refused=false};local value=L.loadTolerant(T,"ToolMessage",I["bad"],fr);
print(fr.malformed && value==null ? "file discarded\n" : "file kept\n");
local B=dofile("backend/schema_module.nut",true).require(%q); local v={};local ar={};B.AnnounceRead(v,I["ann"],ar);
local storage=[B.Envelope(),B.Envelope(),B.Envelope()];local mr={};local n=B.EnvelopeLoadMessages(storage,v,I["wire"],mr);
print(mr.malformed && n==0 && storage[0].Payload.Type==0 && storage[1].Payload.Type==0 ? "batch discarded\n" : "batch kept\n");
local tailValues=[B.LoginRequest()];local previous=tailValues[0];local tr={};local delivered=B.LoginRequestLoadMessages(tailValues,v,I["tail"],tr);
print(tr.malformed && delivered==0 && tailValues[0]!=previous ? "tail discarded\n" : "tail kept\n");`, mod, back)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(program))
	bad, err := hex.DecodeString(unionElemResetMutant)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"bad": bad}
	for key, name := range map[string]string{"ann": "backend_conn.bin", "wire": "backend_batch_damaged_second_message.bin", "tail": "login_full_message.bin"} {
		inputs[key] = mustRead(t, filepath.Join(root, "testdata/wire/tables", name))
		if key == "tail" {
			inputs[key] = append(inputs[key], 0)
		}
	}
	return strings.Join(strings.Fields(string(sqrun.Run(t, dir, "main.nut", inputs))), " ")
}

func TestSquirrelDamageDiscardsValues(t *testing.T) {
	if got := squirrelDamageProbe(t, "", ""); got != "file discarded batch discarded tail discarded" {
		t.Fatalf("damaged read exposed a value: %s", got)
	}
}
func TestSquirrelDamageDiscardNegativeControl(t *testing.T) {
	for _, c := range []struct{ file, anchor, want string }{
		{"messages/schema_table_load.nut", squirrelFileDiscard, "file kept batch discarded tail discarded"},
		{"backend/schema_table_message.nut", squirrelBatchDiscard, "file discarded batch kept tail kept"},
	} {
		t.Run(c.file, func(t *testing.T) {
			if got := squirrelDamageProbe(t, c.file, c.anchor); got != c.want {
				t.Fatalf("discard sabotage: got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSquirrelDelegatedDamageDiscardsValue(t *testing.T) {
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "messages", loadUnit(t, root, []string{"tables/messages"}))
	data, err := hex.DecodeString(unionElemResetMutant)
	if err != nil {
		t.Fatal(err)
	}
	requireSquirrelDiscardWithDelegate(t, dir, "messages", mod, "ToolMessage", data, true)
}
