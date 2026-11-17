package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Both packages go through the public C generator. The newer sender really
// owns extra and orders b before a; its announcement and messages are produced
// by the generated native writer, without editing a build identity.
func TestCTableMessageOrderAndMembership(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	receiver := unitFromSource(t, `package receiver
 fixed table Root { a uint8
 b uint8
 optional ?uint8
 must uint8 }
 fixed table Other { extra uint8
 spare uint8 }
 `)
	sender := unitFromSource(t, `package sender
 fixed table Root { b uint8
 a uint8
 extra uint8
 optional ?uint8
 must uint8 }
 fixed table Other { spare uint8 }
 `)
	if ir.BuildVersion(receiver) == ir.BuildVersion(sender) {
		t.Fatal("evolved writer did not change build identity")
	}
	entries := tablewire.MessageVocabulary(receiver)
	slots := tablewire.MessageVocabularySlots(receiver)
	if len(entries) != 6 || len(tablewire.MessageVocabulary(sender)) != 6 {
		t.Fatal("reference-boundary control requires E=6")
	}
	ref := func(st string, i int) uint64 { return slots[ir.TableFieldEntry(receiver.Tables[st].Fields[i]).Key()] }
	type field struct{ ref, value uint64 }
	good := []field{{ref("Root", 0), 1}, {ref("Root", 1), 2}, {ref("Root", 3), 9}}
	wire := func(bodies ...[]field) string {
		out := []byte{2, byte(len(bodies) - 1)}
		offset := 16
		put := func(v uint64, n int) {
			for bit := range n {
				if offset/8 >= len(out) {
					out = append(out, 0)
				}
				out[offset/8] |= byte((v>>uint(bit))&1) << uint(offset%8)
				offset++
			}
		}
		for _, body := range bodies {
			for _, f := range body {
				put(f.ref, 3)
				if f.ref <= uint64(len(entries)) {
					put(f.value, 8)
				}
			}
			put(0, 3)
		}
		var s strings.Builder
		for _, b := range out {
			fmt.Fprintf(&s, "0x%02x,", b)
		}
		return s.String()
	}
	cases := []struct {
		name string
		body []field
	}{
		{"reversed ordinary fields", []field{{ref("Root", 1), 2}, {ref("Root", 0), 1}, {ref("Root", 3), 9}}},
		{"optional field order", []field{{ref("Root", 2), 0}, {ref("Root", 0), 1}, {ref("Root", 3), 9}}},
		{"last field order", []field{{ref("Root", 3), 9}, {ref("Root", 0), 1}}},
		{"another record member", []field{{ref("Other", 0), 42}, {ref("Root", 3), 9}}},
	}
	var fixtures, checks strings.Builder
	for i, tc := range cases {
		fmt.Fprintf(&fixtures, "static const uint8_t bad%d[]={%s};\n", i, wire(good, tc.body))
		fmt.Fprintf(&checks, "if(!refuses(&v,bad%d,sizeof(bad%d),%q))failures++;\n", i, i, tc.name)
	}
	fmt.Fprintf(&fixtures, "static const uint8_t bad_ref[]={%s};\n", wire(good, []field{{7, 0}}))
	source := fmt.Sprintf(cMessageMembershipProbe, fixtures.String(), checks.String())
	for _, profile := range []struct {
		name  string
		flags []string
	}{
		{"normal", nil}, {"release", []string{"-DNDEBUG"}}, {"sanitized", []string{"-DNDEBUG", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-fno-sanitize-recover=all"}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			dir := t.TempDir()
			var sources []string
			for _, u := range []*ir.Unit{receiver, sender} {
				files, err := New().Generate(u, "c", Options{})
				if err != nil {
					t.Fatal(err)
				}
				sub := filepath.Join(dir, u.Package)
				if err := os.MkdirAll(sub, 0700); err != nil {
					t.Fatal(err)
				}
				for name, data := range files {
					if strings.Contains(name, "/") {
						continue
					}
					path := filepath.Join(sub, name)
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					if strings.HasSuffix(name, ".c") {
						sources = append(sources, path)
					}
				}
			}
			main := filepath.Join(dir, "main.c")
			if err := os.WriteFile(main, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-std=c99", "-O1", "-Wall", "-Wextra", "-Werror", "-Wshadow"}
			args = append(args, profile.flags...)
			args = append(args, "-I", dir, main)
			args = append(args, sources...)
			args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
			ctx, cancel := slowtest.ProbeContext(t)
			defer cancel()
			if out, err := exec.CommandContext(ctx, "cc", args...).CombinedOutput(); err != nil {
				t.Fatalf("compile: %v\n%s", err, out)
			}
			if out, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
				t.Fatalf("execute: %v\n%s", err, out)
			}
		})
	}
}

