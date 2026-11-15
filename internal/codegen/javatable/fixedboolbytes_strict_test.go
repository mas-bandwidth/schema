package javatable

// TestFixedBoolBytesStrict pins the fixed form's bool and present bytes to the
// strict read (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3 step 11): a byte other
// than 0 or 1 refuses the whole load, at the top level, in a nested type, behind
// a union arm, in an array element, and in every record of the batch. The
// verdict is the contract; a refusal's name is each leg's own diagnostic. The
// probe writes a lawful two-record file with the leg's own writer and each
// forged copy differs in exactly one byte.

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fixedBoolBytesSchema = `package strictbool

type Pay { n int32 = 5 }

type ArmTrue { on bool }
type ArmNum { n int32 = 9 }

union Switch
{
    active ArmTrue
    count  ArmNum
}

type Nest { on bool }

fixed table Root
{
    flag bool
    link ?Pay
    sw   Switch
    nest Nest
    arr  [2]bool
}
`

const fixedBoolBytesSaveProbe = `import java.nio.file.*;
import strictbool.*;

public final class Probe_save {
    public static void main(String[] args) throws Exception {
        RootFixed.Value[] vals = { new RootFixed.Value(), new RootFixed.Value() };
        for (RootFixed.Value v : vals) {
            v.flag = true;
            v.linkPresent = true;
            v.link.n = 5;
            v.sw.type = SwitchFixed.active;
            v.sw.active.on = true;
            v.nest.on = true;
            v.arr[0] = true;
            v.arr[1] = false;
        }
        byte[] buf = new byte[RootFixed.measure(2)];
        RootFixed.save(vals, 2, buf);
        Files.write(Paths.get(args[0]), buf);
        System.out.println("SAVED " + buf.length);
    }
}
`

// fixedBoolBytesLayout answers the probe schema's body byte offsets: the scalar
// bool, the optional's present byte, the bool inside the union's selected arm,
// the optional's payload, the nested bool, the bool array, and the body size.
func fixedBoolBytesLayout(t *testing.T) (flag, present, unionBool, payload, nested, array int64, body int) {
	t.Helper()
	u := unitFrom(t, fixedBoolBytesSchema)
	roots := ir.TableFixedRoots(u)
	if len(roots) != 1 {
		t.Fatalf("the probe schema has %d fixed roots, want one", len(roots))
	}
	off := int64(0)
	byName := map[string]int64{}
	fields := map[string]*ir.Field{}
	for _, f := range roots[0].Fields {
		byName[f.Name] = off
		fields[f.Name] = f
		off += ir.TableFixedFieldBytes(f)
	}
	body = int(off)
	get := func(name string) int64 {
		v, ok := byName[name]
		if !ok {
			t.Fatalf("the probe schema has no %s field", name)
		}
		return v
	}
	flag = get("flag")
	link := get("link")
	sw := get("sw")
	nested = get("nest")
	array = get("arr")
	un, ok := fields["sw"].Type.Ref.(*ir.Union)
	if !ok {
		t.Fatal("the probe schema's sw field is not a union")
	}
	payload = ir.TableFixedFieldBytes(fields["link"]) - 1
	return flag, link, sw + ir.TableFixedUnionTagBytes(un), payload, nested, array, body
}

func TestFixedBoolBytesStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "functional test: invokes the JDK (javac/java) and runs a compiled Java fixed-form probe")
	javac, javaBin := javaToolsRequired(t)

	files, err := Generate(unitFrom(t, fixedBoolBytesSchema))
	if err != nil {
		t.Fatalf("generate the probe schema: %v", err)
	}
	dir := t.TempDir()
	writeFixedJava(t, dir, "strictbool", files)
	if err := os.WriteFile(filepath.Join(dir, "Probe_save.java"), []byte(fixedBoolBytesSaveProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Probe_load.java"), []byte(probeSource("Probe_load", "strictbool", "Root", false)), 0o644); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(dir, "strictbool", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	probes, err := filepath.Glob(filepath.Join(dir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, probes...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	cleanPath := filepath.Join(t.TempDir(), "clean.bin")
	if out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Probe_save", cleanPath).CombinedOutput(); err != nil {
		t.Fatalf("Probe_save: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(cleanPath)
	if err != nil {
		t.Fatal(err)
	}

	flagOff, presentOff, unionBoolOff, payloadBytes, nestedOff, arrayOff, body := fixedBoolBytesLayout(t)
	layoutLength := int64(binary.LittleEndian.Uint32(raw[16:20]))
	recordsAt := int64(20) + layoutLength
	recordBytes := int64(8 + body)
	body0 := recordsAt + 8
	secondFlag := body0 + recordBytes + flagOff
	if int64(len(raw)) != recordsAt+2*recordBytes {
		t.Fatalf("the saved file is %d bytes, not two records of %d from %d", len(raw), recordBytes, recordsAt)
	}

	load := func(t *testing.T, data []byte) probeRun {
		t.Helper()
		path := filepath.Join(t.TempDir(), "probe.bin")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Probe_load", path).CombinedOutput()
		if err != nil {
			t.Fatalf("Probe_load: %v\n%s", err, out)
		}
		return parseProbe(t, string(out))
	}

	// Bytes 0 and 1 both read. The first copy carries the writer's 1s; the
	// second zeroes every bool, the present byte and the payload behind it.
	if got := load(t, raw); got.n != 2 || got.refused || got.damaged {
		t.Fatalf("the lawful file must read both records: %+v", got)
	}
	zero := append([]byte(nil), raw...)
	for _, at := range []int64{
		body0 + flagOff, body0 + presentOff, body0 + unionBoolOff,
		body0 + nestedOff, body0 + arrayOff, body0 + arrayOff + 1,
	} {
		zero[at] = 0
	}
	for i := int64(0); i < payloadBytes; i++ {
		zero[body0+presentOff+1+i] = 0
	}
	if got := load(t, zero); got.n != 2 || got.refused || got.damaged {
		t.Fatalf("0 bools and a 0 present byte must read: %+v", got)
	}

	for _, tc := range []struct {
		name string
		at   int64
		b    byte
	}{
		{"a bool byte of 2", body0 + flagOff, 0x02},
		{"a bool byte of 0xff", body0 + flagOff, 0xff},
		{"a present byte of 2", body0 + presentOff, 0x02},
		{"a present byte of 0xff", body0 + presentOff, 0xff},
		{"a bool in a nested type of 2", body0 + nestedOff, 0x02},
		{"a bool array element of 2", body0 + arrayOff, 0x02},
		{"a bool under a union arm of 2", body0 + unionBoolOff, 0x02},
		{"a bool under a union arm of 0xff", body0 + unionBoolOff, 0xff},
		{"a second record's bool of 2", secondFlag, 0x02},
	} {
		forged := append([]byte(nil), raw...)
		forged[tc.at] = tc.b
		got := load(t, forged)
		if got.n != -1 || !got.refused {
			t.Errorf("%s must refuse the whole load: %+v", tc.name, got)
		}
	}
}
