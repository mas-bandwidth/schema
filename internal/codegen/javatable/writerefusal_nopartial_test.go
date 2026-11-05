package javatable

// A REFUSAL WRITES NOTHING (docs/SPEC.md §5.1), and A COUNT OR A LENGTH PAST
// ITS STORAGE IS REFUSED IN EVERY BUILD. Run WITHOUT -ea, the release shape,
// the fixed form's save, the message form's saveMessages and the packet
// writer answer -1 for a value whose count or length would index past its own
// storage (and the two table writers for a NaN), and leave the caller's buffer
// exactly as it was: every probe fills the buffer with a poison byte, saves a
// refused value, and requires every byte of it still poison. A table batch's
// refused value is its SECOND body, behind a first body that would write, and
// every fault sits behind a field of its own that would write too, so a writer
// that stores as it walks is caught.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const noPartialSchemaJava = `package probe

type Item
{
    n int32
}

type Bag
{
    first int32
    tag   string(4)
    items [..3]Item
}

fixed table Root
{
    first int32
    name  string(4)
    items [..3]Item
    ratio float32
}
`

const noPartialDriverJava = `import probe.*;

public final class Driver {
    static byte[] poisoned(int n) {
        byte[] b = new byte[n];
        java.util.Arrays.fill(b, (byte) 0xA5);
        return b;
    }

    static boolean intact(byte[] b) {
        for (byte x : b) {
            if (x != (byte) 0xA5) return false;
        }
        return true;
    }

    static void check(boolean ok, String what) {
        if (!ok) throw new IllegalStateException(what);
    }

    static RootFixed.Value good() {
        RootFixed.Value v = new RootFixed.Value();
        v.first = 5;
        v.name[0] = 0x68;
        v.name[1] = 0x69;
        v.nameLength = 2;
        v.itemsCount = 1;
        v.items[0].n = 9;
        v.ratio = 0.5f;
        return v;
    }

    interface Fault {
        void apply(RootFixed.Value v);
    }

    public static void main(String[] args) {
        boolean ea = false;
        assert ea = true;
        check(!ea, "the probe must run without -ea");
        RootFixed.Value[] fine = {good(), good()};
        int need = RootFixed.measure(2);
        check(RootFixed.save(fine, 2, new byte[need]) == need, "the fixed save of a lawful batch did not write");
        int msgNeed = RootMessage.measureMessages(fine, null);
        check(msgNeed > 2 && RootMessage.saveMessages(fine, new byte[msgNeed], null) == msgNeed,
                "saveMessages of a lawful batch did not write");

        String[] names = {"a count past its storage", "a length past its storage", "a NaN",
                "a negative count", "a negative length"};
        Fault[] faults = {
            v -> v.itemsCount = 4, v -> v.nameLength = 5, v -> v.ratio = Float.NaN,
            v -> v.itemsCount = -1, v -> v.nameLength = -1,
        };
        for (int i = 0; i < faults.length; i++) {
            RootFixed.Value[] batch = {good(), good()};
            faults[i].apply(batch[1]);
            byte[] bytes = poisoned(need + 64);
            check(RootFixed.save(batch, 2, bytes) == -1, "the fixed save wrote " + names[i]);
            check(intact(bytes), "the fixed save refused " + names[i] + " after storing bytes");
            // the message form writes a non-positive count or length as an
            // absent field and indexes nothing: the first three are its own
            if (i < 3) {
                byte[] msg = poisoned(msgNeed + 64);
                check(RootMessage.saveMessages(batch, msg, null) == -1, "saveMessages wrote " + names[i]);
                check(intact(msg), "saveMessages refused " + names[i] + " after storing bytes");
            }
        }

        Probe.Bag bag = new Probe.Bag();
        bag.first = 5;
        bag.itemsCount = 1;
        byte[] packet = new byte[Probe.BAG_MAX_BYTES];
        check(Probe.writeBag(bag, packet) > 0, "the packet writer did not write a lawful value");
        int[][] packetFaults = {{4, 0}, {-1, 0}, {1, 5}, {1, -1}};
        for (int[] f : packetFaults) {
            bag.itemsCount = f[0];
            bag.tagLength = f[1];
            byte[] bytes = poisoned(Probe.BAG_MAX_BYTES);
            check(Probe.writeBag(bag, bytes) == -1,
                    "the packet writer wrote count " + f[0] + " length " + f[1]);
            check(intact(bytes), "the packet writer refused count " + f[0] + " length " + f[1] + " after storing bytes");
        }
        System.out.println("ok");
    }
}
`

func TestWriteRefusalWritesNothingJava(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	u := unitFrom(t, noPartialSchemaJava)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if _, ok := files[name]; !ok {
			files[name] = data
		}
	}
	td := t.TempDir()
	classes := filepath.Join(td, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(td, filepath.Base(name))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := filepath.Join(td, "Driver.java")
	if err := os.WriteFile(driver, []byte(noPartialDriverJava), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-cp", classes, "Driver").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("a refused save stored bytes in the release shape: %v\n%s", err, out)
	}
}
