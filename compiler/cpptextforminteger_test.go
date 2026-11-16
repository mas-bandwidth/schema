package compiler

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestCPPTextFormIntegralNumbersFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ text-form reader")
	const schema = `package probe
table Root {
 small int32
 wide int64
 huge uint128
 values map[int32]int32
}`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if !strings.Contains(name, "/") {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	main := `#include <cstdio>
#include <cstring>
#include <vector>
#include "ProbeTable.h"
using namespace probe;
int main() {
 const char *cases[] = {"(:small 2e0 :wide 2.0 :huge 20e-1 :values ((2e0 3)))", "(:small 2.1)", "(:small 1e999999999999999999999)", "(:values ((2 1) (20e-1 2)))", "(:values ((true 1)))", "(:small -0.0)", "(:wide -9223372036854775808 :huge 340282366920938463463374607431768211455)", "(:wide 9223372036854775808)", "(:small 0e999999999999999999999)", "(:values ((2.1 1)))", "(:wide -9223372036854775809)", "(:huge 340282366920938463463374607431768211456)", "(:values ((2147483648 1)))", "(:small 1e-999999999999999999)", "(:values ((-0.0 1) (0 2)))"};
 for (const char *text : cases) {
  RootBuilder b; const char *reason = nullptr;
  if (!RootLoadTextForm(text,(int64_t)std::strlen(text),b,&reason)) {std::printf("REFUSE:%s\n",reason ? reason : "?"); continue;}
  if (!b.Lock()) return 2;
  const Root *v = b.AsConst(); const auto n = RootToJsonMeasure(v); if (n < 0) return 3;
  std::vector<char> out((size_t)n+1); if (RootToJson(v,out.data(),(int64_t)out.size()) != n) return 4;
  for (int64_t i=0;i<n;i++) std::printf("%02x",(unsigned char)out[(size_t)i]); std::puts("");
 }
}`
	path := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(path, []byte(main), 0600); err != nil {
		t.Fatal(err)
	}
	ser, err := filepath.Abs("../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	cmd := exec.Command("c++", "-std=c++17", "-O1", "-I"+dir, "-I"+ser, path, filepath.Join(dir, "ProbeTable.cpp"), "-o", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 15 {
		t.Fatalf("results: %s", out)
	}
	want := []string{"", "REFUSE:text_kind_mismatch", "REFUSE:text_bound", "REFUSE:text_duplicate_key", "REFUSE:text_kind_mismatch", "", "", "REFUSE:text_bound", "", "REFUSE:text_kind_mismatch", "REFUSE:text_bound", "REFUSE:text_bound", "REFUSE:text_bound", "REFUSE:text_kind_mismatch", "REFUSE:text_duplicate_key"}
	for i, expected := range want {
		if expected != "" && lines[i] != expected {
			t.Errorf("case %d: got %s want %s", i, lines[i], expected)
		}
		if expected == "" && strings.HasPrefix(lines[i], "REFUSE:") {
			t.Errorf("case %d: %s", i, lines[i])
		}
	}
	for _, i := range []int{0, 5, 6, 8} {
		data, err := hex.DecodeString(lines[i])
		if err != nil {
			t.Fatalf("case %d hex: %v", i, err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("case %d json: %v", i, err)
		}
		switch i {
		case 0:
			mapping, ok := value["values"].(map[string]any)
			if !ok || value["small"] != float64(2) || value["wide"] != "2" || value["huge"] != "2" || mapping["2"] != float64(3) {
				t.Errorf("integral values: %s", data)
			}
		case 5, 8:
			if value["small"] != float64(0) {
				t.Errorf("zero: %s", data)
			}
		case 6:
			if value["wide"] != "-9223372036854775808" || value["huge"] != "340282366920938463463374607431768211455" {
				t.Errorf("wide values: %s", data)
			}
		}
	}
	// Plant a fault in the generated runtime, then prove the same value
	// assertion used above detects it rather than merely finding the symbol.
	runtime := filepath.Join(dir, "ProbeTable.cpp")
	original, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "integer = canon.substr( 0, e );"
	if strings.Count(string(original), anchor) != 1 {
		t.Fatal("integer control anchor missing")
	}
	damaged := strings.Replace(string(original), anchor, "integer = \"7\";", 1)
	if err := os.WriteFile(runtime, []byte(damaged), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("c++", "-std=c++17", "-O1", "-I"+dir, "-I"+ser, path, runtime, "-o", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("control compile: %v\n%s", err, out)
	}
	damagedOut, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("control run: %v\n%s", err, damagedOut)
	}
	first, _, _ := strings.Cut(string(damagedOut), "\n")
	damagedJSON, err := hex.DecodeString(first)
	if err != nil {
		t.Fatalf("control hex: %v: %s", err, damagedOut)
	}
	var damagedValue map[string]any
	if err := json.Unmarshal(damagedJSON, &damagedValue); err != nil {
		t.Fatal(err)
	}
	if damagedValue["small"] == float64(2) {
		t.Fatal("INTEGRAL_VALUE_CONTROL did not detect the damaged runtime")
	}
	t.Log("INTEGRAL_VALUE_CONTROL detected the damaged runtime")
}
