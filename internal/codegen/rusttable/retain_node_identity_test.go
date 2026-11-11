package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// retainedNodeIdentityProducer declares the same graph the pointer corpus
// loads, with one field the reader does not name on each list node. Its wire is
// the retaining load's input: both list nodes carry an unknown `extra`.
const retainedNodeIdentityProducer = `package graphdemo

table ListNode
{
    value int32
    name  string(12)
    next  *ListNode
    extra string(16)
}

table Scene
{
    head  *ListNode
    alias *ListNode
}
`

// TestRustRetainedNodeIdentity holds docs/SPEC-TABLES.md §6.6's node rule: the
// unknown fields retained from a pointer node are saved back with that node and
// with no other, so a node the caller drops takes its retained fields with it.
//
// The retaining load answers a graph of two list nodes, each carrying an
// unknown field. The test drops the `alias` node, then builds replacements
// until one lands at the same Rc address, so a store keyed only by that address
// would hand the dropped node's record to the replacement. The replacement must
// save with no retained field while the untouched `head` node saves its own
// field back byte-exact.
func TestRustRetainedNodeIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	wire, producer := retainedNodeIdentityWire(t)
	u := vcLoadUnit(t, repo, "tables/pointers")

	outPath := filepath.Join(t.TempDir(), "retained.wire")
	body := fmt.Sprintf(`
    use std::alloc::{GlobalAlloc, Layout, System};
    use std::rc::Rc;
    use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

    // A one-slot cache for the dropped node's Rc allocation makes the address
    // reuse repeatable: the first replacement is built at the address the
    // dropped node occupied. While the retaining store holds the node's
    // identity, its allocation is not returned here, so no replacement can take
    // that address during the round trip.
    const NODE_ALLOC: usize = 2 * std::mem::size_of::<usize>() + std::mem::size_of::<ListNode>();
    struct DroppedNodeAlloc;
    static DROPPED: AtomicUsize = AtomicUsize::new(0);
    static WATCH: AtomicBool = AtomicBool::new(false);
    unsafe impl GlobalAlloc for DroppedNodeAlloc {
        unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
            if WATCH.load(Ordering::SeqCst) && layout.size() == NODE_ALLOC {
                let p = DROPPED.swap(0, Ordering::SeqCst);
                if p != 0 { return p as *mut u8; }
            }
            System.alloc(layout)
        }
        unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
            if WATCH.load(Ordering::SeqCst) && layout.size() == NODE_ALLOC {
                let old = DROPPED.swap(ptr as usize, Ordering::SeqCst);
                if old != 0 { System.dealloc(old as *mut u8, layout); }
                return;
            }
            System.dealloc(ptr, layout);
        }
    }
    #[global_allocator]
    static DROPPED_NODE_ALLOC: DroppedNodeAlloc = DroppedNodeAlloc;

    const WIRE: [u8; %d] = [%s];
    let mut store = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(1 << 10), nodes: Vec::new() };
    let mut report = TableReport::default();
    let mut scene = scene_load_retain(&WIRE, &mut store, &mut report).expect("the retaining load refused the graph");
    if report.unknown != 2 || report.retained != 2 {
        println!("BAD load counters unknown={} retained={} retain_lost={}", report.unknown, report.retained, report.retain_lost);
        std::process::exit(2);
    }

    let old = scene.alias.take().expect("the alias node");
    let old_addr = Rc::as_ptr(&old) as *const () as usize;
    let mut candidates = Vec::with_capacity(4096);
    WATCH.store(true, Ordering::SeqCst);
    drop(old);
    let mut replacement = None;
    for _ in 0..4096 {
        let candidate = Rc::new(ListNode { value: 21, name: "fresh".into(), next: None });
        if Rc::as_ptr(&candidate) as *const () as usize == old_addr {
            replacement = Some(candidate);
            break;
        }
        candidates.push(candidate);
    }
    WATCH.store(false, Ordering::SeqCst);
    let fresh = match replacement {
        Some(candidate) => candidate,
        None => {
            println!("NO_ADDRESS_REUSE after 4096 allocations");
            candidates.pop().expect("no replacement was built")
        }
    };
    scene.alias = Some(fresh);

    let saved = scene_save_retain(&scene, &mut store, &mut report);
    std::fs::write(%q, &saved).unwrap();
    println!("SAVED {}", saved.len());
`, len(wire), rustBytes(wire), outPath)

	out, err := runRustDiscardProbe(t, u, body)
	if err != nil {
		t.Fatalf("the retained node identity probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SAVED") {
		t.Fatalf("the retained node identity probe did not save:\n%s", out)
	}

	saved, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	back := producer.New(producer.Lookup("Scene"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(producer, back, saved, &report); !ok || err != nil {
		t.Fatalf("the saved graph did not read back: ok=%v err=%v %+v", ok, err, report)
	}
	fresh := retainedNodeField(t, back, "alias").Cell.Node
	if fresh == nil {
		t.Fatal("the replaced alias node did not save")
	}
	if got := string(retainedNodeField(t, fresh, "extra").Cell.Str); got != "" {
		t.Fatalf("the replaced node saved a retained field from the dropped node: extra=%q", got)
	}
	if got := retainedNodeField(t, fresh, "value").Cell.I; got != 21 {
		t.Fatalf("the replaced node did not save its own value: got %d", got)
	}
	if got := string(retainedNodeField(t, fresh, "name").Cell.Str); got != "fresh" {
		t.Fatalf("the replaced node did not save its own name: got %q", got)
	}
	head := retainedNodeField(t, back, "head").Cell.Node
	if head == nil {
		t.Fatal("the untouched head node did not save")
	}
	if got := string(retainedNodeField(t, head, "extra").Cell.Str); got != "head-extra" {
		t.Fatalf("the untouched node did not save its own retained field: extra=%q", got)
	}
	if got := retainedNodeField(t, head, "value").Cell.I; got != 1 {
		t.Fatalf("the untouched node did not save its own value: got %d", got)
	}
}

// retainedNodeIdentityWire writes the producer's graph: a head node carrying
// `head-extra`, and an alias node carrying `alias-extra`, each unknown to the
// pointer corpus's Scene.
func retainedNodeIdentityWire(t *testing.T) ([]byte, *tabletext.Model) {
	t.Helper()
	producer := tabletext.NewModel(unitFrom(t, retainedNodeIdentityProducer))
	scene := producer.New(producer.Lookup("Scene"))
	head := producer.New(producer.Lookup("ListNode"))
	retainedNodeSetInt(t, head, "value", 1)
	retainedNodeSetStr(t, head, "name", "old-head")
	retainedNodeSetStr(t, head, "extra", "head-extra")
	alias := producer.New(producer.Lookup("ListNode"))
	retainedNodeSetInt(t, alias, "value", 2)
	retainedNodeSetStr(t, alias, "name", "alias")
	retainedNodeSetStr(t, alias, "extra", "alias-extra")
	retainedNodeSetNode(t, scene, "head", head)
	retainedNodeSetNode(t, scene, "alias", alias)
	wire, err := tablewire.Encode(producer, scene)
	if err != nil {
		t.Fatalf("the graph did not encode: %v", err)
	}
	return wire, producer
}

func retainedNodeField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("%s declares no field %q", inst.Def.Name, name)
	return nil
}

func retainedNodeSetInt(t *testing.T, inst *tabletext.Instance, name string, v int64) {
	t.Helper()
	f := retainedNodeField(t, inst, name)
	f.Cell.I, f.Cell.U = v, uint64(v)
}

func retainedNodeSetStr(t *testing.T, inst *tabletext.Instance, name, v string) {
	t.Helper()
	f := retainedNodeField(t, inst, name)
	f.Cell.Str = []byte(v)
	f.Count = len(v)
}

func retainedNodeSetNode(t *testing.T, inst *tabletext.Instance, name string, target *tabletext.Instance) {
	t.Helper()
	retainedNodeField(t, inst, name).Cell.Node = target
}
