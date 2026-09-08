package rusttable

const blobRuntime = `/// Blob headers are opaque. Their bytes are borrowed through the owning arena
/// or region, so a copied header can never manufacture an unbounded slice.
#[repr(C)]
#[derive(Clone, Copy, Default)]
pub struct TableBytes {
    length: u32,
    reserved: u32,
}
#[repr(C)]
#[derive(Clone, Copy, Default)]
pub struct TableString {
    length: u32,
    reserved: u32,
}
impl TableBytes {
    pub fn len(&self) -> usize {
        self.length as usize
    }
    pub fn is_empty(&self) -> bool {
        self.length == 0
    }
}
impl TableString {
    pub fn len(&self) -> usize {
        self.length as usize
    }
    pub fn is_empty(&self) -> bool {
        self.length == 0
    }
}
unsafe impl TableRecord for TableBytes {
    fn table_info() -> &'static TableTypeInfo {
        table_bytes_table_type()
    }
}
unsafe impl TableRecord for TableString {
    fn table_info() -> &'static TableTypeInfo {
        table_string_table_type()
    }
}
const fn table_blob_id(name: &[u8]) -> u64 {
    let mut h = 0xcbf29ce484222325u64;
    let mut i = 0;
    while i < name.len() {
        h = (h ^ name[i] as u64).wrapping_mul(0x100000001b3);
        i += 1;
    }
    h
}
macro_rules! table_blob_info {
    ($function:ident,$name:literal,$blob:literal) => {
        pub fn $function() -> &'static TableTypeInfo {
            static INFO: TableTypeInfo = TableTypeInfo {
                wire_extent: None,
                name: $name,
                id: table_blob_id($name.as_bytes()),
                blob: $blob,
                align: 8,
                size: 8,
                initialize: |p| unsafe {
                    core::ptr::write_bytes(p, 0, 8);
                },
                reset: |p| unsafe {
                    core::ptr::write_bytes(p, 0, 8);
                },
                save_body: |w, p| unsafe {
                    let n = (p as *const u32).read() as usize;
                    w.raw(core::slice::from_raw_parts(p.add(8), n));
                    !w.overflow
                },
                load_body: |_, _, _| false,
                visit_refs: |_, _, _| {},
                rewrite_refs: |_, _, _| {},
                num_fields: 0,
                fields: &[],
                doc: TABLE_DOC_NONE,
                num_tags: 0,
                tags: None,
            };
            &INFO
        }
    };
}
table_blob_info!(table_bytes_table_type, "bytes", 1);
table_blob_info!(table_string_table_type, "string", 2);
fn table_blob_extent(length: usize, info: &TableTypeInfo) -> Result<usize, TableRefuseReason> {
    if length > u32::MAX as usize {
        return Err(TableRefuseReason::BlobOverSizeCap);
    }
    8usize
        .checked_add(length)
        .and_then(|n| n.checked_add(usize::from(info.blob == 2)))
        .ok_or(TableRefuseReason::BadLayout)
}
fn table_node_load_extent(
    info: &TableTypeInfo,
    body: &TableReader,
) -> Result<usize, TableRefuseReason> {
    if info.blob != 0 {
        table_blob_extent(body.buffer.len(), info)
    } else {
        let mut at = info.size as usize;
        if let Some(extent) = info.wire_extent {
            extent(*body, &mut at)?;
        }
        Ok(at)
    }
}
impl TableArena {
    unsafe fn alloc_blob_info(
        &mut self,
        info: &'static TableTypeInfo,
        bytes: &[u8],
    ) -> Result<i64, TableRefuseReason> {
        let extent = table_blob_extent(bytes.len(), info)?;
        let mut words = self.storage.allocate(extent);
        let base = words.as_mut_ptr() as *mut u8;
        unsafe {
            (base as *mut u32).write(bytes.len() as u32);
            core::ptr::copy_nonoverlapping(bytes.as_ptr(), base.add(8), bytes.len());
        }
        let handle = self.handle(self.allocations.len());
        self.allocations.push(TableAllocation { words, info });
        Ok(handle)
    }
    pub fn alloc_bytes(&mut self, bytes: &[u8]) -> Result<TableRef<TableBytes>, TableRefuseReason> {
        Ok(TableRef {
            value: unsafe { self.alloc_blob_info(table_bytes_table_type(), bytes)? },
            marker: core::marker::PhantomData,
        })
    }
    pub fn alloc_string(&mut self, text: &str) -> Result<TableRef<TableString>, TableRefuseReason> {
        if text.as_bytes().contains(&0) {
            return Err(TableRefuseReason::BadLayout);
        }
        Ok(TableRef {
            value: unsafe { self.alloc_blob_info(table_string_table_type(), text.as_bytes())? },
            marker: core::marker::PhantomData,
        })
    }
    pub fn bytes<T: TableRecord>(&self, reference: TableRef<T>) -> Option<&[u8]> {
        self.context().blob_bytes(&reference)
    }
    pub fn string(&self, reference: TableRef<TableString>) -> Option<&str> {
        core::str::from_utf8(self.bytes(reference)?).ok()
    }
}
impl<'a> TableContext<'a> {
    fn available(self, node: *const u8, info: &TableTypeInfo) -> Option<usize> {
        match self.source {
            TableSource::Arena(arena) => arena.available(node, info),
            TableSource::Region(data) => {
                let offset = (node as usize).checked_sub(data.as_ptr() as usize)?;
                if offset % info.align as usize != 0 {
                    return None;
                }
                data.len().checked_sub(offset)
            }
        }
    }
    fn node_extent(self, node: *const u8, info: &TableTypeInfo) -> Option<usize> {
        if info.blob == 0 {
            let mut pack = TablePack {
                base: core::ptr::null_mut(),
                at: info.size as usize,
                numbering: None,
                directory: &[],
            };
            unsafe {
                table_pack_record(self, node, core::ptr::null_mut(), info, &mut pack, true).ok()?;
            }
            return Some(pack.at);
        }
        let available = self.available(node, info)?;
        if available < 8 {
            return None;
        }
        let extent =
            table_blob_extent(unsafe { (node as *const u32).read() } as usize, info).ok()?;
        (extent <= available).then_some(extent)
    }
    pub fn blob_bytes<T: TableRecord>(self, reference: &TableRef<T>) -> Option<&'a [u8]> {
        let info = T::table_info();
        if info.blob == 0 {
            return None;
        }
        let node = self.resolve(&reference.value, info)?;
        self.node_extent(node, info)?;
        Some(unsafe {
            core::slice::from_raw_parts(node.add(8), (node as *const u32).read() as usize)
        })
    }
}
impl<T: TableRecord> TableRegion<'_, '_, T> {
    pub fn bytes<U: TableRecord>(&self, reference: &TableRef<U>) -> Option<&[u8]> {
        self.context().blob_bytes(reference)
    }
    pub fn string(&self, reference: &TableRef<TableString>) -> Option<&str> {
        core::str::from_utf8(self.bytes(reference)?).ok()
    }
}
impl<T: TableRecord> TableOwnedRegion<T> {
    pub fn bytes<U: TableRecord>(&self, reference: &TableRef<U>) -> Option<&[u8]> {
        TableContext {
            source: TableSource::Region(unsafe {
                core::slice::from_raw_parts(self.words.as_ptr() as *const u8, self.bytes)
            }),
        }
        .blob_bytes(reference)
    }
    pub fn string(&self, reference: &TableRef<TableString>) -> Option<&str> {
        core::str::from_utf8(self.bytes(reference)?).ok()
    }
}
unsafe fn table_json_read_blob(
    input: &mut TableJsonIn,
    slot: *mut i64,
    info: &'static TableTypeInfo,
    report: &mut TableReport,
) -> bool {
    if input.arena.is_none() {
        input.bad = true;
        return false;
    }
    let mut bytes = Vec::new();
    if info.blob == 2 {
        bytes.resize(input.text.len() - input.pos, 0);
        let Some(n) = table_json_scan_string(
            input,
            TableJsonSink::Storage(bytes.as_mut_ptr(), bytes.len()),
            report,
        ) else {
            return false;
        };
        bytes.truncate(n as usize);
    } else {
        if input.peek() != b'"' {
            input.bad = true;
            return false;
        }
        input.pos += 1;
        let mut accumulator = 0u32;
        let mut held = 0;
        let mut malformed = false;
        loop {
            let Some(&c) = input.text.get(input.pos) else {
                input.bad = true;
                return false;
            };
            input.pos += 1;
            if c == b'"' {
                break;
            }
            if c == b'=' || malformed {
                continue;
            }
            let at = TABLE_JSON_BASE64_DECODE[c as usize];
            if at < 0 {
                malformed = true;
                continue;
            }
            accumulator = (accumulator << 6) | at as u32;
            held += 6;
            if held >= 8 {
                held -= 8;
                bytes.push((accumulator >> held) as u8);
            }
        }
        if malformed {
            report.kind_mismatch += 1;
            unsafe { slot.write(0) };
            return true;
        }
    }
    match unsafe {
        input
            .arena
            .as_deref_mut()
            .unwrap()
            .alloc_blob_info(info, &bytes)
    } {
        Ok(reference) => {
            unsafe { slot.write(reference) };
            true
        }
        Err(_) => {
            input.bad = true;
            false
        }
    }
}
`
