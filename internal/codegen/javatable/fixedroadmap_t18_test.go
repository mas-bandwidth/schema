package javatable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedCorpusHeader reads the form-3 framing, with no JDK or corpus needed
// (FIXED-FORM-ALGORITHM §2). The hash is the writer's claim, never recomputed.
func fixedCorpusHeader(data []byte) (uint64, []byte, error) {
	if len(data) < 20 || data[0] != 3 {
		return 0, nil, fmt.Errorf("fixed header: missing form-3 framing")
	}
	size := binary.LittleEndian.Uint32(data[16:20])
	if uint64(size) > uint64(len(data)-20) {
		return 0, nil, fmt.Errorf("fixed header: truncated layout")
	}
	return binary.LittleEndian.Uint64(data[8:16]), data[20 : 20+int(size)], nil
}

func TestFixedJavaT18ManifestAndHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	text := "file=plain.bin values=r0.x=7,r0.y=8,r0.text=\"hello world, friend\"\nfile=hostile.bin values=r0.x=7 forged=r0.x@44=99\nfile=refused.bin refused=wide_text\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	plain, err := manifestEntries(dir, "plain.bin", "values")
	if err != nil || plain["r0.x"] != "7" || plain["r0.y"] != "8" || plain["r0.text"] != `"hello world, friend"` {
		t.Fatalf("plain manifest: %v %v", plain, err)
	}
	forged, err := manifestEntries(dir, "hostile.bin", "forged")
	if err != nil || forged["r0.x@44"] != "99" {
		t.Fatalf("forged manifest: %v %v", forged, err)
	}
	if _, err := manifestEntries(dir, "refused.bin", "values"); err == nil || !strings.Contains(err.Error(), "refused=wide_text") {
		t.Fatalf("refusal was not named: %v", err)
	}
	layout := []byte{1, 2, 3, 4}
	header := make([]byte, 20+len(layout))
	header[0] = 3
	const hash = uint64(0x1234567812345678)
	binary.LittleEndian.PutUint64(header[8:16], hash)
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(layout)))
	copy(header[20:], layout)
	got, body, err := fixedCorpusHeader(header)
	if err != nil || got != hash || !bytes.Equal(body, layout) {
		t.Fatalf("header: %x %x %v", got, body, err)
	}
	header[8] ^= 1
	corrupt, _, err := fixedCorpusHeader(header)
	if err != nil || corrupt == hash {
		t.Fatalf("controlled hash corruption escaped comparison: %x %v", corrupt, err)
	}
	t.Log("controlled hash corruption caught")
}

// Required functional gates fail instead of silently accepting a missing JDK.
func t18RequireJava(t *testing.T) {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "1" {
		return
	}
	for _, tool := range []struct{ env, name string }{{"JAVAC", "javac"}, {"JAVA", "java"}} {
		path := os.Getenv(tool.env)
		if path == "" {
			var err error
			path, err = exec.LookPath(tool.name)
			if err != nil {
				t.Fatalf("required %s is unavailable: %v", tool.name, err)
			}
		}
		if err := exec.Command(path, "-version").Run(); err != nil {
			t.Fatalf("required %s is unavailable: %v", tool.name, err)
		}
	}
}

// The JVM's bounds checks provide the memory-safety half of the hostile-byte
// sweep (FIXED-FORM-ALGORITHM §7); no fourth answer may escape as an exception.
func t18P3(t *testing.T) {
	t.Helper()
	t18RequireJava(t)
	classes := t07BuildProbe(t, `package probe
fixed table T {
 a int32
 b int32
}
`, t18P3Probe)
	out := t07RunClass(t, classes, "Probe_p2")
	if !strings.Contains(out, "P3 refused=") {
		t.Fatalf("no sweep counts: %s", out)
	}
	t.Log(strings.TrimSpace(out))
}

