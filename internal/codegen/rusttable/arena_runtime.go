package rusttable

// The authoring arena may allocate. Region measurement and loading borrow all
// storage from the caller and never enter the allocator.
const arenaRuntime = `/// Caller-owned, sixteen-aligned storage for native Rust records. Padding in
/// a native struct may be uninitialized even after every field is written.
/// Keeping the backing words MaybeUninit makes that fact part of the type.
pub type TableStorageWord = core::mem::MaybeUninit<u128>;

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum TableRefuseReason {
    Ok,
    NotACook,
    ForeignOrder,
    WrongBuildVersion,
    ReservedNotZero,
    BadAlignment,
    Truncated,
    UnalignedBase,
    BadLayout,
    UnknownForm,
    CountOverLength,
    CountOverExtentCap,
    BlobOverSizeCap,
    DataCycle,
}
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum TableLoadError {
    Refused(TableRefuseReason),
    Damaged,
    BufferTooSmall,
}

/// Eight-byte reference. An arena holds a stable allocation handle; a loaded
/// region holds a signed displacement from this slot. Resolve through its owner.
#[repr(transparent)]
pub struct TableRef<T> {
    pub(crate) value: i64,
    marker: core::marker::PhantomData<fn() -> T>,
}
impl<T> TableRef<T> {
    pub const NULL: Self = Self {
        value: 0,
        marker: core::marker::PhantomData,
    };
    pub fn is_null(self) -> bool {
        self.value == 0
    }
}
impl<T> Copy for TableRef<T> {}
impl<T> Clone for TableRef<T> {
    fn clone(&self) -> Self {
        *self
    }
}
impl<T> Default for TableRef<T> {
    fn default() -> Self {
        Self::NULL
    }
}
impl<T> PartialEq for TableRef<T> {
    fn eq(&self, other: &Self) -> bool {
        self.value == other.value
    }
}
impl<T> Eq for TableRef<T> {}
impl<T> core::fmt::Debug for TableRef<T> {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        f.debug_tuple("TableRef").field(&self.value).finish()
    }
}

/// Generated records implement this contract; user implementations must supply
/// descriptors whose size, alignment, initialization and callbacks match Self.
/// # Safety
/// Self is Copy, contains no borrowed or owning pointers, and its descriptor's
/// callbacks accept exactly initialized Self storage. References are TableRef.
pub unsafe trait TableRecord: Copy + Default + Send + Sync + 'static {
    fn table_info() -> &'static TableTypeInfo;
}

// Slabs retain their Vec allocations while metadata moves. Spans never own or
// resize storage; the arena owns every span until Lock consumes it.
struct TableSpan {
    pointer: *mut TableStorageWord,
    bytes: usize,
}
impl TableSpan {
    fn as_ptr(&self) -> *const TableStorageWord {
        self.pointer
    }
    fn as_mut_ptr(&mut self) -> *mut TableStorageWord {
        self.pointer
    }
}
// SAFETY: spans are private and used only inside their owning arena. Mutation
// requires its exclusive borrow; inserted values are Send + Sync and Copy.
unsafe impl Send for TableSpan {}
unsafe impl Sync for TableSpan {}
#[derive(Default)]
struct TableSlabs {
    slabs: Vec<Vec<TableStorageWord>>,
    large: Vec<Vec<TableStorageWord>>,
    used: usize,
}
impl TableSlabs {
    const WORDS: usize = 65536 / 16;
    fn allocate(&mut self, bytes: usize) -> TableSpan {
        let words = bytes.max(1).div_ceil(16);
        if words > Self::WORDS {
            let mut span = vec![TableStorageWord::zeroed(); words];
            let pointer = span.as_mut_ptr();
            self.large.push(span);
            return TableSpan {
                pointer,
                bytes: words * 16,
            };
        }
        if self.slabs.is_empty() || self.used + words > Self::WORDS {
            self.slabs
                .push(vec![TableStorageWord::zeroed(); Self::WORDS]);
            self.used = 0;
        }
        let pointer = unsafe { self.slabs.last_mut().unwrap().as_mut_ptr().add(self.used) };
        self.used += words;
        TableSpan {
            pointer,
            bytes: words * 16,
        }
    }
}
struct TableAllocation {
    words: TableSpan,
    info: &'static TableTypeInfo,
}
/// A private allocation front. Workers can move to separate threads and join
/// after filling; every node and element handle survives the join unchanged.
pub struct TableArena {
    owner: u32,
    workers: std::collections::HashMap<u32, TableArena>,
    storage: TableSlabs,
    sequences: Vec<TableSequenceAllocation>,
    allocations: Vec<TableAllocation>,
}
impl Default for TableArena {
    fn default() -> Self {
        static NEXT: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(1);
        let owner = NEXT
            .fetch_update(
                std::sync::atomic::Ordering::Relaxed,
                std::sync::atomic::Ordering::Relaxed,
                |n| (n < 0x7fffffff).then_some(n + 1),
            )
            .expect("table worker identity exhausted");
        Self {
            owner,
            workers: std::collections::HashMap::new(),
            storage: TableSlabs::default(),
            sequences: Vec::new(),
            allocations: Vec::new(),
        }
    }
}
impl TableArena {
    pub fn new() -> Self {
        Self::default()
    }
    /// Creating a worker is the only synchronized operation. Allocation within
    /// its slabs uses only that worker's exclusive ownership.
    pub fn worker(&self) -> Self {
        Self::new()
    }
    /// Call after the worker thread has joined. Its storage never moves.
    pub fn join(&mut self, mut worker: Self) {
        self.workers.extend(worker.workers.drain());
        self.workers.insert(worker.owner, worker);
    }
    fn handle(&self, index: usize) -> i64 {
        assert!(
            index < u32::MAX as usize,
            "table worker handle capacity exhausted"
        );
        ((self.owner as i64) << 32) | (index as i64 + 1)
    }
    fn index(value: i64) -> Option<usize> {
        (value as u32 as usize).checked_sub(1)
    }
    fn owner(&self, value: i64) -> Option<&Self> {
        let id = (value as u64 >> 32) as u32;
        if id == self.owner {
            Some(self)
        } else {
            self.workers.get(&id)
        }
    }
    fn owner_mut(&mut self, value: i64) -> Option<&mut Self> {
        let id = (value as u64 >> 32) as u32;
        if id == self.owner {
            Some(self)
        } else {
            self.workers.get_mut(&id)
        }
    }
    fn sequence(&self, value: i64) -> Option<&TableSequenceAllocation> {
        self.owner(value)?.sequences.get(Self::index(value)?)
    }
    fn sequence_mut(&mut self, value: i64) -> Option<&mut TableSequenceAllocation> {
        self.owner_mut(value)?
            .sequences
            .get_mut(Self::index(value)?)
    }
    pub fn alloc<T: TableRecord>(&mut self) -> TableRef<T> {
        let (_, value) = unsafe { self.alloc_info(T::table_info()) };
        TableRef {
            value,
            marker: core::marker::PhantomData,
        }
    }
    pub fn get<T: TableRecord>(&self, reference: TableRef<T>) -> Option<&T> {
        Some(unsafe { &*(self.resolve(reference.value, T::table_info())? as *const T) })
    }
    pub fn get_mut<T: TableRecord>(&mut self, reference: TableRef<T>) -> Option<&mut T> {
        let a = self
            .owner_mut(reference.value)?
            .allocations
            .get_mut(Self::index(reference.value)?)?;
        if !core::ptr::eq(a.info, T::table_info()) {
            return None;
        }
        Some(unsafe { &mut *(a.words.as_mut_ptr() as *mut T) })
    }
    fn resolve(&self, value: i64, info: &'static TableTypeInfo) -> Option<*const u8> {
        let a = self.owner(value)?.allocations.get(Self::index(value)?)?;
        if !core::ptr::eq(a.info, info) {
            return None;
        }
        Some(a.words.as_ptr() as *const u8)
    }
    unsafe fn alloc_info(&mut self, info: &'static TableTypeInfo) -> (*mut u8, i64) {
        assert!(info.align <= 16);
        let mut words = self.storage.allocate(info.size as usize);
        let p = words.as_mut_ptr() as *mut u8;
        unsafe {
            (info.initialize)(p);
        }
        let handle = self.handle(self.allocations.len());
        self.allocations.push(TableAllocation { words, info });
        (p, handle)
    }
    fn available(&self, node: *const u8, info: &TableTypeInfo) -> Option<usize> {
        self.allocations
            .iter()
            .find(|a| a.words.as_ptr() as *const u8 == node && core::ptr::eq(a.info, info))
            .map(|a| a.words.bytes)
            .or_else(|| self.workers.values().find_map(|w| w.available(node, info)))
    }
    pub fn context(&self) -> TableContext<'_> {
        TableContext {
            source: TableSource::Arena(self),
        }
    }
}

pub struct TableBuilder<T: TableRecord> {
    arena: TableArena,
    root: TableRef<T>,
}
impl<T: TableRecord> Default for TableBuilder<T> {
    fn default() -> Self {
        Self::new()
    }
}
impl<T: TableRecord> TableBuilder<T> {
    pub fn new() -> Self {
        let mut arena = TableArena::new();
        let root = arena.alloc();
        Self { arena, root }
    }
    pub fn root(&self) -> &T {
        self.arena.get(self.root).unwrap()
    }
    pub fn root_mut(&mut self) -> &mut T {
        self.arena.get_mut(self.root).unwrap()
    }
    pub fn root_ref(&self) -> TableRef<T> {
        self.root
    }
    pub fn arena(&self) -> &TableArena {
        &self.arena
    }
    pub fn arena_mut(&mut self) -> &mut TableArena {
        &mut self.arena
    }
    pub fn context(&self) -> TableContext<'_> {
        self.arena.context()
    }
    pub fn lock(self) -> Result<TableOwnedRegion<T>, TableRefuseReason> {
        let mut numbering = TableNumbering::new(
            self.context(),
            self.root() as *const T as *const u8,
            T::table_info(),
        )?;
        for e in &mut numbering.entries {
            e.size = numbering
                .context
                .node_extent(e.node, e.info)
                .ok_or(TableRefuseReason::BadLayout)?;
        }
        let mut bytes = 0usize;
        let mut directory = Vec::with_capacity(numbering.entries.len());
        for e in &numbering.entries {
            bytes = table_align(bytes, crate::TABLE_REGION_ALIGNMENT)
                .ok_or(TableRefuseReason::BadLayout)?;
            directory.push(TableNodeDirEntry {
                offset: bytes as u64,
                type_id: e.info.id,
            });
            bytes = bytes
                .checked_add(
                    table_align(e.size, crate::TABLE_REGION_ALIGNMENT)
                        .ok_or(TableRefuseReason::BadLayout)?,
                )
                .ok_or(TableRefuseReason::BadLayout)?;
        }
        let mut words = vec![TableStorageWord::zeroed(); bytes.max(1).div_ceil(16)];
        let base = words.as_mut_ptr() as *mut u8;
        for (i, e) in numbering.entries.iter().enumerate() {
            let offset = directory[i].offset as usize;
            let destination = unsafe { base.add(offset) };
            let copied = if e.info.blob != 0 {
                e.size
            } else {
                e.info.size as usize
            };
            unsafe {
                core::ptr::copy_nonoverlapping(e.node, destination, copied);
            }
            let mut pack = TablePack {
                base,
                at: offset + e.info.size as usize,
                numbering: Some(&numbering),
                directory: &directory,
            };
            if e.info.blob == 0 {
                unsafe {
                    table_pack_record(
                        numbering.context,
                        e.node,
                        destination,
                        e.info,
                        &mut pack,
                        true,
                    )?;
                }
            }
        }
        Ok(TableOwnedRegion {
            words,
            bytes,
            directory,
            marker: core::marker::PhantomData,
        })
    }
}

#[repr(C)]
#[derive(Clone, Copy, Debug, Default)]
pub struct TableNodeDirEntry {
    pub offset: u64,
    pub type_id: u64,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct TableLoadSize {
    pub data_bytes: usize,
    pub attribution_bytes: usize,
}
impl TableLoadSize {
    pub fn directory_entries(self) -> usize {
        self.attribution_bytes / core::mem::size_of::<TableNodeDirEntry>()
    }
}

pub struct TableRegion<'a, 'd, T: TableRecord> {
    data: &'a [u8],
    directory: &'d [TableNodeDirEntry],
    marker: core::marker::PhantomData<T>,
}
impl<'a, 'd, T: TableRecord> TableRegion<'a, 'd, T> {
    pub fn root(&self) -> &T {
        unsafe { &*(self.data.as_ptr() as *const T) }
    }
    pub fn get<U: TableRecord>(&self, reference: &TableRef<U>) -> Option<&U> {
        let p = self.context().resolve(&reference.value, U::table_info())?;
        Some(unsafe { &*(p as *const U) })
    }
    pub fn context(&self) -> TableContext<'_> {
        TableContext {
            source: TableSource::Region(self.data),
        }
    }
    pub fn data_bytes(&self) -> usize {
        self.data.len()
    }
    /// Keep the data view while returning the directory to its caller. No
    /// reading operation needs attribution after references are resolved.
    pub fn release_attribution(self) -> TableReadView<'a, T> {
        TableReadView {
            data: self.data,
            marker: core::marker::PhantomData,
        }
    }

    pub fn directory(&self) -> &[TableNodeDirEntry] {
        self.directory
    }
}
/// A validated data region with no borrow of its temporary node directory.
pub struct TableReadView<'a, T: TableRecord> {
    data: &'a [u8],
    marker: core::marker::PhantomData<T>,
}
impl<'a, T: TableRecord> TableReadView<'a, T> {
    pub fn root(&self) -> &T {
        unsafe { &*(self.data.as_ptr() as *const T) }
    }
    pub fn context(&self) -> TableContext<'_> {
        TableContext {
            source: TableSource::Region(self.data),
        }
    }
    pub fn get<U: TableRecord>(&self, reference: &TableRef<U>) -> Option<&U> {
        let p = self.context().resolve(&reference.value, U::table_info())?;
        Some(unsafe { &*(p as *const U) })
    }
    pub fn data_bytes(&self) -> usize {
        self.data.len()
    }
    pub fn bytes<U: TableRecord>(&self, reference: &TableRef<U>) -> Option<&[u8]> {
        self.context().blob_bytes(reference)
    }
    pub fn string(&self, reference: &TableRef<TableString>) -> Option<&str> {
        core::str::from_utf8(self.bytes(reference)?).ok()
    }
}
pub struct TableOwnedRegion<T: TableRecord> {
    words: Vec<TableStorageWord>,
    bytes: usize,
    directory: Vec<TableNodeDirEntry>,
    marker: core::marker::PhantomData<T>,
}
impl<T: TableRecord> TableOwnedRegion<T> {
    pub fn region(&self) -> TableRegion<'_, '_, T> {
        let data =
            unsafe { core::slice::from_raw_parts(self.words.as_ptr() as *const u8, self.bytes) };
        TableRegion {
            data,
            directory: &self.directory,
            marker: core::marker::PhantomData,
        }
    }
    pub fn root(&self) -> &T {
        unsafe { &*(self.words.as_ptr() as *const T) }
    }
    pub fn context(&self) -> TableContext<'_> {
        TableContext {
            source: TableSource::Region(unsafe {
                core::slice::from_raw_parts(self.words.as_ptr() as *const u8, self.bytes)
            }),
        }
    }
    pub fn get<U: TableRecord>(&self, reference: &TableRef<U>) -> Option<&U> {
        let p = self.context().resolve(&reference.value, U::table_info())?;
        Some(unsafe { &*(p as *const U) })
    }
}
#[derive(Clone, Copy)]
enum TableSource<'a> {
    Arena(&'a TableArena),
    Region(&'a [u8]),
}
#[derive(Clone, Copy)]
pub struct TableContext<'a> {
    source: TableSource<'a>,
}
impl TableContext<'_> {
    fn resolve(self, slot: *const i64, info: &'static TableTypeInfo) -> Option<*const u8> {
        let value = unsafe { slot.read() };
        if value == 0 {
            return None;
        }
        match self.source {
            TableSource::Arena(arena) => arena.resolve(value, info),
            TableSource::Region(data) => {
                let slot_offset = (slot as usize).checked_sub(data.as_ptr() as usize)?;
                if slot_offset.checked_add(8)? > data.len() {
                    return None;
                }
                let offset = slot_offset.checked_add_signed(isize::try_from(value).ok()?)?;
                if offset.checked_add(info.size as usize)? > data.len()
                    || offset % info.align as usize != 0
                {
                    return None;
                }
                Some(unsafe { data.as_ptr().add(offset) })
            }
        }
    }
}
struct TableNumberedNode {
    size: usize,
    node: *const u8,
    info: &'static TableTypeInfo,
}
pub struct TableNumbering<'a> {
    context: TableContext<'a>,
    entries: Vec<TableNumberedNode>,
    indices: std::collections::HashMap<usize, u64>,
}
impl<'a> TableNumbering<'a> {
    fn new(
        context: TableContext<'a>,
        root: *const u8,
        info: &'static TableTypeInfo,
    ) -> Result<Self, TableRefuseReason> {
        let mut out = Self {
            context,
            entries: Vec::new(),
            indices: std::collections::HashMap::new(),
        };
        let mut open = std::collections::HashSet::new();
        let mut stack = vec![(root, info, false)];
        while let Some((node, info, closing)) = stack.pop() {
            let key = node as usize;
            if closing {
                open.remove(&key);
                continue;
            }
            if open.contains(&key) {
                return Err(TableRefuseReason::DataCycle);
            }
            if out.indices.contains_key(&key) {
                continue;
            }
            let size = if info.blob != 0 {
                context
                    .node_extent(node, info)
                    .ok_or(TableRefuseReason::BadLayout)?
            } else {
                info.size as usize
            };
            out.entries.push(TableNumberedNode { node, info, size });
            out.indices.insert(key, out.entries.len() as u64);
            open.insert(key);
            stack.push((node, info, true));
            let mut edges = Vec::new();
            let mut bad = false;
            unsafe {
                (info.visit_refs)(context, node, &mut |slot, target| {
                    if *slot != 0 {
                        match context.resolve(slot, target) {
                            Some(p) => edges.push((p, target, false)),
                            None => bad = true,
                        }
                    }
                });
            }
            if bad {
                return Err(TableRefuseReason::BadLayout);
            }
            stack.extend(edges.into_iter().rev());
        }
        Ok(out)
    }
    fn index(&self, slot: *const i64, info: &'static TableTypeInfo) -> Option<u64> {
        if unsafe { slot.read() } == 0 {
            return Some(0);
        }
        self.indices
            .get(&(self.context.resolve(slot, info)? as usize))
            .copied()
    }
}
impl TableWriter<'_> {
    pub fn putref<T: TableRecord>(&mut self, slot: &TableRef<T>) -> bool {
        let Some(nodes) = self.nodes else {
            return false;
        };
        let Some(index) = nodes.index(&slot.value, T::table_info()) else {
            return false;
        };
        self.putleb(index);
        true
    }
}
#[derive(Clone, Copy)]
struct TableNodeMap<'a> {
    entries: &'a [TableNodeDirEntry],
    good: bool,
}
impl TableReader<'_> {
    pub fn reference<T: TableRecord>(
        &self,
        slot: &mut TableRef<T>,
        index: u64,
        report: &mut TableReport,
    ) {
        slot.value = 0;
        let Some(nodes) = self.nodes else {
            return;
        };
        if index == 0 || !nodes.good {
            return;
        }
        let Some(entry) = nodes.entries.get((index - 1) as usize) else {
            report.malformed = true;
            return;
        };
        if entry.offset == u64::MAX {
            return;
        }
        if entry.type_id != T::table_info().id {
            report.kind_mismatch += 1;
            return;
        }
        slot.value = if self.builder.is_some() {
            entry.offset as i64
        } else {
            index as i64
        };
    }
}
fn table_align(size: usize, alignment: usize) -> Option<usize> {
    size.checked_add(alignment - 1)
        .map(|n| n & !(alignment - 1))
}

pub fn table_graph_write<T: TableRecord>(
    value: &T,
    context: TableContext<'_>,
    buffer: Option<&mut [u8]>,
    ids: &mut [u64],
) -> Result<i64, TableRefuseReason> {
    let numbering = TableNumbering::new(context, value as *const T as *const u8, T::table_info())?;
    let mut w = TableWriter::new(buffer, ids);
    w.nodes = Some(&numbering);
    w.put8(1);
    if !unsafe { (T::table_info().save_body)(&mut w, value as *const T as *const u8) } {
        return Err(TableRefuseReason::BadLayout);
    }
    // save_body wrote the root terminator. The node table belongs immediately before it.
    w.offset -= 1;
    if numbering.entries.len() > 1 {
        w.putid(u64::MAX);
        w.put8(12);
        if !w.framed(|w| {
            w.putleb((numbering.entries.len() - 1) as u64);
            for e in &numbering.entries[1..] {
                w.putid(e.info.id);
                if !w.framed(|w| unsafe { (e.info.save_body)(w, e.node) }) {
                    return false;
                }
            }
            !w.overflow
        }) {
            return Err(TableRefuseReason::BadLayout);
        }
    }
    w.putleb(0);
    let size = w.finish();
    if size < 0 {
        Err(TableRefuseReason::BadLayout)
    } else {
        Ok(size)
    }
}

#[derive(Clone, Copy)]
struct TableNodeScan<'a> {
    payload: Option<TableReader<'a>>,
    declared: u64,
    records: usize,
    present: bool,
    malformed: bool,
}
impl<'a> TableNodeScan<'a> {
    fn begin(mut root: TableReader<'a>) -> Self {
        let mut s = Self {
            payload: None,
            declared: 0,
            records: 0,
            present: false,
            malformed: false,
        };
        while let Some(Some(id)) = root.getid() {
            if !root.has(1) {
                break;
            }
            let kind = root.get8();
            if id == u64::MAX {
                s.present = true;
                if kind != 12 {
                    s.malformed = true;
                    return s;
                }
                s.payload = root.take();
                if s.payload.is_none() {
                    s.malformed = true;
                    return s;
                }
            } else if !root.skip(kind) {
                break;
            }
        }
        if let Some(ref mut payload) = s.payload {
            match payload.getleb() {
                Some(n) => s.declared = n,
                None => s.malformed = true,
            }
        }
        s
    }
    fn next(&mut self) -> Option<(u64, TableReader<'a>)> {
        if self.malformed {
            return None;
        }
        let r = self.payload.as_mut()?;
        if r.offset == r.buffer.len() {
            return None;
        }
        let Some(Some(id)) = r.getid() else {
            self.malformed = true;
            return None;
        };
        let Some(body) = r.take() else {
            self.malformed = true;
            return None;
        };
        self.records += 1;
        Some((id, body))
    }
    fn whole(&self) -> bool {
        !self.malformed && (!self.present || self.records as u64 == self.declared)
    }
}

pub(crate) fn table_graph_load_measure<T: TableRecord>(
    bytes: &[u8],
    lookup: fn(u64) -> Option<&'static TableTypeInfo>,
) -> Result<TableLoadSize, TableLoadError> {
    let root = TableReader::open(bytes).map_err(|v| {
        if v == TableOpenVerdict::Refused {
            TableLoadError::Refused(TableRefuseReason::UnknownForm)
        } else {
            TableLoadError::Damaged
        }
    })?;
    let mut scan = TableNodeScan::begin(root);
    let root_extent =
        table_node_load_extent(T::table_info(), &root).map_err(TableLoadError::Refused)?;
    let mut data = table_align(root_extent, crate::TABLE_REGION_ALIGNMENT).unwrap();
    while let Some((id, body)) = scan.next() {
        if let Some(info) = lookup(id) {
            data = data
                .checked_add(
                    table_align(
                        table_node_load_extent(info, &body).map_err(TableLoadError::Refused)?,
                        crate::TABLE_REGION_ALIGNMENT,
                    )
                    .ok_or(TableLoadError::Refused(TableRefuseReason::BadLayout))?,
                )
                .ok_or(TableLoadError::Refused(TableRefuseReason::BadLayout))?;
        }
    }
    let attribution = (scan.records + 1)
        .checked_mul(core::mem::size_of::<TableNodeDirEntry>())
        .ok_or(TableLoadError::Refused(TableRefuseReason::BadLayout))?;
    Ok(TableLoadSize {
        data_bytes: data,
        attribution_bytes: attribution,
    })
}

pub(crate) fn table_graph_load<'a, 'd, T: TableRecord>(
    words: &'a mut [TableStorageWord],
    directory: &'d mut [TableNodeDirEntry],
    bytes: &[u8],
    report: &mut TableReport,
    lookup: fn(u64) -> Option<&'static TableTypeInfo>,
) -> Result<TableRegion<'a, 'd, T>, TableLoadError> {
    let size = table_graph_load_measure::<T>(bytes, lookup).inspect_err(|error| match error {
        TableLoadError::Damaged => {
            report.verdict = TableOpenVerdict::Damaged;
            report.malformed = true;
        }
        _ => report.verdict = TableOpenVerdict::Refused,
    })?;
    if size.data_bytes > core::mem::size_of_val(words) || size.directory_entries() > directory.len()
    {
        return Err(TableLoadError::BufferTooSmall);
    }
    let directory = &mut directory[..size.directory_entries()];
    let base = words.as_mut_ptr() as *mut u8;
    let mut root = TableReader::open(bytes).unwrap();
    let scan_start = TableNodeScan::begin(root);
    let mut scan = scan_start;
    directory[0] = TableNodeDirEntry {
        offset: 0,
        type_id: T::table_info().id,
    };
    unsafe {
        (base as *mut T).write(T::default());
    }
    let root_extent = table_node_load_extent(T::table_info(), &root).unwrap();
    let mut data = table_align(root_extent, crate::TABLE_REGION_ALIGNMENT).unwrap();
    let mut index = 1;
    let mut unknown_records = 0;
    let mut invalid_text = false;
    while let Some((id, body)) = scan.next() {
        let mut offset = u64::MAX;
        if let Some(info) = lookup(id) {
            offset = data as u64;
            unsafe {
                (info.initialize)(base.add(data));
            }
            let extent = table_node_load_extent(info, &body).unwrap();
            if info.blob != 0 {
                let invalid = info.blob == 2
                    && (body.buffer.contains(&0) || core::str::from_utf8(body.buffer).is_err());
                if invalid {
                    offset = u64::MAX;
                    invalid_text = true;
                } else {
                    unsafe {
                        (base.add(data) as *mut u32).write(body.buffer.len() as u32);
                        core::ptr::copy_nonoverlapping(
                            body.buffer.as_ptr(),
                            base.add(data + 8),
                            body.buffer.len(),
                        );
                        if info.blob == 2 {
                            base.add(data + 8 + body.buffer.len()).write(0);
                        }
                    }
                }
            }
            data += table_align(extent, crate::TABLE_REGION_ALIGNMENT).unwrap();
        } else {
            unknown_records += 1;
        }
        directory[index] = TableNodeDirEntry {
            offset,
            type_id: id,
        };
        index += 1;
    }
    let good = scan.whole();
    if !good {
        report.malformed = true;
    } else {
        report.unknown += unknown_records;
        report.malformed |= invalid_text;
    }
    let nodes = TableNodeMap {
        entries: directory,
        good,
    };
    root.nodes = Some(nodes);
    scan = scan_start;
    index = 1;
    if good {
        while let Some((id, mut body)) = scan.next() {
            if let Some(info) = lookup(id).filter(|i| i.blob == 0) {
                body.nodes = Some(nodes);
                let offset = directory[index].offset as usize;
                let extent = TableExtent {
                    base,
                    at: core::cell::Cell::new(offset + info.size as usize),
                    end: offset + table_node_load_extent(info, &body).unwrap(),
                };
                body.extent = Some(&extent);
                let destination = unsafe { base.add(directory[index].offset as usize) };
                unsafe {
                    (info.load_body)(&mut body, report, destination);
                }
            }
            index += 1;
        }
    }
    let extent = TableExtent {
        base,
        at: core::cell::Cell::new(T::table_info().size as usize),
        end: root_extent,
    };
    root.extent = Some(&extent);
    let ok = unsafe { (T::table_info().load_body)(&mut root, report, base) };
    // Decode uses validated indices so an enum arm can move into its final slot.
    // Only after every body is resident do slots become self-relative deltas.
    for (index, entry) in directory.iter().enumerate() {
        let info = if index == 0 {
            Some(T::table_info())
        } else {
            lookup(entry.type_id)
        };
        if let Some(info) = info {
            if entry.offset == u64::MAX {
                continue;
            }
            unsafe {
                (info.rewrite_refs)(base, base.add(entry.offset as usize), &mut |slot, _| {
                    let target = *slot;
                    if target != 0 {
                        let offset = directory[target as usize - 1].offset;
                        *slot = base as i64 + offset as i64 - slot as *mut i64 as i64;
                    }
                });
            }
        }
    }
    report.verdict = if ok {
        TableOpenVerdict::Ok
    } else {
        TableOpenVerdict::BodyStopped
    };
    let data = unsafe { core::slice::from_raw_parts(base, size.data_bytes) };
    Ok(TableRegion {
        data,
        directory,
        marker: core::marker::PhantomData,
    })
}

pub fn table_graph_from_json<T: TableRecord>(
    builder: &mut TableBuilder<T>,
    text: &[u8],
    report: &mut TableReport,
) -> bool {
    *builder = TableBuilder::new();
    let root = builder
        .arena
        .resolve(builder.root.value, T::table_info())
        .unwrap() as *mut u8;
    unsafe {
        table_json_read_context(
            root,
            T::table_info(),
            text,
            report,
            Some(&mut builder.arena),
        )
    }
}
pub fn table_graph_to_json<T: TableRecord>(
    value: &T,
    context: TableContext<'_>,
    buffer: Option<&mut [u8]>,
) -> i64 {
    unsafe {
        table_json_write_context(
            value as *const T as *const u8,
            T::table_info(),
            buffer,
            Some(context),
        )
    }
}
`
