package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Compile all emitted table modules, including accelerators, so a root whose
// form is disabled cannot leave a dangling module or export in the closure.
func rustFormsProbe(t *testing.T, files map[string][]byte, body string) {
	t.Helper()
	dir := t.TempDir()
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, ".rs") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0600); err != nil {
			t.Fatal(err)
		}
		mod := strings.TrimSuffix(name, ".rs")
		fmt.Fprintf(&main, "pub mod %s; pub use %s::*;\n", mod, mod)
	}
	main.WriteString("fn main(){\n" + body + "\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("rustc", "--edition", "2024", "--cfg", `feature="cook"`, "--cfg", `feature="block"`, "-O", "-A", "warnings", "-o", "run", "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Rust forms compile: %v\n%s", err, out)
	}
	cmd = exec.Command(filepath.Join(dir, "run"))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Rust forms run: %v\n%s", err, out)
	}
}

func TestRustDeclaredFormsNative(t *testing.T) {
	slowtest.Gate(t, "rustc (all eight declared form masks)")
	keyedRequireRustc(t)
	for mask := range 8 {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			forms := "wire"
			for bit, name := range []string{"json", "block", "cook"} {
				if mask&(1<<bit) != 0 {
					forms += "," + name
				}
			}
			u := unitFrom(t, fmt.Sprintf("package probe\nfixed table Root | forms=%q { value int32 }\n", forms))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			source := string(files["probe_table.rs"])
			for _, verb := range []string{"root_to_json(", "root_from_json(", "root_from_json_bytes("} {
				if strings.Contains(source, verb) != (mask&1 != 0) {
					t.Fatalf("mask %d JSON export %s", mask, verb)
				}
			}
			for bit, name := range map[int]string{2: "probe_block.rs", 4: "probe_cook.rs"} {
				if (len(files[name]) > 0) != (mask&bit != 0) {
					t.Fatalf("mask %d module %s", mask, name)
				}
			}
			m := tabletext.NewModel(u)
			v := m.New(u.Tables["Root"])
			if !m.Read(v, []byte(`{"value":7}`), &tabletext.Report{}) {
				t.Fatal("fixture read")
			}
			wire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`let value=Root{value:7};let wire=vec!%s;assert_eq!(root_save(&value),wire);assert_eq!(root_load(&wire,&mut TableReport::default()).unwrap().value,7);
let rows=vec![RootRow{value:37};2];let mut fixed=vec![0;root_fixed_measure(2)];assert_eq!(root_fixed_save(&rows,&mut fixed),Some(fixed.len()));let mut read=vec![RootRow::default();2];let mut plan=vec![TableFixedEntry::default();256];let mut remap=vec![0;256];assert_eq!(root_fixed_load(&mut read,&fixed,&mut plan,&mut remap,&mut TableFixedReport::default()),Some(2));assert_eq!(read[1].value,37);
let mut storage=vec![TableMessageEntry::default();64];let mut vocab=TableVocabulary::new(&mut storage);assert!(announce_read(&mut vocab,&TABLE_ANNOUNCEMENT,&mut TableReport::default()));let mut msg=vec![0;root_measure_messages(&rows,&mut TableReport::default()) as usize];assert_eq!(root_save_messages(&rows,&mut msg,&mut TableReport::default()),msg.len() as i64);assert_eq!(root_load_messages(&mut read,&vocab,&msg,&mut TableReport::default()),(2,true));assert_eq!(read[0].value,37);
`, rustByteVector(wire))
			if mask&1 != 0 {
				body += `let text=root_to_json(&value);assert_eq!(root_from_json(&text,&mut TableReport::default()).unwrap().value,7);assert!(root_from_json("{",&mut TableReport::default()).is_none());`
			}
			if mask&2 != 0 {
				offset := ir.Blocks(u).Block("Root").Projection.FieldByName("value").Offset
				body += fmt.Sprintf(`#[repr(align(64))] struct Aligned([u8;4096]);let mut block=Aligned([0;4096]);let count=unsafe{root_block_begin(block.0.as_mut_ptr(),4096,&RootCounts::default())}.unwrap();block.0[%d..%d].copy_from_slice(&23i32.to_ne_bytes());let opened=unsafe{RootBlock::open(block.0.as_ptr(),count)}.unwrap();assert_eq!(opened.projection().value,23);block.0[0]^=1;assert!(unsafe{RootBlock::open(block.0.as_ptr(),count)}.is_none());`, offset, offset+4)
			}
			if mask&4 != 0 {
				for _, big := range []bool{false, true} {
					pin, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
					if err != nil {
						t.Fatal(err)
					}
					order := 1
					if big {
						order = 2
					}
					body += fmt.Sprintf(`{let bytes=root_cook_write(&value,%d).unwrap();assert_eq!(bytes,vec!%s);let mut aligned=vec![0u64;(bytes.len()+7)/8];let p=aligned.as_mut_ptr() as *mut u8;unsafe{std::ptr::copy_nonoverlapping(bytes.as_ptr(),p,bytes.len());}let got=unsafe{RootCook::open(p,bytes.len() as u64)};if %d==TABLE_COOK_BYTE_ORDER {assert_eq!(unsafe{(*got.unwrap().root()).value},7);}else{assert!(got.is_none());}}`, order, rustByteVector(pin), order)
				}
			}
			rustFormsProbe(t, files, body)
		})
	}
}

func TestRustDeclaredFormsMixedClosure(t *testing.T) {
	slowtest.Gate(t, "rustc (mixed form closure across source files)")
	keyedRequireRustc(t)
	paths, err := filepath.Glob("../../../testdata/conformance/feature-witnesses/forms/*.schema")
	if err != nil || len(paths) != 3 {
		t.Fatalf("fixture: %v %v", paths, err)
	}
	var sources []check.SourceFile
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		ast, errs := parser.Parse(p, data)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		name := filepath.Base(p)
		sources = append(sources, check.SourceFile{Path: p, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: data, AST: ast})
	}
	u, errs := check.Unit(sources)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	for _, bad := range []string{"pub fn disabled_to_json(", "pub fn disabled_from_json(", "pub fn disabled_cook_write(", "pub struct DisabledCook", "pub struct DisabledBlock"} {
		if strings.Contains(all.String(), bad) {
			t.Fatalf("disabled root leaked: %s", bad)
		}
	}
	m := tabletext.NewModel(u)
	var body strings.Builder
	for _, tc := range []struct{ root, text string }{{"Allowed", `{"child":{"value":7}}`}, {"Holder", `{"by_name":{"a":{"value":17}},"child":{"value":23}}`}} {
		v := m.New(u.Tables[tc.root])
		if !m.Read(v, []byte(tc.text), &tabletext.Report{}) {
			t.Fatal("mixed fixture read")
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		name := ir.RustSnake(tc.root)
		fmt.Fprintf(&body, `{let wire=vec!%s;let value=%s_load(&wire,&mut TableReport::default()).unwrap();let text=%s_to_json(&value);let back=%s_from_json(&text,&mut TableReport::default()).unwrap();assert_eq!(%s_save(&back),wire);`, rustByteVector(wire), name, name, name, name)
		for _, big := range []bool{false, true} {
			pin, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := 1
			if big {
				order = 2
			}
			fmt.Fprintf(&body, `assert_eq!(%s_cook_write(&value,%d).unwrap(),vec!%s);`, name, order, rustByteVector(pin))
		}
		body.WriteString("}\n")
	}
	rustFormsProbe(t, files, body.String())
}