const t18P3Probe = `import probe.*;
public final class Probe_p2 {
 public static void main(String[] args) {
  TFixed.Value v=new TFixed.Value();v.a=12345;v.b=-67890;
  byte[] good=new byte[TFixed.measure(1)];if(TFixed.save(new TFixed.Value[]{v},1,good)!=good.length)throw new AssertionError("save");
  int refused=0,malformed=0,landed=0;
  for(int at=0;at<good.length;at++)for(int bit=0;bit<8;bit++) {
   byte[] data=good.clone();data[at]^=(byte)(1<<bit);
   TableFixed.Report r=new TableFixed.Report();
   try {
    int count=TFixed.load(new TFixed.Value[]{new TFixed.Value()},1,data,TableFixed.plan(64),new short[64],TFixed.image(),r);
    if(r.refused){if(r.reason==TableFixed.Reason.none)throw new AssertionError("unnamed refusal at "+at+"/"+bit);refused++;}else if(r.malformed)malformed++;else if(count==1)landed++;
    else throw new AssertionError("fourth answer at "+at+"/"+bit+" count="+count);
   }catch(RuntimeException broken){throw new AssertionError("exception at "+at+"/"+bit,broken);}
  }
  if(refused==0||malformed==0||landed==0)throw new AssertionError("missing answer class: "+refused+"/"+malformed+"/"+landed);
  System.out.println("P3 refused="+refused+" malformed="+malformed+" landed="+landed);
 }
}`

// The probe deliberately feeds A's compiled destination plan to B's record
// and B's scatter. Replacing it with B's own plan must make this guard red
// (FIXED-FORM-ALGORITHM §7 item 4), which distinguishes a real blade from a
// comparison of differently named Java fields.
func t18P4(t *testing.T) {
	t.Helper()
	t18RequireJava(t)
	classes := t07BuildProbe(t, `package probe
fixed table A {
 p int64
}
fixed table B {
 x int32
 y int32
}
`, t18P4Probe)
	out := t07RunClass(t, classes, "Probe_p2")
	if !strings.Contains(out, "P4 wrong plan differs") {
		t.Fatalf("wrong-plan guard: %s", out)
	}
	t.Log(strings.TrimSpace(out))
}

const t18P4Probe = `import probe.*;
public final class Probe_p2 {
 public static void main(String[] args) {
  BFixed.Value b=new BFixed.Value();b.x=111;b.y=222;
  byte[] wire=new byte[BFixed.measure(1)];if(BFixed.save(new BFixed.Value[]{b},1,wire)!=wire.length)throw new AssertionError("save");
  TableFixed.Report report=new TableFixed.Report();
  TableFixed.Layout theirs=TableFixed.parse(BFixed.layout,0,BFixed.layout.length,report);
  TableFixed.Layout mine=TableFixed.parse(AFixed.layout,0,AFixed.layout.length,report);
  TableFixed.Entry[] wrong=TableFixed.plan(64);short[] remap=new short[64];
  int made=TableFixed.compile(theirs,mine,AFixed.dest,wrong,remap,report);
  if(made<0&&!report.refused)throw new AssertionError("plan compile gave no named answer");
  byte[] image=BFixed.image();
  TableFixed.Entry[] chosen=wrong;int count=made;
  if(!report.refused)TableFixed.run(chosen,count,remap,wire,BFixed.headerBytes+8,BFixed.bodyBytes,image,report);
  BFixed.Value back=new BFixed.Value();BFixed.scatter(image,0,back,report);
  if(!report.refused&&back.x==b.x&&back.y==b.y)throw new AssertionError("P4 wrong plan reproduced B");
  System.out.println("P4 wrong plan differs");
 }
}`

func TestFixedRoadmapJavaT18OracleHostile(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		run  func(*testing.T)
	}{{"java/W13", t18W13}, {"java/R27", t18R27}, {"java/P3", t18P3}, {"java/P4", t18P4}} {
		t.Run(test.name, func(t *testing.T) { t.Parallel(); test.run(t) })
	}
}

// Each row names its writer generation; forged and repeated rows retain the
// old writer's header (FIXED-FORM-ALGORITHM §2, §5.9 manifest oracle).
func t18Unit(t *testing.T, row map[string]string) *ir.Unit {
	t.Helper()
	name := strings.ToUpper(row["row"]) + ".schema"
	side := row["side"]
	if side != "none" {
		prefix := map[string]string{"old": "VOLD_", "new": "VNEW_", "mid": "VMID_", "a": "VBRA_", "b": "VBRB_", "hostile": "VOLD_", "past": "VOLD_", "many": "VOLD_"}[side]
		name = prefix + row["row"] + ".schema"
	}
	if row["row"] == "keyed" || row["row"] == "pack" {
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "examples", strings.Title(row["row"])+".schema"))
		if err != nil {
			t.Fatal(err)
		}
		return unitFrom(t, string(src))
	}
	return versioningSchemaUnit(t, name)
}

