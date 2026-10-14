package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJavaVariableWideValues(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\ntype Line { text wstring(4) }\ntable Root {\n title wstring(7)\n line Line\n lines [..3]Line\n after int32\n}\n")
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	rep := new(tabletext.Report)
	if !model.Read(value, []byte(`{"title":"A😀中","line":{"text":"é"},"lines":[{"text":"𐀀"}],"after":17}`), rep) {
		t.Fatalf("oracle: %+v", rep)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	text, err := model.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, src, 0644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	for name, data := range map[string][]byte{"wire": wire, "json": append(text, '\n')} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	driver := `package probe;
public final class Driver {
 static void check(boolean ok, String message) { if (!ok) throw new AssertionError(message); }
 static byte[] frame(byte[] payload) {
  TableVariable.Writer w = new TableVariable.Writer(new byte[1024],new TableVariable.Ids(64));
  w.put8(1);w.putLeb(w.ref(RootVariable.F_title));w.put8(33);w.putLeb(payload.length);w.putBytes(payload,0,payload.length);
  w.putLeb(w.ref(RootVariable.F_after));w.put8(4);w.put32(17);w.put8(0);w.trailer();
  return java.util.Arrays.copyOf(w.buf,w.off);
 }
 public static void main(String[] args) throws Exception {
  byte[] wire=java.nio.file.Files.readAllBytes(java.nio.file.Path.of(args[0],"wire"));
  byte[] json=java.nio.file.Files.readAllBytes(java.nio.file.Path.of(args[0],"json"));
  TableVariable.Report report=new TableVariable.Report();
  RootVariable.Value v=RootVariable.load(wire,report);
  check(v!=null&&!report.malformed&&!report.refused&&report.unknown==0&&report.kindMismatch==0&&report.clamped==0&&report.widened==0,"reference report");
  check(java.util.Arrays.equals(wire,RootVariable.save(v)),"canonical wire");
  check(java.util.Arrays.equals(json,RootVariable.toJson(v)),"canonical JSON expected="+new String(json,java.nio.charset.StandardCharsets.UTF_8)+" actual="+new String(RootVariable.toJson(v),java.nio.charset.StandardCharsets.UTF_8));
  v=RootVariable.fromJson(json,new TableVariable.Report());
  check(java.util.Arrays.equals(wire,RootVariable.save(v)),"JSON input");
  for(byte[] damage:new byte[][]{{1},{0,0},{0,(byte)0xd8},{0,(byte)0xdc}}) {
   report=new TableVariable.Report();v=RootVariable.load(frame(damage),report);
   check(v!=null&&report.malformed&&!report.refused&&v.title.length==0&&v.after==17,"damaged text and parent continuation");
  }
  // Seven-unit bound cuts between the high and low surrogate at slots 6/7.
  report=new TableVariable.Report();v=RootVariable.load(frame(new byte[]{65,0,66,0,67,0,68,0,69,0,70,0,0,(byte)0xd8,0,(byte)0xdc}),report);
  check(v.title.length==6&&report.clamped==1&&!report.malformed&&v.after==17,"surrogate boundary clamp");
  v.title=new char[]{0xd800};check(RootVariable.save(v)==null,"writer rejects unpaired surrogate");
  System.out.println("wide reference wire/JSON, malformed defaults, parent continuation, pair clamp and writer validation pass");
 }
}`
	p := filepath.Join(dir, "Driver.java")
	if err := os.WriteFile(p, []byte(driver), 0644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, p)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	if out, err := exec.Command(java, "-ea", "-cp", dir, "probe.Driver", dir).CombinedOutput(); err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
