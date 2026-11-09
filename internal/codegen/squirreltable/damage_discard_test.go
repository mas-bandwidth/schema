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

const squirrelFileDiscard = `  if (report.bodyDamaged) {
    // The strict witness distinguishes body damage from an unopened file,
    // without requiring this walk API to hand it a partial record.
    local frame = ::getstackinfos(2);
    if (frame != null && ("_strictFrame" in frame.locals)) frame.locals.store.walkDamaged = true;
    return null;
  }
`
const squirrelBatchDiscard = `  for(local i=0;i<r.written;i++)storage[i]=d.make();
`

func squirrelDamageProbe(t *testing.T, patchFile, anchor string) string {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	msgUnit := loadUnit(t, root, []string{"tables/messages"})
	msgUnit.Tables["ToolMessage"].FixedDeclared = false
	mod := emitUnit(t, dir, "messages", msgUnit)
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
		patched := strings.Replace(string(data), anchor, "", 1)
		if strings.HasSuffix(patchFile, "schema_table_load.nut") {
			// the wire-alone check refuses the same damage after the walk:
			// the control takes both defences out
			patched = withoutWireCheck(t, patched)
		}
		writeFile(t, path, []byte(patched))
	}
	program := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local S=dofile("messages/schema_module.nut",true); local T=S.require(%q); local L=S.require("schema_table_load");
local fr={unknown=0,mismatch=0,widened=0,oor=0,repeated=0,bodyDamaged=false,refused=false};local value=L.loadWalk(T,"ToolMessage",I["bad"],fr);
print(fr.bodyDamaged && value==null ? "file discarded\n" : "file kept\n");
local B=dofile("backend/schema_module.nut",true).require(%q); local v={};local ar={};B.AnnounceRead(v,I["ann"],ar);
local storage=[B.Envelope(),B.Envelope(),B.Envelope()];local mr={};local n=B.EnvelopeLoadMessages(storage,v,I["wire"],mr);
print(mr.bodyDamaged && n==0 && storage[0].Payload.Type==0 && storage[1].Payload.Type==0 ? "batch discarded\n" : "batch kept\n");
local tailValues=[B.LoginRequest()];local tr={};local delivered=B.LoginRequestLoadMessages(tailValues,v,I["tail"],tr);
print(tr.bodyDamaged && delivered==0 && tailValues[0].PlayerId==0 && tailValues[0].ClientBuild==0 ? "tail discarded\n" : "tail kept\n");`, mod, back)
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
		// the damaged batch's first body carries no payload, so its row reads
		// the same reset or kept; the tail batch's fully read body shows it
		{"backend/schema_table_message.nut", squirrelBatchDiscard, "file discarded batch discarded tail kept"},
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
	msgUnit := loadUnit(t, root, []string{"tables/messages"})
	msgUnit.Tables["ToolMessage"].FixedDeclared = false
	mod := emitUnit(t, dir, "messages", msgUnit)
	data, err := hex.DecodeString(unionElemResetMutant)
	if err != nil {
		t.Fatal(err)
	}
	requireSquirrelDiscardWithDelegate(t, dir, "messages", mod, "ToolMessage", data, true)
}