func t18W13(t *testing.T) {
	t.Helper()
	corpus := corpusDir(t)
	rows, err := manifestRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	compared, refused := 0, 0
	for _, row := range rows {
		u := t18Unit(t, row)
		files, err := Generate(u)
		root := u.Tables[row["root"]]
		if err != nil || root == nil || files[fixedClass(row["root"])+".java"] == nil {
			refused++
			t.Logf("W13 refused %s: %v", row["file"], err)
			continue
		}
		data, err := os.ReadFile(filepath.Join(corpus, row["file"]))
		if err != nil {
			t.Fatal(err)
		}
		hash, layout, err := fixedCorpusHeader(data)
		if err != nil {
			t.Fatal(err)
		}
		want := fixedLayoutBytes(fixedWalkRoot(root).entries)
		wantHash := ir.TableFixedLayoutHash(want, root)
		if hash != wantHash || !bytes.Equal(layout, want) {
			t.Errorf("W13 %s: header layout/hash differs: got %016x want %016x", row["file"], hash, wantHash)
		}
		t.Logf("W13 compared %s", row["file"])
		compared++
	}
	t.Logf("W13 compared=%d refused=%d", compared, refused)
	if compared == 0 {
		t.Fatal("W13 no rows compared against the C++ corpus")
	}
}

// R27 constructs assertions only from the manifest. Java reads the actual file
// through its generated codec; the bytes under test never provide an expected
// field value (FIXED-FORM-ALGORITHM §5.9 #32, #37).
func t18R27(t *testing.T) {
	t.Helper()
	corpus := corpusDir(t)
	rows, err := manifestRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	t18RequireJava(t)
	javac, _ := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	compared, refused := 0, 0
	for _, row := range rows {
		u := t18Unit(t, row)
		files, err := Generate(u)
		if err != nil || files[fixedClass(row["root"])+".java"] == nil {
			refused++
			t.Logf("R27 refused %s: %v", row["file"], err)
			continue
		}
		packet, err := javacodegen.Generate(u)
		if err != nil {
			t.Fatalf("R27 %s packet support: %v", row["file"], err)
		}
		for _, name := range []string{"Int128.java", "UInt128.java"} {
			if data := packet[name]; data != nil {
				files[name] = data
			}
		}
		writeFixedJava(t, src, u.Package, files)
		values, err := manifestEntries(corpus, row["file"], "values")
		if err != nil {
			t.Fatal(err)
		}
		if row["forged"] != "" {
			forged, err := manifestEntries(corpus, row["file"], "forged")
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range forged {
				key, _, _ = strings.Cut(key, "@")
				values[key] = value
			}
		}
		path, err := filepath.Abs(filepath.Join(corpus, row["file"]))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, " try { Object values=load(%q,%q,%s);\n", u.Package+"."+fixedClass(row["root"]), path, row["records"])
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts := strings.Split(key, ".")
			for i := 1; i < len(parts); i++ {
				name, suffix, ok := strings.Cut(parts[i], "[")
				parts[i] = javaName(name)
				if ok {
					parts[i] += "[" + suffix
				}
			}
			fmt.Fprintf(&body, " check(values,%q,%q,%q);\n", strings.Join(parts, "."), values[key], row["file"]+":"+key)
		}
		fmt.Fprintf(&body, " } catch (Exception | AssertionError bad) { failures++; System.err.println(%q + bad); }\n", row["file"]+": ")
		compared++
	}
	if compared == 0 {
		t.Fatal("R27 no rows compared against the manifest")
	}
	main := strings.Replace(t18ManifestProbe, "@@CHECKS@@", body.String(), 1)
	if err := os.WriteFile(filepath.Join(src, "Probe_p2.java"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, filepath.Join(src, "Probe_p2.java"))
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("R27 javac: %v\n%s", err, out)
	}
	t.Logf("R27 selected=%d refused=%d; actual value comparisons follow", compared, refused)
	t.Log(strings.TrimSpace(t07RunClass(t, classes, "Probe_p2")))
	t.Logf("R27 compared=%d refused=%d", compared, refused)
}

