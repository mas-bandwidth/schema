package darttable

// THE DEAD BYTES INSIDE AN ARRAY OF WALKED ELEMENTS. Every dead byte in a
// fixed record is zero, and a non-zero one refuses the whole load
// (docs/rulings.json fixed-dead-bytes-are-zero; docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.5, §5.3 step 11; docs/SPEC-TABLES.md §3.4, §13.10). The shared
// dead-byte vectors carry scalar dead regions; this schema puts the dead
// regions inside the elements of a fixed array of a struct and of a union,
// and inside the live element of a counted array of a struct, which is where
// an element's own plan must run instead of one copy of the whole array.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const nestedDeadSchema = `package probe

fixed table Small
{
    label string(8)
    child ?int32
    x int32
}

union U
{
    a int32
    b int64
}

fixed table Root
{
    items [3]Small
    us [3]U
    more [..3]Small
}
`

func TestFixedDeadBytesNestedArrays(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, nestedDeadSchema)
	offsets := nestedDeadOffsets(t, u)
	var list strings.Builder
	for _, off := range offsets {
		fmt.Fprintf(&list, "%d, ", off)
	}
	body := fmt.Sprintf(`
  values[0].moreCount = 1;
  values[0].us[0].type = 1;
  values[0].us[0].a = 0x11223344;
  final wrote = t.rootFixedSave(values, 1, bytes);
  check(wrote == bytes.length, 'the writer wrote $wrote of ${bytes.length} bytes');
  final body = t.rootFixedHeaderBytes + 8;
  final plan = t.rootFixedNewPlan();
  final rep = t.TableFixedReport();
  final clean = [t.Root()];
  t.initRoot(clean[0]);
  final n = t.rootFixedLoad(clean, 1, bytes, bytes.length, plan, rep);
  check(n == 1, 'the clean record must read: n=$n ${rep.refused}');
  final back = Uint8List(t.rootFixedMeasure(1));
  final wroteBack = t.rootFixedSave(clean, 1, back);
  check(wroteBack == back.length, 'the control saves $wroteBack of ${back.length}');
  for (var i = 0; i < back.length; i++) {
    check(back[i] == bytes[i], 'the control saves byte $i back: '
        '${back[i]} != ${bytes[i]}');
  }
  for (final off in <int>[%s]) {
    final dirty = Uint8List.fromList(bytes);
    dirty[body + off] = 0xA5;
    final loaded = [t.Root()];
    t.initRoot(loaded[0]);
    final r = t.TableFixedReport();
    final m = t.rootFixedLoad(loaded, 1, dirty, dirty.length, plan, r);
    check(m == -1, 'the dead byte at $off must refuse: m=$m ${r.refused}');
    check(loaded[0].moreCount == 0 &&
        loaded[0].items[0].labelLength == 0 &&
        loaded[0].items[0].childPresent == false &&
        loaded[0].us[0].type == 0,
        'a refused read lands nothing at $off: '
        'moreCount=${loaded[0].moreCount} '
        'labelLength=${loaded[0].items[0].labelLength} '
        'type=${loaded[0].us[0].type}');
  }
`, list.String())
	t12Capability(t, nestedDeadSchema, body, 1)
}

// nestedDeadOffsets is every dead byte this schema's clean record carries,
// as an offset into the body image: the string slack and absent-optional
// payload of the struct array's first element, the inactive arm and the
// bytes past the narrower taken arm of the union array, the same inside a
// live counted element, and the counted array's dead slot.
func nestedDeadOffsets(t *testing.T, u *ir.Unit) []int64 {
	t.Helper()
	root := u.Tables["Root"]
	if root == nil {
		t.Fatal("the schema declares no Root")
	}
	field := func(st *ir.Struct, name string) *ir.Field {
		for _, f := range st.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("%s declares no field %s", st.Name, name)
		return nil
	}
	at := func(st *ir.Struct, name string) int64 {
		var off int64
		for _, f := range st.Fields {
			if f.Name == name {
				return off
			}
			off += ir.TableFixedFieldBytes(f)
		}
		t.Fatalf("%s declares no field %s", st.Name, name)
		return 0
	}
	items := field(root, "items")
	small, ok := items.Type.Ref.(*ir.Struct)
	if !ok {
		t.Fatalf("items is not a struct array: %v", items.Type.Ref)
	}
	label := at(small, "label")
	child := at(small, "child")
	smallBytes := ir.TableFixedElementBytes(items)

	us := field(root, "us")
	union, ok := us.Type.Ref.(*ir.Union)
	if !ok {
		t.Fatalf("us is not a union array: %v", us.Type.Ref)
	}
	tag := fixedUnionTagBytes(union)
	usBytes := ir.TableFixedElementBytes(us)
	// the narrower arm is `a int32`; the bytes past it in the widest arm's
	// span (b int64) are dead.
	narrow := int64(4)

	itemsOff := at(root, "items")
	usOff := at(root, "us")
	moreOff := at(root, "more")

	return []int64{
		// items[0]: text slack, then the absent optional's payload.
		itemsOff + label + 4,
		itemsOff + child + 1,
		// us[0] takes the int32 arm: the bytes past it are dead.
		usOff + tag + narrow,
		// us[1] and us[2] name no arm: every arm byte under tag 0 is dead.
		usOff + usBytes + tag,
		usOff + 2*usBytes + tag,
		// more[0] is live (count 1): its own text slack and optional payload.
		moreOff + 4 + label + 4,
		moreOff + 4 + child + 1,
		// more[1] is past the live count: the slot is dead.
		moreOff + 4 + smallBytes,
	}
}
