package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCppHiddenUnionExtentRefusal(t *testing.T) {
	u := unitFromSource(t, `package probe
 table Item { values []uint32 }
 union Choice { item Item }
 union Outer { choice Choice }
 table Root { arms [..2]Choice
 nested [..2]Outer }
 `)
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint(nested), func(t *testing.T) {
			setup := "r->arms_count=0;r->arms[1].type=ChoiceType::Item;Item * item=&r->arms[1].item;"
			if nested {
				setup = "r->nested_count=0;r->nested[1].type=OuterType::Choice;r->nested[1].choice.type=ChoiceType::Item;Item * item=&r->nested[1].choice.item;"
			}
			source := `#include "ProbeTable.h"
#include <cstdio>
using namespace probe;
#define CHECK(x) do {if(!(x)){std::fprintf(stderr,"line %d: %s\n",__LINE__,#x);return 1;}}while(0)
int main(){RootBuilder builder;Root * r=builder.GetRoot();CHECK(r);` + setup + `
 uint32_t * value=ItemValuesAdd(builder.main,item->values);CHECK(value);*value=7;
 uint8_t output[4096];std::memset(output,0xa5,sizeof(output));CHECK(RootMeasure(builder)>0);
 CHECK(RootCookMeasure(builder)==-1);CHECK(!RootCook(builder,output,sizeof(output),TableByteOrder::Little));for(size_t i=0;i<sizeof(output);i++)CHECK(output[i]==0xa5);
 CHECK(!builder.Lock());CHECK(builder.AsConst()==NULL);return 0;}
`
			issue710CompileRun(t, dir, "c++", ".cpp", source)
		})
	}
}
