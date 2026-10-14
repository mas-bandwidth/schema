package compiler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A ZERO-BIT ANNOUNCED ELEMENT CANNOT BUY UNBOUNDED RESOLVING WORK
// (docs/SPEC-TABLES.md §6.6). An unbounded list of zero-bit elements announces
// min == max, so TableBitsRequired spends no bit per element and the count comes
// straight from the vocabulary, where kTableMessageListMax is 0xFFFFFFFF. The
// message retention walk must test the least resolved element bytes against the
// caller's remaining record BEFORE it converts the count, refuse the whole
// record, and finish inside a 64-byte store. The C target pins the same pair in
// TestCTableRetainCapacityBoundsZeroBitMessage.
func TestCppTableRetainCapacityBoundsZeroBitMessage(t *testing.T) {
	old := unitFromSource(t, "package probe\ntable Root { next *Root }\n")
	future := unitFromSource(t, "package probe\ntable Root { next *Root\nzeroes []int32 | min = 0, max = 0 }\n")
	files, err := New().Generate(old, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var announcement strings.Builder
	for _, b := range tablewire.Announce(future) {
		fmt.Fprintf(&announcement, "0x%02x,", b)
	}
	runCppTableProbe(t, files, fmt.Sprintf(`#include "ProbeTable.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t announcement[]={%s};
int main(){
 TableMessageEntry entries[64];TableVocabulary vocabulary(entries,64);TableReport report={0};
 CHECK(AnnounceRead(vocabulary,announcement,sizeof(announcement),&report));
 uint64_t ref=0;for(int i=0;i<(int)vocabulary.count;i++)if(vocabulary.entries[i].kind==14)ref=(uint64_t)i+1;CHECK(ref!=0);
 uint8_t wire[64];TableBitWriter w(wire,sizeof(wire));
 w.put(2,8);w.put(0,8);w.put(ref,vocabulary.ref_bits);w.put(UINT32_MAX,32);w.put(0,vocabulary.ref_bits);w.align();CHECK(!w.overflow);
 const int64_t need=RootLoadMeasure(vocabulary,wire,w.bits/8);CHECK(need>0);
 uint8_t * region=(uint8_t *)malloc((size_t)need);CHECK(region);
 TableRetain retain;TableRetain::Id ids[16];uint8_t store[65];memset(store,0xa5,sizeof(store));
 retain.bytes=store;retain.capacity=sizeof(store)-1;retain.ids=ids;retain.id_capacity=16;
 const Root * roots[1];int64_t count=1;
 CHECK(RootLoadRetainMessages(roots,&count,region,need,vocabulary,wire,w.bits/8,&retain,&report));
 CHECK(count==1&&!report.malformed&&!report.refused&&report.unknown==1&&report.retain_lost==1&&report.retained==0);
 CHECK(retain.used==0&&retain.count==0&&store[sizeof(store)-1]==0xa5);
 free(region);return 0;
}
`, announcement.String()))
}

// runCppTableProbe writes the emitted sources and one driver beside them,
// compiles at -O1 under ASan/UBSan, and runs the driver under a hard timeout: a
// resolving walk that will not stop is the failure this test exists to catch,
// so the wall clock is part of the assertion.
func runCppTableProbe(t *testing.T, files map[string][]byte, source string) {
	t.Helper()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("generated C++ execution requires c++")
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-g", "-Wall", "-Wextra", "-Werror", "-fsanitize=address,undefined", "-fno-sanitize-recover=all", filepath.Join(dir, "main.cpp")}
	for name := range files {
		if strings.HasSuffix(name, ".cpp") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, bin).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
}
