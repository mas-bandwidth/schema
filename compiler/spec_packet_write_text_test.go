package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecPacketWriteChecksText(t *testing.T) {
	t.Parallel()
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	section := func(page, start, end string) string {
		t.Helper()
		a := strings.Index(page, start)
		if a < 0 {
			t.Fatalf("missing %q", start)
		}
		b := strings.Index(page[a+len(start):], end)
		if b < 0 {
			t.Fatalf("missing %q", end)
		}
		return page[a : a+len(start)+b]
	}
	spec := read("../docs/SPEC.md")
	algorithm := read("../docs/PACKET-WIRE-ALGORITHM.md")
	usage := read("../docs/USAGE.md")
	for name, page := range map[string]string{"SPEC": spec, "algorithm": algorithm, "USAGE": usage} {
		if strings.Contains(page, "every build in every leg") {
			t.Errorf("%s uses an unqualified every-build phrase", name)
		}
	}
	trust := section(spec, "## 5. ", "### 5.1 ")
	for _, stale := range []string{
		"Text well-formedness is not one of them",
		"Every\nwrite-side contract\nasserts in DEBUG ONLY",
	} {
		if strings.Contains(trust, stale) {
			t.Errorf("§5 retains %q", stale)
		}
	}
	for _, want := range []string{"row 8", "zero byte", "well-formed UTF-8", "debug-only assert", "refusal in every build"} {
		if !strings.Contains(trust, want) {
			t.Errorf("§5 missing %q", want)
		}
	}
	write := section(spec, "### 5.1 ", "## 6. ")
	if !strings.Contains(write, "packet `Write` (rows 1–8 and 10)") {
		t.Error("§5.1 packet column does not cover rows 1–8 and 10")
	}
	field := section(algorithm, "### 2.4 ", "### 2.5 ")
	if !strings.Contains(field, "`string`: check the used bytes for both 0x00 and well-formed UTF-8") {
		t.Error("§2.4 string writer does not check both text rules")
	}
	if !strings.Contains(field, "the leg's own mechanism (§5.1)") {
		t.Error("§2.4 omits the per-leg write mechanism")
	}
	if strings.Contains(section(algorithm, "## 5. ", "## 6. "), "not** a write-side contract") {
		t.Error("algorithm §5 exempts text from the write contract")
	}
	js := section(spec, "- **JavaScript:**", "### 6.2 ")
	if strings.Contains(js, "checked/production fork") || strings.Contains(js, "production variant") {
		t.Error("§6.1 describes a production writer variant")
	}
	if !strings.Contains(js, "writer checks in every build") {
		t.Error("§6.1 omits every-build JavaScript writer checks")
	}
	usageFlat := section(usage, "const ok = ReadShipCreateFlat", "The **runtime tier**")
	if strings.Contains(usageFlat, "checked/production") || strings.Contains(usageFlat, "production\nbundles") {
		t.Error("USAGE describes a production writer variant")
	}
	if !strings.Contains(usageFlat, "writers check in every build") {
		t.Error("USAGE omits every-build JavaScript writer checks")
	}
	cases := read("../test/packet-write-text/cases.txt")
	for _, want := range []string{
		"interior-null | 61 00 62 | refused",
		"invalid-byte | 61 ff 62 | refused",
		"surrogate | ed a0 80 | refused",
		"overlong | c0 af | refused",
		"above-unicode | f4 90 80 80 | refused",
		"truncated | e2 82 | refused",
		"bare-continuation | 80 | refused",
		"ascii | 61 62 63 | written",
		"euro | e2 82 ac | written",
		"emoji | f0 9f 98 80 | written",
		"empty | - | written",
	} {
		if !strings.Contains(cases, want) {
			t.Errorf("shared text cases missing %q", want)
		}
	}
	if !strings.Contains(cases, "debug assert") || !strings.Contains(cases, "refusal in every build") {
		t.Error("shared cases omit the per-leg verdict meaning")
	}
	if !strings.Contains(read("../test/packet-write-text/Text.schema"), "text string(8)") {
		t.Error("shared packet schema lacks string(8)")
	}
}
