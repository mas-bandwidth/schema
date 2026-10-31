package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookHasExtent reports a record whose node is its storage plus an extent
// (docs/SPEC-TABLES.md §2.8, §2.9, §7.2): a list or a map reachable BY VALUE,
// directly, inside a nested record or inside a union arm. It is the tool's
// recordHasExtent (internal/tablecook/extent.go). A pointer and a byte buffer
// are nodes of their own and never part of it.
func cookHasExtent(u *ir.Unit, st *ir.Struct) bool {
	return cookExtentWalk(u, st, nil, map[string]bool{})
}

func cookExtentWalk(u *ir.Unit, st *ir.Struct, un *ir.Union, seen map[string]bool) bool {
	var fields []*ir.Field
	switch {
	case st != nil:
		if seen[st.Name] {
			return false
		}
		seen[st.Name] = true
		fields = st.Fields
	case un != nil:
		if seen["union:"+un.Name] {
			return false
		}
		seen["union:"+un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				fields = append(fields, v.F)
			}
		}
	}
	for _, f := range fields {
		if f.IsMap() || f.IsList() {
			return true
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if cookExtentWalk(u, ref, nil, seen) {
				return true
			}
		case *ir.Union:
			if cookExtentWalk(u, nil, ref, seen) {
				return true
			}
		}
	}
	return false
}

// cookRecordBytes is one node's record half (docs/SPEC-TABLES.md §2.8, §7.2):
// sizeof, or, when an extent follows, where that extent begins
// (ir.CookExtentStart, the one rule every leg and the tool share).
func cookRecordBytes(u *ir.Unit, st *ir.Struct) int64 {
	if !cookHasExtent(u, st) {
		return ir.RecordLayout(u, st).Size
	}
	return ir.CookExtentStart(u, st)
}

// cookNodeAlign is one node's alignment in a cooked region (docs/SPEC-TABLES.md
// §2.8, §6.2): its record's, and for a record with an extent the greatest
// alignment any array that extent can hold too (ir.CookNodeAlign).
func cookNodeAlign(u *ir.Unit, st *ir.Struct) int64 {
	if !cookHasExtent(u, st) {
		return ir.RecordLayout(u, st).Align
	}
	return ir.CookNodeAlign(u, st)
}

// writeCookLayout adds the cook writer's columns (docs/SPEC-TABLES.md §7.6):
// the piece offsets a region is written at, the array class, and the type id
// a directory entry carries.
func writeCookLayout(b *strings.Builder, u *ir.Unit, f *ir.Field, fl *ir.FieldLayout) {
	off := int64(0)
	if fl != nil {
		off = fl.Offset
	}
	pieces := ir.FieldPieces(u, f, off)
	countOff, presentOff := int64(-1), int64(-1)
	for i, name := range cookRowMembers(f) {
		if i >= len(pieces) {
			break
		}
		if strings.HasSuffix(name, "_length") || strings.HasSuffix(name, "_count") {
			countOff = pieces[i].Offset
		}
		if strings.HasSuffix(name, "_present") {
			presentOff = pieces[i].Offset
		}
	}
	if f.Array == ir.ArrayCounted && f.KeyEnum == "" && countOff < 0 && len(pieces) > 1 {
		// a counted array of pointers is its N slots, then the int32 used
		// count (§7.2), though the reader's row names it no companion
		countOff = pieces[1].Offset
	}
	fmt.Fprintf(b, ", cook_off: %d, count_off: %d, present_off: %d", off, countOff, presentOff)
	if f.IsMap() {
		// a map's holder points at its ENTRY array in the node extent (§2.8):
		// one generated entry record per key, at the entry's size and alignment
		ml := ir.RecordLayout(u, f.MapEntry)
		fmt.Fprintf(b, ", entry_size: %d, entry_align: %d", ml.Size, max(ml.Align, 1))
	}
	// one :array key per descriptor: a keyed array is a fixed one whose index
	// is an enum, and :keyed is the shape the cook dispatches on
	switch {
	case f.KeyEnum != "":
		b.WriteString(", array: :keyed")
	case f.Array == ir.ArrayList:
		b.WriteString(", array: :list")
	case f.Array == ir.ArrayCounted:
		b.WriteString(", array: :counted")
	case f.Array == ir.ArrayFixed:
		b.WriteString(", array: :fixed")
	}
	if f.Type.Pointer && f.Type.Blob() {
		fmt.Fprintf(b, ", cook_type_id: 0x%016X", ir.BlobTypeId(f))
	} else if f.Type.Pointer && isStruct(f) {
		ref := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, ", cook_type_id: 0x%016X", ir.TableTypeId(ref.WireName()))
	}
	if f.Array != ir.ArrayNone && f.Type.Pointer && isStruct(f) {
		ref := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, ", elem_cook_id: 0x%016X", ir.TableTypeId(ref.WireName()))
	}
	if e := enumRef(f); e != nil {
		b.WriteString(", ords: %{")
		for i, v := range e.Variants {
			fmt.Fprintf(b, "%q => %d, ", v, i+1)
		}
		b.WriteString("}")
		fmt.Fprintf(b, ", enum_width: %d", e.StorageBits/8)
	}
	if f.Array == ir.ArrayCounted || f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, ", max_count: %d", f.ArrayBound)
	}
	if f.Array == ir.ArrayNone && enumRef(f) == nil && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits ||
		f.Type.Kind == ir.TFixed || flagsRef(f) != nil) {
		// an integer's storage width (§7.2): a bits(N) is four or eight
		// bytes and a flags mask eight, wider than the wire's
		size, _ := ir.ListElementLayout(u, f)
		fmt.Fprintf(b, ", cook_width: %d", size)
	}
	if f.KeyEnum != "" {
		// an enum-keyed array's slots are positional in a region (§7.2):
		// variant v's element at (v - 1) times the element's size
		size, _ := ir.ListElementLayout(u, f)
		fmt.Fprintf(b, ", elem_size: %d", size)
	}
	if un := unionRef(f); un != nil {
		// a union is a tag beside its arms (§7.2): the tag at the union's own
		// base at its storage width, the set arm's pieces at arm_off
		_, _, tag, armOff := ir.UnionLayout(u, un)
		fmt.Fprintf(b, ", tag_width: %d, arm_off: %d", tag, armOff)
	}
	if f.Array != ir.ArrayNone {
		switch {
		case f.Type.Pointer && isStruct(f):
			b.WriteString(", elem_t: :pointer")
		case isStruct(f):
			b.WriteString(", elem_t: :record")
		case enumRef(f) != nil:
			b.WriteString(", elem_t: :enum")
		case unionRef(f) != nil:
			b.WriteString(", elem_t: :union")
		case f.Type.Kind == ir.TBool:
			b.WriteString(", elem_t: :bool")
		case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
			b.WriteString(", elem_t: :float")
		case f.Type.Kind == ir.TString:
			b.WriteString(", elem_t: :string")
		case f.Type.Kind == ir.TBytes:
			b.WriteString(", elem_t: :bytes")
		default:
			b.WriteString(", elem_t: :int")
			fmt.Fprintf(b, ", elem_signed: %v", f.Type.Signed)
		}
	}
}

