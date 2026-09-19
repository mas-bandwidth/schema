package rusttable

const graphJsonRuntime = `#[derive(Clone, Copy)]
struct TableJsonLabel {
    reference: i64,
    info: Option<&'static TableTypeInfo>,
    open: bool,
}
struct TableJsonGraphOut {
    counts: std::collections::HashMap<usize, usize>,
    labels: std::collections::HashMap<usize, u64>,
}
impl TableJsonGraphOut {
    unsafe fn new(
        context: TableContext<'_>,
        root: *const u8,
        info: &TableTypeInfo,
    ) -> Option<Self> {
        let mut result = Self {
            counts: std::collections::HashMap::new(),
            labels: std::collections::HashMap::new(),
        };
        let mut open = std::collections::HashSet::new();
        let mut seen = std::collections::HashSet::new();
        let mut stack = vec![(root, info, false)];
        while let Some((node, info, closing)) = stack.pop() {
            if closing {
                open.remove(&(node as usize));
                continue;
            }
            if open.contains(&(node as usize)) {
                return None;
            }
            if !seen.insert(node as usize) {
                continue;
            }
            open.insert(node as usize);
            stack.push((node, info, true));
            let mut edges = Vec::new();
            let mut bad = false;
            unsafe {
                (info.visit_refs)(context, node, &mut |slot, info| {
                    if slot.read() != 0 {
                        if let Some(p) = context.resolve(slot, info) {
                            *result.counts.entry(p as usize).or_default() += 1;
                            edges.push((p, info, false));
                        } else {
                            bad = true;
                        }
                    }
                });
            }
            if bad {
                return None;
            }
            stack.extend(edges.into_iter().rev());
        }
        Some(result)
    }
}
unsafe fn table_json_write_pointer(
    out: &mut TableJsonOut,
    node: *const u8,
    info: &TableTypeInfo,
    depth: i32,
) -> bool {
    unsafe {
        if depth > TABLE_JSON_MAX_DEPTH {
            return false;
        }
        let Some(graph) = out.graph.as_mut() else {
            return false;
        };
        if info.blob != 0 {
            if graph.counts.get(&(node as usize)).copied().unwrap_or(0) > 1 {
                return false;
            }
            let Some(context) = out.context else {
                return false;
            };
            if context.node_extent(node, info).is_none() {
                return false;
            }
            let bytes =
                core::slice::from_raw_parts(node.add(8), (node as *const u32).read() as usize);
            if info.blob == 2 {
                table_json_write_string_bytes(out, bytes);
                return true;
            }
            table_json_write_base64(out, bytes);
            return true;
        }
        if graph.counts.get(&(node as usize)).copied().unwrap_or(0) < 2 {
            return table_json_write_value(out, node, info, depth);
        }
        if let Some(label) = graph.labels.get(&(node as usize)).copied() {
            out.put(b'{');
            out.line(depth + 1);
            out.raw(b"\"&node\": ");
            table_json_write_unsigned(out, label);
            out.line(depth);
            out.put(b'}');
            return true;
        }
        let label = graph.labels.len() as u64 + 1;
        graph.labels.insert(node as usize, label);
        out.definition = label;
        table_json_write_value(out, node, info, depth)
    }
}
fn table_json_label(input: &mut TableJsonIn) -> Option<u64> {
    input.space();
    let begin = input.pos;
    if !(b'1'..=b'9').contains(&input.peek()) {
        input.bad = true;
        return None;
    }
    let mut n = 0u64;
    while input.pos < input.text.len() && input.text[input.pos].is_ascii_digit() {
        let digit = (input.text[input.pos] - b'0') as u64;
        n = match n.checked_mul(10).and_then(|n| n.checked_add(digit)) {
            Some(n) => n,
            None => {
                input.bad = true;
                return None;
            }
        };
        input.pos += 1;
    }
    if input.pos == begin || !matches!(input.peek(), b',' | b'}') {
        input.bad = true;
        return None;
    }
    Some(n)
}
unsafe fn table_json_read_pointer(
    input: &mut TableJsonIn,
    slot: *mut i64,
    info: &'static TableTypeInfo,
    depth: i32,
    report: &mut TableReport,
) -> bool {
    unsafe {
        if info.blob != 0 {
            return table_json_read_blob(input, slot, info, report);
        }
        if input.arena.is_none() || depth + 1 > TABLE_JSON_MAX_DEPTH || input.peek() != b'{' {
            input.bad = true;
            return false;
        }
        let begin = input.pos;
        input.pos += 1;
        if input.peek() == b'}' {
            input.pos += 1;
            let (_, reference) = input.arena.as_deref_mut().unwrap().alloc_info(info);
            slot.write(reference);
            return true;
        }
        let mut key = TableJsonKey::new();
        if !table_json_scan_key(input, &mut key, report) || input.peek() != b':' {
            input.bad = true;
            return false;
        }
        input.pos += 1;
        if key.as_str() != "&node" {
            input.pos = begin;
            let (node, reference) = input.arena.as_deref_mut().unwrap().alloc_info(info);
            slot.write(reference);
            return table_json_read_table(input, node, info, depth + 1, report);
        }
        let Some(label) = table_json_label(input) else {
            return false;
        };
        let had_comma = input.peek() == b',';
        if had_comma {
            input.pos += 1;
        }
        let bare = input.peek() == b'}';
        let previous = input.labels.get(&label).copied();
        if bare {
            let Some(previous) = previous else {
                input.bad = true;
                return false;
            };
            if previous.open {
                input.bad = true;
                return false;
            }
            input.pos += 1;
            slot.write(0);
            if let Some(target) = previous.info {
                if core::ptr::eq(target, info) {
                    slot.write(previous.reference);
                } else {
                    report.kind_mismatch += 1;
                }
            }
            return true;
        }
        if previous.is_some() || !had_comma {
            input.bad = true;
            return false;
        }
        let (node, reference) = input.arena.as_deref_mut().unwrap().alloc_info(info);
        slot.write(reference);
        input.labels.insert(
            label,
            TableJsonLabel {
                reference,
                info: Some(info),
                open: true,
            },
        );
        if !table_json_read_table_keys(input, node, info, depth + 1, report) {
            return false;
        }
        input.labels.get_mut(&label).unwrap().open = false;
        true
    }
}
`
