package cstable

// The public measure contract (docs/SPEC-TABLES.md §6.5): a measure of a wire
// it refuses answers -1 with the reason on the reason path, and never throws.
// The typed overloads convert the untyped measure's reason string; the corpus's
// shared-blob wire makes that string "blob_shared", which the emitted enum must
// hold like every other reason the measure assigns.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestCSharpTypedMeasureTotal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "blobs", "Assets.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := csTypedMeasureRun(t, files, shared)
	t.Logf("typed measure probe:\n%s", out)
	if err != nil || !strings.Contains(out, "TYPED_MEASURE_TOTAL_OK") {
		t.Fatalf("typed measure total:\n%s\n%v", out, err)
	}

	if err := csTypedMeasureReasons(files); err != nil {
		t.Fatal(err)
	}
}

func TestCSharpTypedMeasureTotalNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "blobs", "Assets.schema"))
	if err != nil {
		t.Fatal(err)
	}
	clean, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := csTypedMeasureRun(t, clean, shared); err != nil || !strings.Contains(out, "TYPED_MEASURE_TOTAL_OK") {
		t.Fatalf("unplanted tree:\n%s\n%v", out, err)
	}

	files := make(map[string][]byte, len(clean))
	for name, data := range clean {
		files[name] = append([]byte(nil), data...)
	}
	if err := csTypedMeasureRestoreThrow(files); err != nil {
		t.Fatal(err)
	}
	out, err := csTypedMeasureRun(t, files, shared)
	t.Logf("planted probe:\n%s", out)
	if err != nil || strings.Contains(out, "TYPED_MEASURE_TOTAL_OK") ||
		!strings.Contains(out, "typed-measure-total/two-out/refused: threw ArgumentException") ||
		!strings.Contains(out, "typed-measure-total/four-out/refused: threw ArgumentException") {
		t.Fatalf("planted artifact did not throw in both typed measure overloads:\n%s\n%v", out, err)
	}
}

// csTypedMeasureRestoreThrow puts the pre-fix artifact back: an enum without
// blob_shared, which makes the emitted Enum.Parse conversion throw.
func csTypedMeasureRestoreThrow(files map[string][]byte) error {
	restored := 0
	for name, data := range files {
		s := string(data)
		const member = ", blob_shared"
		if !strings.Contains(s, "public enum TableRefuseReason {") || !strings.Contains(s, member) {
			continue
		}
		files[name] = []byte(strings.Replace(s, member, "", 1))
		restored++
	}
	if restored != 1 {
		return fmt.Errorf("restored the pre-fix enum in %d files, want 1", restored)
	}
	return nil
}

func csTypedMeasureRun(t *testing.T, files map[string][]byte, shared []byte) (string, error) {
	t.Helper()
	probe := fmt.Sprintf(csTypedMeasureProbeSrc, hex.EncodeToString(shared))
	return vtCellsCompileRun(t, files, probe)
}

const csTypedMeasureProbeSrc = `using System;
using Blobdemo;

public static class TypedMeasureProbe
{
    static int bad;

    static void Row(string what, bool ok, string why)
    {
        if (ok) { Console.WriteLine("ROW " + what + ": ok"); return; }
        bad++;
        Console.WriteLine("ROW " + what + ": " + why);
    }

    public static int Main()
    {
        byte[] shared = Convert.FromHexString("%s");

        long raw = Schema.CatalogLoadMeasure(shared);
        Row("typed-measure-total/untyped/refused", raw == -1, "untyped measure = " + raw);

        TableRefuseReason reason;
        try
        {
            long n = Schema.CatalogLoadMeasure(shared, out reason);
            Row("typed-measure-total/two-out/refused", n == -1, "measure = " + n);
        }
        catch (Exception e)
        {
            Row("typed-measure-total/two-out/refused", false, "threw " + e.GetType().Name + ": " + e.Message);
        }

        try
        {
            long n = Schema.CatalogLoadMeasure(shared, out long dataBytes, out long attributionBytes, out TableRefuseReason fourReason);
            Row("typed-measure-total/four-out/refused", n == -1, "measure = " + n);
        }
        catch (Exception e)
        {
            Row("typed-measure-total/four-out/refused", false, "threw " + e.GetType().Name + ": " + e.Message);
        }

        Catalog clean = new Catalog();
        clean.Thumb = new TableBlob(new byte[] { 1, 2, 3 });
        clean.Alias = new TableBlob(new byte[] { 4, 5 });
        clean.Note = new TableBlob(new byte[] { (byte)'n' });
        long want = Schema.CatalogMeasure(clean);
        if (want < 0)
        {
            Row("typed-measure-total/clean", false, "clean value refused");
        }
        else
        {
            byte[] wire = new byte[want];
            long wrote = Schema.CatalogSave(clean, wire);
            long untyped = Schema.CatalogLoadMeasure(wire);
            Row("typed-measure-total/clean/untyped", wrote == want && untyped > 0, "wrote=" + wrote + " untyped=" + untyped + " want=" + want);

            TableRefuseReason cleanReason;
            long two = Schema.CatalogLoadMeasure(wire, out cleanReason);
            Row("typed-measure-total/clean/two-out", two == untyped, "typed=" + two + " untyped=" + untyped);

            long four = Schema.CatalogLoadMeasure(wire, out long cleanData, out long cleanAttribution, out TableRefuseReason cleanFourReason);
            Row("typed-measure-total/clean/four-out", four == untyped, "typed=" + four + " untyped=" + untyped);
        }

        if (bad == 0) { Console.WriteLine("TYPED_MEASURE_TOTAL_OK"); }
        return 0;
    }
}
`

var (
	csMeasureReasonAssign = regexp.MustCompile(`"[^"]*"`)
	csMeasureEnumMembers  = regexp.MustCompile(`public enum TableRefuseReason \{([^}]*)\}`)
)

// csTypedMeasureReasons checks every reason string an emitted measure path can
// assign against the emitted TableRefuseReason members, unless the typed
// conversion has a non-throwing fallback.
func csTypedMeasureReasons(files map[string][]byte) error {
	members := map[string]bool{}
	var reasons []string
	fallback := false
	for _, data := range files {
		s := string(data)
		if m := csMeasureEnumMembers.FindStringSubmatch(s); m != nil {
			for member := range strings.SplitSeq(m[1], ",") {
				members[strings.TrimSpace(member)] = true
			}
		}
		for line := range strings.SplitSeq(s, "\n") {
			if strings.Contains(line, "reason ==") {
				continue
			}
			if i := strings.Index(line, "reason ="); i >= 0 && !strings.Contains(line, "reason ==") {
				for _, literal := range csMeasureReasonAssign.FindAllString(line[i:], -1) {
					reasons = append(reasons, strings.Trim(literal, `"`))
				}
			}
			if strings.Contains(line, "out string") && strings.Contains(line, "TableRefuseReason") &&
				strings.Contains(line, "TryParse<TableRefuseReason>") {
				fallback = true
			}
		}
	}
	if len(members) == 0 {
		return fmt.Errorf("emitted source has no TableRefuseReason enum")
	}
	if len(reasons) == 0 {
		return fmt.Errorf("emitted measure assigns no reason string; the check is looking at the wrong text")
	}
	var missing []string
	for _, reason := range reasons {
		if !members[reason] {
			missing = append(missing, reason)
		}
	}
	if len(missing) > 0 && !fallback {
		return fmt.Errorf("emitted measure assigns reason(s) %s that TableRefuseReason does not name and the conversion has no non-throwing fallback", strings.Join(missing, ", "))
	}
	return nil
}
