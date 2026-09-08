package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

func TestCTableViewOutsideClosure(t *testing.T) {
	for _, tables := range []bool{false, true} {
		t.Run(map[bool]string{false: "packet", true: "tables"}[tables], func(t *testing.T) {
			source := viewSrc
			if !tables {
				source = strings.Replace(source, "table Root", "type Root", 1)
			}
			u := unitFromSource(t, source)
			runCTableWireProbe(t, u, `#include "ProbeView.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
int main(void) {
 const UnitViewInfo * view=unit_view(); const TableTypeInfo * type; Orphan value; int i;
 CHECK(view==unit_view() && !strcmp(view->package,"probe"));
 CHECK(view->num_enums==1 && view->num_flags==1 && view->num_unions==0);
 CHECK(view->enums[0].num_variants==3 && !strcmp(view->enums[0].variants[1].name,"Alpha"));
 CHECK(view->enums[0].variants[1].id==0 && view->flags[0].variants[1].value==1);
 CHECK(view->num_types>=2 && !strcmp(view->types[0].name,"Orphan"));
 type=view->types[0].type; CHECK(type==orphan_table_type() && type->num_fields==4);
 CHECK(!strcmp(type->doc,"A type no table reaches.") && type->num_tags==1 && !strcmp(type->tags[0],"inspected"));
 for(i=0;i<type->num_fields;i++) { CHECK(type->fields[i].id==0 && type->fields[i].json==NULL); }
 CHECK(type->fields[0].variants[1].id==0 && type->fields[3].keys[2].id==0);
 memset(&value,0xa5,sizeof(value));type->reset(&value);CHECK(value.grade==0 && value.label_length==0 && value.slots[0]==0);
 return 0;
}

`)
		})
	}
}

func TestCTableViewListing(t *testing.T) {
	for _, path := range []string{"../tables/examples", "../tables/pointers", "../tables/maps", "../tables/lists", "../tables/arms", "../examples"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			c := New()
			paths, err := filepath.Glob(filepath.Join(path, "*.schema"))
			if err != nil {
				t.Fatal(err)
			}
			u, err := c.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			files, err := c.Generate(u, "c", Options{})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir}
			base := strings.ToUpper(u.Package[:1]) + u.Package[1:] + "View"
			args = append(args, "-DVIEW_HEADER=\""+base+".h\"", "../test/c-tables/view_main.c")
			for name, data := range files {
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, data, 0600); err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(name, ".c") {
					args = append(args, p)
				}
			}
			binary := filepath.Join(dir, "view")
			args = append(args, "-lm", "-o", binary)
			if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
				t.Fatalf("compile: %v\n%s", err, out)
			}
			out, err := exec.Command(binary).CombinedOutput()
			if err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			if string(out) != viewlisting.Listing(u) {
				t.Fatalf("registry listing differs\nexpected:\n%s\ngot:\n%s", viewlisting.Listing(u), out)
			}
		})
	}
}
