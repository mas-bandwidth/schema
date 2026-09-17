package rusttable

const builderRuntime = `// Shared by nested readers during one exclusive builder decode. The arena
// owns every resident record; its allocations keep addresses through growth.
struct TableDecodeBuilder {
    arena: *mut TableArena,
    refused: core::cell::Cell<Option<TableRefuseReason>>,
}
impl TableReader<'_> {
    pub fn builder_refused(&self) -> bool {
        self.builder.is_some_and(|b| b.refused.get().is_some())
    }
}
pub(crate) fn table_graph_load_builder<T: TableRecord>(
    builder: &mut TableBuilder<T>,
    bytes: &[u8],
    report: &mut TableReport,
    lookup: fn(u64) -> Option<&'static TableTypeInfo>,
) -> Result<(), TableLoadError> {
    *builder = TableBuilder::new();
    let result = table_graph_decode_builder(builder, bytes, report, lookup);
    if result.is_err() {
        *builder = TableBuilder::new();
    }
    result
}
fn table_graph_decode_builder<T: TableRecord>(
    builder: &mut TableBuilder<T>,
    bytes: &[u8],
    report: &mut TableReport,
    lookup: fn(u64) -> Option<&'static TableTypeInfo>,
) -> Result<(), TableLoadError> {
    let mut root = TableReader::open(bytes).map_err(|verdict| {
        report.verdict = verdict;
        if verdict == TableOpenVerdict::Refused {
            TableLoadError::Refused(TableRefuseReason::UnknownForm)
        } else {
            report.malformed = true;
            TableLoadError::Damaged
        }
    })?;
    let scan_start = TableNodeScan::begin(root);
    let mut scan = scan_start;
    let mut entries = vec![TableNodeDirEntry {
        offset: builder.root.value as u64,
        type_id: T::table_info().id,
    }];
    let (mut unknown, mut invalid_text) = (0, false);
    while let Some((id, body)) = scan.next() {
        let mut handle = u64::MAX;
        if let Some(info) = lookup(id) {
            if info.blob == 0 {
                handle = unsafe { builder.arena.alloc_info(info) }.1 as u64;
            } else if info.blob == 2
                && (body.buffer.contains(&0) || core::str::from_utf8(body.buffer).is_err())
            {
                invalid_text = true;
            } else {
                handle = unsafe { builder.arena.alloc_blob_info(info, body.buffer) }
                    .map_err(TableLoadError::Refused)? as u64;
            }
        } else {
            unknown += 1;
        }
        entries.push(TableNodeDirEntry {
            offset: handle,
            type_id: id,
        });
    }
    let good = scan.whole();
    if good {
        report.unknown += unknown;
        report.malformed |= invalid_text;
    } else {
        report.malformed = true;
    }
    let context = TableDecodeBuilder {
        arena: &mut builder.arena,
        refused: core::cell::Cell::new(None),
    };
    let nodes = TableNodeMap {
        entries: &entries,
        good,
    };
    root.nodes = Some(nodes);
    root.builder = Some(&context);
    if good {
        scan = scan_start;
        let mut index = 1;
        while let Some((id, mut body)) = scan.next() {
            if let Some(info) = lookup(id).filter(|i| i.blob == 0) {
                body.nodes = Some(nodes);
                body.builder = Some(&context);
                let destination = builder
                    .arena
                    .resolve(entries[index].offset as i64, info)
                    .unwrap() as *mut u8;
                unsafe {
                    (info.load_body)(&mut body, report, destination);
                }
                if let Some(reason) = context.refused.get() {
                    return Err(TableLoadError::Refused(reason));
                }
            }
            index += 1;
        }
    }
    let destination = builder
        .arena
        .resolve(builder.root.value, T::table_info())
        .unwrap() as *mut u8;
    let ok = unsafe { (T::table_info().load_body)(&mut root, report, destination) };
    if let Some(reason) = context.refused.get() {
        return Err(TableLoadError::Refused(reason));
    }
    report.verdict = if ok {
        TableOpenVerdict::Ok
    } else {
        TableOpenVerdict::BodyStopped
    };
    Ok(())
}
impl TableArena {
    unsafe fn sequence_drop_last(&mut self, slot: *mut u8) -> bool {
        unsafe {
            let count = slot.add(8) as *mut i32;
            let Some(seq) = self.sequence_mut((slot as *const i64).read()) else {
                return false;
            };
            let Some(segment) = seq.segments.last_mut() else {
                return false;
            };
            if segment.used == 0 || seq.count != count.read() {
                return false;
            }
            let bit = 1 << (segment.used - 1);
            if segment.live & bit == 0 {
                return false;
            }
            segment.live &= !bit;
            seq.count -= 1;
            count.write(count.read() - 1);
            true
        }
    }
}
`