const cMessageMembershipProbe = `#include "receiver/ProbeTable.h"
#include "sender/ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}}while(0)
%s
static int refuses(receiver_table_vocabulary_t *v,const uint8_t *wire,int64_t bytes,const char *name){
 receiver_root_t out[2];memset(out,0x7f,sizeof(out));receiver_table_report_t report={0};int64_t count=2;
 int ok=receiver_root_load_messages(out,&count,v,wire,bytes,&report);
 if(ok||count!=0||!report.refused||report.refuse_reason!=SCHEMA_RECEIVER_TABLE_REFUSE_WIRE_DAMAGED||out[0].a||out[0].b||out[0].must||out[1].a||out[1].b||out[1].must){fprintf(stderr,"case %%s accepted or retained partial batch\n",name);return 0;}return 1;
}
int main(void){
 int failures=0;
 receiver_table_message_entry_t entries[16];receiver_table_vocabulary_t v=receiver_table_vocabulary(entries,16);receiver_table_report_t report={0};
 CHECK(receiver_announce_read(&v,RECEIVER_K_TABLE_ANNOUNCE,RECEIVER_K_TABLE_ANNOUNCE_BYTES,&report));
 /* Ordinary generated own-writer output still reads at its own build. */
 receiver_root_t own[2],out[2];receiver_root_reset(own);receiver_root_reset(own+1);own[0].a=1;own[0].b=2;own[0].must=9;own[1].a=3;own[1].must=4;
 uint8_t wire[512];int64_t n=receiver_root_save_messages(own,2,wire,sizeof(wire),&report),count=2;
 CHECK(n>0 && receiver_root_load_messages(out,&count,&v,wire,n,&report));CHECK(count==2 && !report.refused && out[0].a==1 && out[1].a==3);
 %s
 /* E+1 always refuses, independently of build/order membership proof. */
 CHECK(refuses(&v,bad_ref,sizeof(bad_ref),"own out-of-vocabulary reference"));
 sender_root_t newer;sender_root_reset(&newer);newer.a=1;newer.b=2;newer.extra=42;newer.optional_present=1;newer.optional=3;newer.must=9;
 sender_table_report_t sender_report={0};n=sender_root_save_messages(&newer,1,wire,sizeof(wire),&sender_report);
 CHECK(n>0 && !sender_report.refused);
 uint8_t announcement[SENDER_K_TABLE_ANNOUNCE_BYTES];CHECK(sender_announce(announcement,sizeof(announcement))==sizeof(announcement));
 v=receiver_table_vocabulary(entries,16);
 CHECK(receiver_announce_read(&v,announcement,sizeof(announcement),&report));
 CHECK(v.build_version!=SCHEMA_RECEIVER_BUILD_VERSION_VALUE);
 count=1;CHECK(receiver_root_load_messages(out,&count,&v,wire,n,&report));
 CHECK(count==1 && !report.refused && report.unknown==1 && out[0].a==1 && out[0].b==2 && out[0].optional_present && out[0].optional==3 && out[0].must==9);
 CHECK(refuses(&v,bad_ref,sizeof(bad_ref),"evolved out-of-vocabulary reference"));
 return failures?1:0;
}
`
