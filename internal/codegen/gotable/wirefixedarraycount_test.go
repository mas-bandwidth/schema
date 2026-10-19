package gotable

import "testing"

// TestGoWireFixedArrayShortCount pins the standard's fixed-array rule
// (docs/SPEC-TABLES.md §4, §6.5): a count short of a [N]T field's own N is a
// damaged count. The tolerant read keeps the elements the writer sent and
// leaves the tail at defaults but counts malformed naming the field; the
// strict read refuses it as malformed. A count above N keeps clamped, and a
// [..N]T field's short count keeps the silent evolution tolerance.
func TestGoWireFixedArrayShortCount(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
enum Difficulty { Easy, Normal, Hard }
fixed table GlobalSettings
{
    tick_rate    uint32 = 60         | min = 1, max = 240
    difficulty   Difficulty = Normal
    build_note   string(48)
    spawn_delays [3]float32
    budget       [..3]float32
}
`, `package probe
import("bytes";"errors";"testing")
func shortCount(t *testing.T, wire []byte, count byte) []byte {
 t.Helper()
 out:=append([]byte(nil),wire...)
 pat:=[]byte{0x0e,0x0e,0x0a,0x03}
 i:=bytes.Index(out,pat)
 if i<0 || bytes.Index(out[i+len(pat):],pat)>=0 {t.Fatalf("fixed array header not found once: %x",wire)}
 out[i+3]=count
 return out
}
func TestFixedArrayShortCount(t *testing.T){
 sent:=[3]float32{1.5,2.5,3.5}
 v:=GlobalSettings{TickRate:60,Difficulty:1,SpawnDelays:sent}
 wire:=make([]byte,GlobalSettingsMeasure(&v))
 if GlobalSettingsSave(&v,wire)!=int64(len(wire)){t.Fatal("save")}
 var clean GlobalSettings
 var rep TableReport
 if !GlobalSettingsLoad(&clean,wire,&rep)||rep!=(TableReport{}){t.Fatalf("clean load %+v",rep)}
 if clean.SpawnDelays!=sent{t.Fatalf("clean value %v",clean.SpawnDelays)}
 again:=make([]byte,len(wire))
 if GlobalSettingsSave(&clean,again)!=int64(len(wire))||!bytes.Equal(again,wire){t.Fatal("clean wire did not re-encode")}
 for _,c:=range []byte{0,1,2}{
  bad:=shortCount(t,wire,c)
  var val GlobalSettings
  rep=TableReport{}
  if !GlobalSettingsLoadTolerant(&val,bad,&rep){t.Fatalf("count %d: tolerant read refused",c)}
  want:=[3]float32{}
  for i:=range int(c){want[i]=sent[i]}
  if val.SpawnDelays!=want{t.Fatalf("count %d: value %v, want %v",c,val.SpawnDelays,want)}
  if !rep.Malformed||rep.Reason!="malformed: spawn_delays"{t.Fatalf("count %d: report %+v",c,rep)}
  if _,err:=GlobalSettingsReadStrict(bad);!errors.Is(err,TableRefuseMalformed){t.Fatalf("count %d: strict err %v",c,err)}
 }
 bad:=shortCount(t,wire,4)
 var val GlobalSettings
 rep=TableReport{}
 if !GlobalSettingsLoadTolerant(&val,bad,&rep){t.Fatal("count 4: tolerant read refused")}
 if val.SpawnDelays!=sent||rep.Malformed||rep.Clamped!=1||rep.Reason!=""{t.Fatalf("count 4: %v %+v",val.SpawnDelays,rep)}
 if _,err:=GlobalSettingsReadStrict(bad);!errors.Is(err,TableRefuseClamped){t.Fatalf("count 4: strict err %v",err)}
 bv:=GlobalSettings{TickRate:60,Difficulty:1,BudgetCount:2,Budget:[3]float32{4.5,5.5,0}}
 bw:=make([]byte,GlobalSettingsMeasure(&bv))
 if GlobalSettingsSave(&bv,bw)!=int64(len(bw)){t.Fatal("bounded save")}
 if !bytes.Contains(bw,[]byte{0x0e,0x0a,0x0a,0x02}){t.Fatalf("bounded header: %x",bw)}
 var bval GlobalSettings
 rep=TableReport{}
 if !GlobalSettingsLoadTolerant(&bval,bw,&rep){t.Fatal("bounded load")}
 if bval.BudgetCount!=2||bval.Budget!=[3]float32{4.5,5.5,0}||rep!=(TableReport{}){t.Fatalf("bounded short count: count %d report %+v",bval.BudgetCount,rep)}
}
`)
}
