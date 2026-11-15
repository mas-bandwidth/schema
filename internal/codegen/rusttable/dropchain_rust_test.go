package rusttable

// DESTROYING A LOADED POINTER CHAIN COSTS NO STACK PER NODE (docs/SPEC-TABLES.md
// §3.1, §4; docs/SECURITY.md): a value the Rust leg reads from a conforming file
// can be dropped on any thread however long its pointer chain is. The load and
// save walks already hold their own stack; a table type that can reach itself
// through a pointer field emits a Drop impl that unlinks the chain iteratively,
// so the compiler's drop glue does not recurse once per node. This is the same
// rule for the partial graph a refused read owns: the loader drops it inside
// the load.
//
// A FUNCTIONAL TEST: it compiles and runs the generated tables/pointers unit
// with rustc. It skips by name when rustc is absent and SCHEMA_REQUIRE_CORPUS
// is unset, and fails by name when SCHEMA_REQUIRE_CORPUS=1. The probe builds a
// 200,000-node chain with the unit's own save, loads it back, and drops it on a
// 256 KB thread; a second thread loads a long chain whose last record carries
// ill-formed text and drops the partial graph the refusal owns. The probe
// prints the length it read and exits 0.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const rustPointerChainDropProbe = `
    use std::rc::Rc;

    const NODES: usize = 200_000;
    const MARKER: &str = "LASTNODE";

    fn build_chain(n: usize) -> ListNode {
        let mut node = ListNode { value: (n - 1) as i32, name: MARKER.to_string(), next: None };
        for i in (0..n - 1).rev() {
            node = ListNode { value: i as i32, name: String::new(), next: Some(Rc::new(node)) };
        }
        node
    }

    fn chain_len(mut node: &ListNode) -> usize {
        let mut len = 0usize;
        loop {
            len += 1;
            match &node.next {
                Some(next) => node = next,
                None => break,
            }
        }
        len
    }

    let chain = build_chain(NODES);
    let wire = list_node_save(&chain);
    std::mem::forget(chain);

    let mut damaged = wire.clone();
    let at = damaged
        .windows(MARKER.len())
        .position(|w| w == MARKER.as_bytes())
        .expect("the last record's name marker is in the wire");
    damaged[at] = 0xFF;

    let load = std::thread::Builder::new()
        .stack_size(256 * 1024)
        .spawn(move || {
            let mut report = TableReport::default();
            let value = list_node_load(&wire, &mut report).expect("the conforming chain loads");
            println!("DROP len {}", chain_len(&value));
            // the loaded value drops here, on this thread
        })
        .expect("spawn the loading thread")
        .join();
    if load.is_err() {
        std::process::exit(2);
    }

    let partial = std::thread::Builder::new()
        .stack_size(256 * 1024)
        .spawn(move || {
            let mut report = TableReport::default();
            let value = list_node_load(&damaged, &mut report);
            if value.is_some() {
                println!("PARTIAL loaded a value");
                std::process::exit(3);
            }
            println!("PARTIAL refused");
            // the loader dropped the partial graph before returning
        })
        .expect("spawn the refusing thread")
        .join();
    if partial.is_err() {
        std::process::exit(4);
    }
`

func TestRustLongPointerChainDrops(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the drop rule cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(vcLoadUnit(t, root, "tables/pointers"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out, err := runRustGeneratedFilesProbe(t, files, rustPointerChainDropProbe)
	if err != nil {
		t.Fatalf("the drop probe did not finish: %v\n%s", err, out)
	}
	if !strings.Contains(out, "DROP len 200000") {
		t.Errorf("the probe did not report the length it read:\n%s", out)
	}
	if !strings.Contains(out, "PARTIAL refused") {
		t.Errorf("the probe did not refuse the damaged chain:\n%s", out)
	}
}