const t18ManifestProbe = `import java.lang.reflect.*;
import java.nio.file.*;
import java.math.*;
public final class Probe_p2 {
 static int checked,failures;
 static Object load(String name,String path,int count)throws Exception {
  Class<?> codec=Class.forName(name),value=Class.forName(name+"$Value");String pkg=name.substring(0,name.lastIndexOf('.'));
  Class<?> runtime=Class.forName(pkg+".TableFixed"),reportClass=Class.forName(pkg+".TableFixed$Report");
  Object report=reportClass.getConstructor().newInstance(),values=Array.newInstance(value,count);
  for(int i=0;i<count;i++)Array.set(values,i,value.getConstructor().newInstance());
  Method load=null;for(Method method:codec.getMethods())if(method.getName().equals("load"))load=method;
  Object plan=runtime.getMethod("plan",int.class).invoke(null,16384),image=codec.getMethod("image").invoke(null);
  int n=(Integer)load.invoke(null,values,count,Files.readAllBytes(Paths.get(path)),plan,new short[16384],image,report);
  if(n!=count||reportClass.getField("refused").getBoolean(report)||reportClass.getField("malformed").getBoolean(report))throw new AssertionError(path+": own-unit load refused or malformed, n="+n);
  return values;
 }
 static Object field(Object values,String path)throws Exception {
  String[] parts=path.split("\\.");Object at=Array.get(values,Integer.parseInt(parts[0].substring(1)));
  for(int i=1;i<parts.length;i++){String p=parts[i];int open=p.indexOf('[');String name=open<0?p:p.substring(0,open);at=at.getClass().getField(name).get(at);if(open>=0)at=Array.get(at,Integer.parseInt(p.substring(open+1,p.length()-1)));}
  return at;
 }
 static int[] units(String quoted) {
  java.util.ArrayList<Integer> out=new java.util.ArrayList<>();int start=quoted.startsWith("u\"")?2:1;
  for(int i=start;i<quoted.length()-1;i++){char c=quoted.charAt(i);if(c=='\\'){c=quoted.charAt(++i);if(c=='u'){out.add(Integer.parseInt(quoted.substring(i+1,i+5),16));i+=4;continue;}if(c=='n')c='\n';else if(c=='r')c='\r';else if(c=='t')c='\t';}out.add((int)c);}
  int[] values=new int[out.size()];for(int i=0;i<values.length;i++)values[i]=out.get(i);return values;
 }
 static void check(Object values,String path,String expected,String label)throws Exception {
  Object got=field(values,path);boolean ok=false;
  if(expected.startsWith("\"")||expected.startsWith("u\"")){int[] want=units(expected);ok=got!=null&&got.getClass().isArray()&&Array.getLength(got)>=want.length;for(int i=0;ok&&i<want.length;i++){Object v=Array.get(got,i);int n=v instanceof Character?(Character)v:((Number)v).intValue()&255;ok=n==want[i];}}
  else if(expected.contains("|0x")){String hex=expected.substring(expected.indexOf("|0x")+3);long bits=Long.parseUnsignedLong(hex,16);ok=got instanceof Float?Integer.toUnsignedLong(Float.floatToRawIntBits((Float)got))==bits:got instanceof Double&&Double.doubleToRawLongBits((Double)got)==bits;}
  else {if(expected.contains("|raw="))expected=expected.substring(expected.indexOf("|raw=")+5);BigInteger want=new BigInteger(expected),have;
   if(got instanceof Boolean)have=(Boolean)got?BigInteger.ONE:BigInteger.ZERO;
   else if(got instanceof Number){long n=((Number)got).longValue();have=BigInteger.valueOf(n);if(want.signum()>=0&&n<0){int width=got instanceof Byte?8:got instanceof Short?16:got instanceof Integer?32:64;have=have.add(BigInteger.ONE.shiftLeft(width));}}
   else {long hi=got.getClass().getField("hi").getLong(got),lo=got.getClass().getField("lo").getLong(got);have=BigInteger.valueOf(hi).shiftLeft(64).add(new BigInteger(Long.toUnsignedString(lo)));if(got.getClass().getSimpleName().equals("UInt128")&&hi<0)have=have.add(BigInteger.ONE.shiftLeft(128));}
   ok=have.equals(want);
  }
  if(!ok){failures++;System.err.println("R27 "+label+" expected="+expected+" got="+got);}checked++;
 }
 public static void main(String[] args)throws Exception {
@@CHECKS@@
  System.out.println("R27 manifest values compared="+checked+" failures="+failures);
  if(failures!=0)throw new AssertionError("R27 manifest mismatches="+failures);
 }
}`
