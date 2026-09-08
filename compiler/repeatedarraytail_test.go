package compiler

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// #725: a later array occurrence decodes no elements. The old loaded
// value kept the first occurrence's list in entries[1], above count zero.
func TestRepeatedArrayTailDefaults(t *testing.T) {
	paths, err := filepath.Glob("../tables/arms/*.schema")
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString("01010e1e0f0202040401000000030d12040e0e040305000000060000000700000000010e1e0f0202047f01000000030d12040e0e0403050000000600000007000000000504020000000053a245082ca7b2c507b252164e194dfdd50802a2ad84ad246f2c414fbf84783ee9ea716f0f0182bf0500000000000000")
	if err != nil {
		t.Fatal(err)
	}
	common := fmt.Sprintf(`#include "CarryTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"tail defaults line %%d: %%s\n",__LINE__,#x);return 1;}}while(0)
static const uint8_t wire[]={%s};
`, cppWire(wire))
	t.Run("c-bounded-extent", func(t *testing.T) {
		// Replace the first array count with UINT64_MAX, preserving its L.
		// Only the two encoded elements exist; extent must stop at EOF.
		mutant := append([]byte(nil), wire[:5]...)
		mutant[3] += 9
		mutant = append(mutant, 255, 255, 255, 255, 255, 255, 255, 255, 255, 1)
		mutant = append(mutant, wire[6:]...)
		source := strings.Replace(common, cppWire(wire), cppWire(mutant), 1)
		runCTableWireProbe(t, u, source+`int main(void){CHECK(hand_load_measure(wire,sizeof(wire))==88);return 0;}`)
	})
	t.Run("cpp", func(t *testing.T) {
		files, err := New().Generate(u, "cpp", Options{})
		if err != nil {
			t.Fatal(err)
		}
		referenceCompileRun(t, files, common+`using namespace armdemo;
int main(){TableReport report;int64_t n=HandLoadMeasure(wire,sizeof(wire));CHECK(n>0);uint8_t * region=(uint8_t*)malloc(n);const Hand * r=HandLoad(region,n,wire,sizeof(wire),&report);CHECK(r);CHECK(report.malformed);CHECK(r->entries_count==0);CHECK(r->entries[1].type==CarryType::None);HandBuilder b;CHECK(HandLoadBuilder(b,wire,sizeof(wire),&report));CHECK(b.GetRoot()->entries[1].type==CarryType::None);CHECK(HandCookMeasure(b)>0);free(region);return 0;}`)
	})
	t.Run("c", func(t *testing.T) {
		runCTableWireProbe(t, u, common+`
int main(void){TableReport report={0};int64_t n=hand_load_measure(wire,sizeof(wire));CHECK(n>0);uint8_t * region=(uint8_t*)malloc((size_t)n);const Hand * r=hand_load(region,n,wire,sizeof(wire),&report);CHECK(r);CHECK(report.malformed);CHECK(r->entries_count==0);CHECK(r->entries[1].type==0);free(region);return 0;}`)
	})
}
