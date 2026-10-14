package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func runVariableProbe(t *testing.T, u *ir.Unit, driver string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	files["Driver.java"] = []byte(driver)
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	if out, err := exec.Command(java, "-ea", "-cp", dir, u.Package+".Driver").CombinedOutput(); err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}

func TestJavaVariableMapDamageAndRetention(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\ntable Root {\n entries map[string(8)]int32\n after int32\n}\n")
	runVariableProbe(t, u, `package probe;
public final class Driver {
 static void check(boolean ok,String message){if(!ok)throw new AssertionError(message);}
 static byte[] frame(String[] keys,boolean unknown,boolean slack,boolean incompatibleRepeat){
  TableVariable.Writer w=new TableVariable.Writer(new byte[4096],new TableVariable.Ids(64)),body=w.sub(new byte[2048]);
  w.put8(1);w.putLeb(w.ref(RootVariable.F_entries));w.put8(14);body.put8(13);body.putLeb(keys.length);
  for(int i=0;i<keys.length;i++){
   TableVariable.Writer e=w.sub(new byte[256]);byte[] key=keys[i].getBytes(java.nio.charset.StandardCharsets.UTF_8);
   e.putLeb(e.ref(RootEntriesEntryVariable.F_key));e.put8(12);e.putLeb(key.length);e.putBytes(key,0,key.length);
   e.putLeb(e.ref(RootEntriesEntryVariable.F_value));e.put8(4);e.put32(i+1);
   if(unknown&&i==0){e.putLeb(e.ref(12345));e.put8(4);e.put32(77);}
   e.put8(0);if(slack&&i==1)e.put8(99);body.putLeb(e.off);body.putBytes(e.buf,0,e.off);
  }
  w.putLeb(body.off);w.putBytes(body.buf,0,body.off);
  if(incompatibleRepeat){w.putLeb(w.ref(RootVariable.F_entries));w.put8(14);w.putLeb(3);w.put8(6);w.putLeb(1);w.put8(5);}
  w.putLeb(w.ref(RootVariable.F_after));w.put8(4);w.put32(17);w.put8(0);w.trailer();
  return java.util.Arrays.copyOf(w.buf,w.off);
 }
 public static void main(String[] args){
  for(int which=0;which<2;which++){
   TableVariable.Report report=new TableVariable.Report();
   RootVariable.Value v=RootVariable.load(frame(which==0?new String[]{"a","b"}:new String[]{"b","a"},false,which==0,false),report);
   check(v!=null&&report.malformed&&!report.refused&&v.entries.size()==1&&v.after==17,"slack or descending key must retain only ascending prefix and continue parent");
  }
  TableRetain retain=new TableRetain(4096,128);TableVariable.Report report=new TableVariable.Report();
  RootVariable.Value v=RootRetain.loadRetain(frame(new String[]{"a","a"},true,false,false),retain,report);
  check(v!=null&&!report.malformed&&report.duplicate==1&&report.unknown==1&&report.retained==1&&retain.used==0&&v.entries.size()==1&&v.entries.get(0).value==2,"duplicate replaces whole entry and its retained tail");
  check(java.util.Arrays.equals(RootVariable.save(v),RootRetain.saveRetain(v,retain,report)),"duplicate old unknown must not reappear");
  retain=new TableRetain(4096,128);report=new TableVariable.Report();
  v=RootRetain.loadRetain(frame(new String[]{"a"},true,false,true),retain,report);
  check(v!=null&&!report.malformed&&report.kindMismatch==1&&report.unknown==1&&retain.used>0&&v.entries.size()==1&&v.entries.get(0).value==1&&v.after==17,"incompatible repeat preserves prior value and retained subtree");
  byte[] saved=RootRetain.saveRetain(v,retain,report);TableVariable.Report again=new TableVariable.Report();RootVariable.Value loaded=RootVariable.load(saved,again);
  check(loaded!=null&&again.unknown==1&&loaded.entries.get(0).value==1,"skipped repeat keeps unknown on resave");
  report=new TableVariable.Report();v=RootVariable.fromJson("{\"entries\":{\"a\":7,\"a\":8},\"after\":17}".getBytes(java.nio.charset.StandardCharsets.UTF_8),report);
  check(v!=null&&!report.malformed&&report.duplicate==1&&v.entries.size()==1&&v.entries.get(0).value==8,"JSON duplicates are last-wins");
  System.out.println("map slack, descending prefix, duplicate replacement, skipped repeat retention and JSON duplicates pass");
 }
}`)
}