// cookExtentDef and cookCarveDef are the cook writer's two entries into the
// extent walk (docs/SPEC-TABLES.md §2.8, §2.9, §7.6), and extentWalkFamilies
// the walk itself. In a unit where no table has an extent every has_extent
// the walk branches on reads a literal false, which Elixir's type checker
// reports as a branch that cannot match, so such a unit is emitted with the
// two entries answering what the walk would (no extent, nothing carved) and
// without the walk.
const cookExtentDef = "  defp cook_extent(desc, value),\n" +
	"    do: cook_walk_record(desc, value, 0, %{buf: nil, base: 0, at: 0, index: nil, order: nil}).at\n"

const cookCarveDef = "  defp cook_carve(desc, value, node_off, index, order, buf) do\n" +
	"    ctx = %{buf: buf, base: node_off + desc.record, at: 0, index: index, order: order}\n" +
	"    cook_walk_record(desc, value, node_off, ctx).buf\n" +
	"  end\n"

var extentWalkFamilies = []string{
	"cook_walk_record", "cook_walk_field", "cook_walk_union", "cook_walk_elem",
	"cook_walk_holder", "cook_put_holder", "cook_put_member", "cook_walk_member", "cook_list",
}

// withoutExtentWalks is the cook writer for a unit with no extent anywhere.
// Every piece must be found, so a template edit that moves one fails loudly.
func withoutExtentWalks(cook string) string {
	cook = replaceOnce(cook, cookExtentDef, "  defp cook_extent(_desc, _value), do: 0\n")
	cook = replaceOnce(cook, cookCarveDef, "  defp cook_carve(_desc, _value, _node_off, _index, _order, buf), do: buf\n")
	found := map[string]bool{}
	cook = dropTopLevel(cook, func(head string) bool {
		for _, name := range extentWalkFamilies {
			if strings.HasPrefix(head, "  defp "+name+"(") {
				found[name] = true
				return true
			}
		}
		return false
	})
	for _, name := range extentWalkFamilies {
		if !found[name] {
			panic("elixirtable: extent walk family not found: " + name)
		}
	}
	return cook
}

// cookUnionFamilies and cookUnionClauses are the cook writer's pieces that
// walk or write a union. In a unit with no union there is no @unions, so a
// unit is emitted without them (withoutUnions is the runtime's twin). The
// extent walk's union pieces, cookUnionWalkFamilies and cookUnionWalkClauses,
// are there only when the walk is (withoutExtentWalks).
var cookUnionFamilies = []string{"cook_collect_arm", "cook_arm", "cook_union"}

var cookUnionClauses = []string{
	"  defp cook_collect_field(%{t: :union",
	"  defp cook_collect_elem(%{elem_kind: 15",
	"  defp cook_value(%{t: :union",
	"  defp cook_elem(%{elem_t: :union",
}

var cookUnionWalkFamilies = []string{"cook_walk_union"}

var cookUnionWalkClauses = []string{
	"  defp cook_walk_field(%{t: :union",
	"  defp cook_walk_elem(%{elem_t: :union",
}

// cookWithoutUnions drops the union pieces from the cook writer, each with the
// comment above it. Every one must be found, so a template change that renames
// one fails the emitter's tests. walks says the extent walk is still there.
func cookWithoutUnions(cook string, walks bool) string {
	families := append([]string{}, cookUnionFamilies...)
	clauses := append([]string{}, cookUnionClauses...)
	if walks {
		families = append(families, cookUnionWalkFamilies...)
		clauses = append(clauses, cookUnionWalkClauses...)
	}
	found := map[string]bool{}
	cook = dropTopLevel(cook, func(head string) bool {
		for _, name := range families {
			if strings.HasPrefix(head, "  defp "+name+"(") {
				found[name] = true
				return true
			}
		}
		for _, c := range clauses {
			if strings.HasPrefix(head, c) {
				found[c] = true
				return true
			}
		}
		return false
	})
	for _, k := range append(families, clauses...) {
		if !found[k] {
			panic("elixirtable: cook union piece not found: " + k)
		}
	}
	return cook
}

// cookWriteSource is the runtime cook writer (docs/SPEC-TABLES.md §7.6):
// cook_measure, cook_measure_reason and cook_from. Measure comes before write.
// A buffer shorter than the measure writes nothing and returns false. An
// invalid value answers invalid_value, the reference leg's TableRefuseInvalidValue.
const cookWriteSource = `  # ---- cook write (docs/SPEC-TABLES.md §7.6) ----

  def cook_measure(table, value) do
    {size, _} = cook_measure_reason(table, value)
    size
  end

  def cook_measure_reason(table, value) do
    case cook_plan(table, value) do
      {:ok, plan} ->
        # the measure walks the value (docs/SPEC-TABLES.md §7.6): an invalid
        # value is invalid_value, not a size.
        case cook_render(plan, :little) do
          {:ok, bytes} -> {byte_size(bytes), :ok}
          _ -> {-1, :invalid_value}
        end

      {:error, reason} ->
        {-1, reason}
    end
  end

  # cook_from writes one cook (docs/SPEC-TABLES.md §7.6). Measure runs first.
  # A buffer shorter than that measure returns false and is not written. On
  # success the cook occupies the front of the returned binary and the tail of
  # the caller's buffer is kept.
  def cook_from(table, value, buffer, order)
      when is_binary(buffer) and order in [:little, :big] do
    case cook_plan(table, value) do
      {:error, _} ->
        false

      {:ok, plan} ->
        if byte_size(buffer) < plan.total do
          false
        else
          case cook_render(plan, order) do
            {:ok, bytes} ->
              {true, bytes <> binary_part(buffer, plan.total, byte_size(buffer) - plan.total)}

            _ ->
              false
          end
        end
    end
  end

  def cook_from(_table, _value, _buffer, _order), do: false

  defp cook_plan(table, value) do
    try do
      if not is_map(value) do
        throw(:invalid)
      end

      desc = Map.fetch!(@tables, table)
      {body, children} = cook_collect(desc, value, [])

      root = %{
        id: desc.type_id,
        key: {desc.type_id, value},
        kind: :table,
        string: false,
        value: body,
        elem: table,
        desc: desc
      }

      {data, align, laid} = cook_lay([root | Enum.reverse(children)])
      total = 64 + data + length(laid) * 16
      {:ok, %{total: total, data: data, align: align, nodes: laid, desc: desc}}
    catch
      :invalid -> {:error, :invalid_value}
    end
  end

  # cook_collect numbers the nodes a value reaches in §3.1's depth-first
  # first-visit order and answers the value with every blob slot replaced by
  # the node it names, {:cook_blob, n}. A table node is its type and content,
  # walked once however often it is named; A BLOB IS A NODE OF ITS SLOT
  # (docs/SPEC-TABLES.md §2.5, §3.1), so two slots holding equal bytes are two
  # nodes and the record writer finds each slot's own node by its mark.
  defp cook_collect(desc, value, acc) when is_map(value) do
    Enum.reduce(desc.fields, {value, acc}, fn f, {value, acc} ->
      case Map.fetch(value, f.name) do
        {:ok, v} ->
          {v, acc} = cook_collect_field(f, v, acc)
          {Map.put(value, f.name, v), acc}

        :error ->
          {value, acc}
      end
    end)
  end

  defp cook_collect(_desc, _value, _acc), do: throw(:invalid)

  # a cook packs the graph as it finds it (docs/SPEC-TABLES.md §2.5): a blob
  # the tolerant read found named from a second slot is the one node the
  # earlier slot holding those bytes already numbered
  defp cook_collect_field(%{t: :blob, cook_type_id: id} = f, {:blob_shared, v}, acc) do
    case Enum.find(Enum.reverse(acc), fn n -> n.kind == :blob and n.id == id and n.value == v end) do
      nil -> cook_collect_field(f, v, acc)
      n -> {elem(n.key, 1), acc}
    end
  end

  defp cook_collect_field(%{t: :blob, cook_type_id: id} = f, v, acc) when not is_nil(v) do
    if not is_binary(v), do: throw(:invalid)
    slot = {:cook_blob, length(acc)}
    {slot, [%{id: id, key: {id, slot}, kind: :blob, string: f.blob == :string, value: v} | acc]}
  end

  defp cook_collect_field(%{t: :pointer, cook_type_id: id, elem: elem}, v, acc) when is_map(v),
    do: {v, cook_push(acc, id, elem, v)}

  defp cook_collect_field(%{t: :table, elem: elem}, v, acc) when is_map(v),
    do: cook_collect(Map.fetch!(@tables, elem), v, acc)

  defp cook_collect_field(%{t: :list} = f, v, acc) when is_list(v),
    do: Enum.map_reduce(v, acc, fn e, acc -> cook_collect_elem(f, e, acc) end)

  defp cook_collect_field(%{t: :list}, v, _acc) when not is_nil(v), do: throw(:invalid)

  # a MAP's value is its entries ASCENDING BY KEY (docs/SPEC-TABLES.md §2.8):
  # the writer holds the sort (map_entries, the wire's own), so the numbering,
  # the record writer and the extent walk all read the one order
  defp cook_collect_field(%{t: :map} = f, v, acc) when is_map(v),
    do: Enum.map_reduce(map_entries(f, v), acc, fn e, acc -> cook_collect_elem(f, e, acc) end)

  defp cook_collect_field(%{t: :map}, v, _acc) when not is_nil(v), do: throw(:invalid)

  # an enum-keyed array's slots are walked in the key enum's order (§2.4)
  defp cook_collect_field(%{t: :keyed, item: item, key_names: names}, slots, acc)
       when is_map(slots) do
    Enum.reduce(names, {slots, acc}, fn name, {slots, acc} ->
      case Map.fetch(slots, name) do
        {:ok, v} ->
          {v, acc} = cook_collect_field(item, v, acc)
          {Map.put(slots, name, v), acc}

        :error ->
          {slots, acc}
      end
    end)
  end

  defp cook_collect_field(%{t: :union, elem: union}, v, acc), do: cook_collect_arm(union, v, acc)

  defp cook_collect_field(_, v, acc), do: {v, acc}

  defp cook_collect_elem(%{elem_kind: 13, elem: elem}, e, acc) when is_map(e),
    do: cook_collect(Map.fetch!(@tables, elem), e, acc)

  defp cook_collect_elem(%{elem_kind: 17, elem_cook_id: id, elem: elem}, e, acc) when is_map(e),
    do: {e, cook_push(acc, id, elem, e)}

  defp cook_collect_elem(%{elem_kind: 15, elem: union}, e, acc),
    do: cook_collect_arm(union, e, acc)

  defp cook_collect_elem(_, e, acc), do: {e, acc}

  # a union's set arm is a field line where the union sits (§2.6, §3.1)
  defp cook_collect_arm(_union, nil, acc), do: {nil, acc}

  defp cook_collect_arm(union, {arm, payload}, acc) do
    {payload, acc} = cook_collect_field(cook_arm(union, arm), payload, acc)
    {{arm, payload}, acc}
  end

  defp cook_collect_arm(_union, _v, _acc), do: throw(:invalid)

  defp cook_arm(union, arm) do
    case Map.fetch(Map.fetch!(@unions, union).by_name, arm) do
      {:ok, a} -> a
      :error -> throw(:invalid)
    end
  end

  # cook_push numbers a TABLE node once and walks its body the first time
  # only: a node named again is the one record, blobs and all (§3.1). The node
  # is placed before what its body reaches, then given the walked body.
  defp cook_push(acc, id, elem, v) do
    key = {id, v}

    if Enum.any?(acc, fn n -> n.key == key end) do
      acc
    else
      node = %{id: id, key: key, kind: :table, string: false, value: v, elem: elem}
      before = length(acc)
      {body, acc} = cook_collect(Map.fetch!(@tables, elem), v, [node | acc])
      List.update_at(acc, length(acc) - before - 1, &%{&1 | value: body})
    end
  end

  defp cook_lay(nodes) do
    {offset, align, acc} =
      Enum.reduce(nodes, {0, 8, []}, fn n, {offset, align, acc} ->
        case n.kind do
          :blob ->
            start = cook_align(offset, 8)
            size = 8 + byte_size(n.value) + if(n.string, do: 1, else: 0)
            {start + size, align, [Map.merge(n, %{offset: start, size: size, extent: 0}) | acc]}

          :table ->
            # a node aligns to its record's alignment and every array its
            # extent can hold (ir.CookNodeAlign, docs/SPEC-TABLES.md §2.8)
            d = Map.get(n, :desc) || Map.fetch!(@tables, n.elem)
            align = max(align, d.node_align)
            start = cook_align(offset, d.node_align)
            ext = if(d.has_extent, do: cook_extent(d, n.value), else: 0)
            size = d.record + ext

            {start + size, align,
             [Map.merge(n, %{offset: start, size: size, extent: ext, desc: d}) | acc]}
        end
      end)

    {cook_align(offset, align), align, Enum.reverse(acc)}
  end

  # THE NODE EXTENT (docs/SPEC-TABLES.md §2.8, §2.9, §7.2, §7.6; the layout
  # tla/NodeExtent.tla models and internal/tablecook/extent.go writes). A
  # record's lists and maps lay their arrays after its own storage, from
  # desc.record on: field by field in declaration order, each holder's WHOLE
  # array at the running offset rounded to its element's alignment, then,
  # element by element (entry by entry in key order), the arrays those
  # elements hold — PRE-ORDER. One walk measures (buf nil) and writes, so the
  # two cannot drift.
  defp cook_extent(desc, value),
    do: cook_walk_record(desc, value, 0, %{buf: nil, base: 0, at: 0, index: nil, order: nil}).at

  defp cook_carve(desc, value, node_off, index, order, buf) do
    ctx = %{buf: buf, base: node_off + desc.record, at: 0, index: index, order: order}
    cook_walk_record(desc, value, node_off, ctx).buf
  end

  defp cook_walk_record(desc, nil, rec, ctx), do: cook_walk_record(desc, %{}, rec, ctx)

  defp cook_walk_record(desc, value, rec, ctx) do
    if desc.has_extent do
      Enum.reduce(desc.fields, ctx, fn f, ctx -> cook_walk_field(f, value, rec, ctx) end)
    else
      ctx
    end
  end

  # a LIST's or a MAP's sixteen-byte holder, at the field's own offset
  defp cook_walk_field(%{t: :list, array: :list} = f, value, rec, ctx),
    do: cook_walk_holder(f, cook_list(value, f), f.elem_size, f.elem_align, rec + f.cook_off, ctx)

  defp cook_walk_field(%{t: :map} = f, value, rec, ctx),
    do:
      cook_walk_holder(f, cook_list(value, f), f.entry_size, f.entry_align, rec + f.cook_off, ctx)

  # a record held by value, and a fixed or counted array's live elements
  defp cook_walk_field(%{t: :table, elem: elem} = f, value, rec, ctx),
    do: cook_walk_record(Map.fetch!(@tables, elem), Map.get(value, f.name), rec + f.cook_off, ctx)

  defp cook_walk_field(%{t: :list, elem_t: t} = f, value, rec, ctx) when t in [:record, :union] do
    Enum.reduce(Enum.with_index(cook_list(value, f)), ctx, fn {e, i}, ctx ->
      cook_walk_elem(f, e, rec + f.cook_off + i * f.elem_size, ctx)
    end)
  end

  # an enum-keyed array's slots, in the key enum's order (§2.4)
  defp cook_walk_field(%{t: :keyed, item: item} = f, value, rec, ctx) do
    slots = Map.get(value, f.name) || %{}

    Enum.reduce(Enum.with_index(f.key_names), ctx, fn {name, i}, ctx ->
      at = rec + f.cook_off + i * f.elem_size
      cook_walk_field(item, %{item.name => Map.get(slots, name)}, at, ctx)
    end)
  end

  defp cook_walk_field(%{t: :union, elem: union} = f, value, rec, ctx),
    do: cook_walk_union(union, f, Map.get(value, f.name), rec + f.cook_off, ctx)

  defp cook_walk_field(_f, _value, _rec, ctx), do: ctx

  # a union's SET arm, a field at arm_off past the tag (§2.6, §7.2)
  defp cook_walk_union(_union, _f, nil, _at, ctx), do: ctx

  defp cook_walk_union(union, f, {arm, payload}, at, ctx) do
    a = cook_arm(union, arm)
    cook_walk_field(a, %{a.name => payload}, at + f.arm_off, ctx)
  end

  defp cook_walk_union(_union, _f, _v, _at, _ctx), do: throw(:invalid)

  defp cook_walk_elem(%{elem_t: :record, elem: elem}, e, at, ctx),
    do: cook_walk_record(Map.fetch!(@tables, elem), e, at, ctx)

  defp cook_walk_elem(%{elem_t: :union, elem: union} = f, e, at, ctx),
    do: cook_walk_union(union, f, e, at, ctx)

  defp cook_walk_elem(_f, _e, _at, ctx), do: ctx

  # ONE holder: the whole array at the running offset rounded to the element's
  # alignment, the slot's self-relative delta to it (zero when empty) and its
  # int32 count, then, element by element, the arrays each element holds
  defp cook_walk_holder(f, elems, size, align, slot, ctx) do
    start = cook_align(ctx.at, align)
    array = ctx.base + start
    buf = cook_put_holder(f, elems, size, slot, array, ctx)
    ctx = %{ctx | at: start + length(elems) * size, buf: buf}

    Enum.reduce(Enum.with_index(elems), ctx, fn {e, i}, ctx ->
      cook_walk_member(f, e, array + i * size, ctx)
    end)
  end

  defp cook_put_holder(_f, _elems, _size, _slot, _array, %{buf: nil}), do: nil

  defp cook_put_holder(f, elems, size, slot, array, ctx) do
    %{buf: buf, index: index, order: order} = ctx
    buf = if elems == [], do: buf, else: put_i64(buf, slot, array - slot, order)
    buf = put_i32(buf, slot + 8, length(elems), order)

    Enum.reduce(Enum.with_index(elems), buf, fn {e, i}, buf ->
      cook_put_member(f, e, array + i * size, index, order, buf)
    end)
  end

  # a map's member is its generated { key, value } ENTRY record (§2.8)
  defp cook_put_member(%{t: :map, elem: entry}, e, at, index, order, buf),
    do: cook_record(Map.fetch!(@tables, entry), e, at, index, order, buf)

  defp cook_put_member(f, e, at, index, order, buf), do: cook_elem(f, e, at, index, order, buf)

  defp cook_walk_member(%{t: :map, elem: entry}, e, at, ctx),
    do: cook_walk_record(Map.fetch!(@tables, entry), e, at, ctx)

  defp cook_walk_member(f, e, at, ctx), do: cook_walk_elem(f, e, at, ctx)

  defp cook_list(value, f) do
    case Map.get(value, f.name, []) do
      v when is_list(v) -> v
      nil -> []
      _ -> throw(:invalid)
    end
  end

  defp cook_render(plan, order) do
    try do
      data = :binary.copy(<<0>>, plan.data)
      index = Map.new(plan.nodes, fn n -> {n.key, n.offset} end)

      data =
        Enum.reduce(plan.nodes, data, fn n, data ->
          case n.kind do
            :blob ->
              data = put_u32(data, n.offset, byte_size(n.value), order)
              put_raw(data, n.offset + 8, n.value)

            :table ->
              data = cook_record(n.desc, n.value, n.offset, index, order, data)

              if n.extent == 0 and not n.desc.has_extent do
                data
              else
                cook_carve(n.desc, n.value, n.offset, index, order, data)
              end
          end
        end)

      header = cook_header(plan, order)
      dir = cook_directory(plan.nodes, order)
      {:ok, IO.iodata_to_binary([header, data, dir])}
    catch
      :invalid -> :error
    end
  end

  defp cook_header(plan, order) do
    words = [
      @cook_magic,
      @cook_build_version,
      if(order == :big, do: 2, else: 1),
      plan.data,
      length(plan.nodes) * 16,
      plan.align,
      0,
      0
    ]

    for w <- words, into: <<>>, do: cook_u64(w, order)
  end

  defp cook_directory(nodes, order) do
    for n <- nodes, into: <<>>, do: cook_u64(n.offset, order) <> cook_u64(n.id, order)
  end

  defp cook_record(desc, value, base, index, order, buf) do
    if not is_map(value), do: throw(:invalid)

    Enum.reduce(desc.fields, buf, fn f, buf ->
      cook_field(f, value, base, index, order, buf)
    end)
  end

  # a LIST's and a MAP's holder is written by the extent walk, which places
  # the array it points at (§7.6)
  defp cook_field(%{array: :list}, _value, _base, _index, _order, buf), do: buf
  defp cook_field(%{t: :map}, _value, _base, _index, _order, buf), do: buf

  defp cook_field(f, value, base, index, order, buf) do
    # an absent optional reads nil (§2.3)
    present = Map.get(value, f.name) != nil

    buf =
      if f.present_off >= 0 do
        put_u8(buf, base + f.present_off, if(present, do: 1, else: 0))
      else
        buf
      end

    if Map.get(f, :optional, false) and not present do
      cook_absent(f, base, index, order, buf)
    else
      cook_value(f, Map.get(value, f.name, f.default), base, index, order, buf)
    end
  end

  # WHAT THE STORAGE HOLDS IS WHAT IS WRITTEN (§7.6): an absent optional's
  # storage is value-initialized, so a record keeps its declared defaults and
  # an array its value-initialized slots; a scalar's is zero.
  defp cook_absent(%{t: t} = f, base, index, order, buf) when t in [:table, :keyed],
    do: cook_value(f, %{}, base, index, order, buf)

  defp cook_absent(%{t: :list} = f, base, index, order, buf),
    do: cook_value(f, [], base, index, order, buf)

  defp cook_absent(_f, _base, _index, _order, buf), do: buf

  # a blob slot holds the mark cook_collect gave it, {:cook_blob, n}: the
  # node of this slot (§2.5)
  defp cook_value(%{t: :blob, cook_type_id: id, cook_off: off}, v, base, index, order, buf) do
    cook_ref(buf, base + off, id, v, index, order)
  end

  defp cook_value(%{t: :pointer, cook_type_id: id, cook_off: off}, v, base, index, order, buf) do
    cook_ref(buf, base + off, id, v, index, order)
  end

  defp cook_value(%{t: :table, elem: elem, cook_off: off}, v, base, index, order, buf) do
    cook_record(Map.fetch!(@tables, elem), v || %{}, base + off, index, order, buf)
  end

  defp cook_value(
         %{t: :string, cook_off: off, count_off: len_at, size: bound},
         v,
         base,
         _index,
         order,
         buf
       ) do
    cook_text(buf, base + off, base + len_at, v, bound, order)
  end

  defp cook_value(
         %{t: :bytes, cook_off: off, count_off: len_at, size: bound},
         v,
         base,
         _index,
         order,
         buf
       ) do
    cook_text(buf, base + off, base + len_at, v, bound, order)
  end

  defp cook_value(
         %{t: :wstring, cook_off: off, count_off: len_at, size: bound},
         v,
         base,
         _index,
         order,
         buf
       ) do
    cook_units(buf, base + off, base + len_at, v, bound, order)
  end

  defp cook_value(%{t: :enum, cook_off: off} = f, v, base, _index, order, buf) do
    ord = cook_ord(f, v)
    put_uint(buf, base + off, ord, f.enum_width, order)
  end

  # an integer slot is its STORAGE width (§7.2), which is not always the
  # wire's: a bits(N) is four or eight bytes, a flags mask eight
  defp cook_value(%{t: :int, cook_off: off, cook_width: w} = f, v, base, _index, order, buf) do
    if not is_integer(v), do: throw(:invalid)
    if Map.has_key?(f, :min) and (v < f.min or v > f.max), do: throw(:invalid)
    put_int(buf, base + off, v, w, Map.get(f, :signed, false), order)
  end

  defp cook_value(%{t: :float, cook_off: off, width: w}, v, base, _index, order, buf) do
    if not is_number(v), do: throw(:invalid)
    put_float(buf, base + off, v, w, order)
  end

  defp cook_value(%{t: :bool, cook_off: off}, v, base, _index, _order, buf) do
    if not is_boolean(v), do: throw(:invalid)
    put_u8(buf, base + off, if(v, do: 1, else: 0))
  end

  # an enum-keyed array's slots are POSITIONAL (§7.2): variant v's element at
  # (v - 1) elements in, every slot written, a missing one at its default
  defp cook_value(%{t: :keyed, item: item} = f, v, base, index, order, buf) do
    slots = if(is_map(v), do: v, else: throw(:invalid))

    Enum.reduce(Enum.with_index(f.key_names), buf, fn {name, i}, buf ->
      at = base + f.cook_off + i * f.elem_size
      cook_value(item, Map.get(slots, name, item.default), at, index, order, buf)
    end)
  end

  defp cook_value(%{t: :union, elem: union, cook_off: off} = f, v, base, index, order, buf),
    do: cook_union(union, f, v, base + off, index, order, buf)

  defp cook_value(%{t: :list, array: :counted} = f, v, base, index, order, buf) do
    cook_slots(f, v, base, index, order, buf, true)
  end

  defp cook_value(%{t: :list, array: :fixed} = f, v, base, index, order, buf) do
    cook_slots(f, v, base, index, order, buf, false)
  end

  defp cook_value(%{t: t}, v, _base, _index, _order, _buf) when not is_nil(v) and t != nil,
    do: throw(:invalid)

  defp cook_value(_f, _v, _base, _index, _order, buf), do: buf

  defp cook_slots(f, v, base, index, order, buf, counted) do
    elems = if(is_list(v), do: v, else: throw(:invalid))
    # cardBound overwrites :bound with nil; the array maximum is the first bound
    # the descriptor wrote, kept here as the list's own length ceiling when the
    # field named one. A missing ceiling is the element's own.
    ceiling = cook_ceiling(f)

    if ceiling != nil and length(elems) > ceiling, do: throw(:invalid)

    buf =
      if counted and f.count_off >= 0 do
        put_i32(buf, base + f.count_off, length(elems), order)
      else
        buf
      end

    # the storage is allocate-max (§7.2): a record slot past the live count
    # holds the element type's declared defaults, every other one zero
    elems =
      if f.elem_t == :record and ceiling != nil and length(elems) < ceiling,
        do: elems ++ List.duplicate(%{}, ceiling - length(elems)),
        else: elems

    Enum.reduce(Enum.with_index(elems), buf, fn {e, i}, buf ->
      cook_elem(f, e, base + f.cook_off + i * f.elem_size, index, order, buf)
    end)
  end

  # The wire descriptor's bound key is overwritten by cardBound. The array
  # maximum rides on the list clause as the first :bound, which cardBound then
  # replaces. Read the maximum from elem storage: a counted array's ceiling is
  # the declared bound stored beside elem_size when the emitter kept it.
  defp cook_ceiling(f) do
    Map.get(f, :max_count)
  end

  defp cook_elem(%{elem_t: :record, elem: elem}, e, at, index, order, buf) do
    cook_record(Map.fetch!(@tables, elem), e || %{}, at, index, order, buf)
  end

  defp cook_elem(%{elem_t: :pointer, elem_cook_id: id}, e, at, index, order, buf) do
    cook_ref(buf, at, id, e, index, order)
  end

  defp cook_elem(%{elem_t: :enum} = f, e, at, _index, order, buf) do
    put_uint(buf, at, cook_ord(f, e), f.enum_width, order)
  end

  defp cook_elem(%{elem_t: :bool}, e, at, _index, _order, buf) do
    if not is_boolean(e), do: throw(:invalid)
    put_u8(buf, at, if(e, do: 1, else: 0))
  end

  defp cook_elem(%{elem_t: :float, elem_size: w}, e, at, _index, order, buf) do
    if not is_number(e), do: throw(:invalid)
    put_float(buf, at, e, w, order)
  end

  defp cook_elem(%{elem_t: :int, elem_size: w} = f, e, at, _index, order, buf) do
    if not is_integer(e), do: throw(:invalid)
    put_int(buf, at, e, w, Map.get(f, :elem_signed, false), order)
  end

  # string(N)'s pieces (§7.2): char[N + 1], then the int32 used length at four
  defp cook_elem(%{elem_t: :string, size: bound}, e, at, _index, order, buf) do
    cook_text(buf, at, cook_align(at + bound + 1, 4), e, bound, order)
  end

  defp cook_elem(%{elem_t: :union, elem: union} = f, e, at, index, order, buf),
    do: cook_union(union, f, e, at, index, order, buf)

  defp cook_elem(_, _, _, _, _, buf), do: buf

  # A UNION is a TAG beside its arms (§7.2): the tag, the set arm's ordinal
  # plus one, at the union's base at its storage width, and only the set arm's
  # pieces at arm_off. None is tag 0 and writes nothing.
  defp cook_union(_union, _f, nil, _at, _index, _order, buf), do: buf

  defp cook_union(union, f, {arm, payload}, at, index, order, buf) do
    a = cook_arm(union, arm)
    buf = put_uint(buf, at, a.ordinal + 1, f.tag_width, order)
    cook_value(a, payload, at + f.arm_off, index, order, buf)
  end

  defp cook_union(_union, _f, _v, _at, _index, _order, _buf), do: throw(:invalid)

  defp cook_ord(%{ords: _ords}, nil), do: 0

  defp cook_ord(%{ords: ords}, v) when is_binary(v) do
    case Map.fetch(ords, v) do
      {:ok, n} -> n
      :error -> throw(:invalid)
    end
  end

  defp cook_ord(%{ords: ords}, v) when is_integer(v) do
    if v in Map.values(ords) or v == 0, do: v, else: throw(:invalid)
  end

  defp cook_ord(_, _), do: throw(:invalid)

  defp cook_ref(buf, _at, _id, nil, _index, _order), do: buf

  defp cook_ref(buf, at, id, v, index, order) do
    case Map.fetch(index, {id, v}) do
      {:ok, to} when to != at -> put_i64(buf, at, to - at, order)
      _ -> throw(:invalid)
    end
  end

  defp cook_text(buf, at, len_at, v, bound, order) do
    if not is_binary(v) or byte_size(v) > bound, do: throw(:invalid)
    buf = put_raw(buf, at, v)
    put_i32(buf, len_at, byte_size(v), order)
  end

  # wstring(N)'s pieces (§7.2): char16_t[N + 1], each UTF-16 code unit a
  # two-byte scalar in the cook's byte order, then the int32 used length in
  # code units
  defp cook_units(buf, at, len_at, v, bound, order) do
    if not is_binary(v), do: throw(:invalid)
    units = for <<u::little-unsigned-16 <- utf16(v)>>, do: u
    if length(units) > bound, do: throw(:invalid)

    buf =
      Enum.reduce(Enum.with_index(units), buf, fn {u, i}, buf ->
        put_uint(buf, at + 2 * i, u, 2, order)
      end)

    put_i32(buf, len_at, length(units), order)
  end

  defp cook_align(v, a) when a <= 1, do: v
  defp cook_align(v, a), do: div(v + a - 1, a) * a

  defp cook_u64(v, :little), do: <<v::little-unsigned-64>>
  defp cook_u64(v, :big), do: <<v::big-unsigned-64>>

  defp put_u8(buf, at, v), do: put_raw(buf, at, <<v::unsigned-8>>)
  defp put_u32(buf, at, v, :little), do: put_raw(buf, at, <<v::little-unsigned-32>>)
  defp put_u32(buf, at, v, :big), do: put_raw(buf, at, <<v::big-unsigned-32>>)
  defp put_i32(buf, at, v, :little), do: put_raw(buf, at, <<v::little-signed-32>>)
  defp put_i32(buf, at, v, :big), do: put_raw(buf, at, <<v::big-signed-32>>)
  defp put_i64(buf, at, v, :little), do: put_raw(buf, at, <<v::little-signed-64>>)
  defp put_i64(buf, at, v, :big), do: put_raw(buf, at, <<v::big-signed-64>>)

  defp put_uint(buf, at, v, 1, _), do: put_u8(buf, at, v)
  defp put_uint(buf, at, v, 2, :little), do: put_raw(buf, at, <<v::little-unsigned-16>>)
  defp put_uint(buf, at, v, 2, :big), do: put_raw(buf, at, <<v::big-unsigned-16>>)
  defp put_uint(buf, at, v, 4, order), do: put_u32(buf, at, v, order)
  defp put_uint(buf, at, v, 8, :little), do: put_raw(buf, at, <<v::little-unsigned-64>>)
  defp put_uint(buf, at, v, 8, :big), do: put_raw(buf, at, <<v::big-unsigned-64>>)
  # a 128-bit slot (§7.2): the low half first in a little-endian cook, the
  # high half first in a big-endian one, which is the 128-bit integer's order
  defp put_uint(buf, at, v, 16, :little), do: put_raw(buf, at, <<v::little-unsigned-128>>)
  defp put_uint(buf, at, v, 16, :big), do: put_raw(buf, at, <<v::big-unsigned-128>>)
  defp put_uint(_buf, _at, _v, _w, _order), do: throw(:invalid)

  defp put_int(buf, at, v, w, false, order), do: put_uint(buf, at, v, w, order)
  defp put_int(buf, at, v, 1, true, _), do: put_raw(buf, at, <<v::signed-8>>)
  defp put_int(buf, at, v, 2, true, :little), do: put_raw(buf, at, <<v::little-signed-16>>)
  defp put_int(buf, at, v, 2, true, :big), do: put_raw(buf, at, <<v::big-signed-16>>)
  defp put_int(buf, at, v, 4, true, order), do: put_i32(buf, at, v, order)
  defp put_int(buf, at, v, 8, true, :little), do: put_raw(buf, at, <<v::little-signed-64>>)
  defp put_int(buf, at, v, 8, true, :big), do: put_raw(buf, at, <<v::big-signed-64>>)
  defp put_int(buf, at, v, 16, true, :little), do: put_raw(buf, at, <<v::little-signed-128>>)
  defp put_int(buf, at, v, 16, true, :big), do: put_raw(buf, at, <<v::big-signed-128>>)
  defp put_int(_buf, _at, _v, _w, _s, _order), do: throw(:invalid)

  defp put_float(buf, at, v, 4, :little), do: put_raw(buf, at, <<v::little-float-32>>)
  defp put_float(buf, at, v, 4, :big), do: put_raw(buf, at, <<v::big-float-32>>)
  defp put_float(buf, at, v, 8, :little), do: put_raw(buf, at, <<v::little-float-64>>)
  defp put_float(buf, at, v, 8, :big), do: put_raw(buf, at, <<v::big-float-64>>)
  defp put_float(_buf, _at, _v, _w, _order), do: throw(:invalid)

  defp put_raw(buf, at, bytes) do
    n = byte_size(bytes)
    binary_part(buf, 0, at) <> bytes <> binary_part(buf, at + n, byte_size(buf) - at - n)
  end
`
