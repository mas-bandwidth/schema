package rusttable

const sequenceRuntime = `/// A list's sixteen-byte storage slot. Resolve it through its owner; its
/// count and displacement cannot be changed independently by safe code.
#[repr(C)]
pub struct TableList<T> {
    pub(crate) value: i64,
    pub(crate) count: i32,
    reserved: i32,
    marker: core::marker::PhantomData<fn() -> T>,
}
impl<T> Copy for TableList<T> {}
impl<T> Clone for TableList<T> {
    fn clone(&self) -> Self {
        *self
    }
}
impl<T> Default for TableList<T> {
    fn default() -> Self {
        Self {
            value: 0,
            count: 0,
            reserved: 0,
            marker: core::marker::PhantomData,
        }
    }
}
impl<T> PartialEq for TableList<T> {
    fn eq(&self, other: &Self) -> bool {
        self.value == other.value && self.count == other.count
    }
}
impl<T> Eq for TableList<T> {}
impl<T> core::fmt::Debug for TableList<T> {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        f.debug_struct("TableList")
            .field("value", &self.value)
            .field("count", &self.count)
            .finish()
    }
}
impl<T> TableList<T> {
    pub fn len(&self) -> usize {
        self.count.max(0) as usize
    }
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
}
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct TableElementRef<T> {
    collection: i64,
    index: usize,
    marker: core::marker::PhantomData<fn() -> T>,
}
struct TableSequenceSegment {
    words: TableSpan,
    live: u32,
    used: usize,
}
pub struct TableSequenceAllocation {
    map_index: Option<std::collections::HashMap<TableOwnedKey, usize>>,
    count: i32,
    segments: Vec<TableSequenceSegment>,
    type_id: core::any::TypeId,
    size: usize,
    align: usize,
}
impl TableArena {
    pub fn list_add<T: Copy + Default + Send + Sync + 'static>(
        &mut self,
        list: &mut TableList<T>,
    ) -> Result<TableElementRef<T>, TableRefuseReason> {
        if list.value != 0 && (list.value as u64 >> 32) as u32 != self.owner {
            return self
                .owner_mut(list.value)
                .ok_or(TableRefuseReason::BadLayout)?
                .list_add(list);
        }
        if list.count == i32::MAX {
            return Err(TableRefuseReason::CountOverExtentCap);
        }
        let index = if list.value == 0 {
            self.sequences.push(TableSequenceAllocation {
                map_index: None,
                count: 0,
                segments: Vec::new(),
                type_id: core::any::TypeId::of::<T>(),
                size: core::mem::size_of::<T>().max(1),
                align: core::mem::align_of::<T>(),
            });
            list.value = self.handle(self.sequences.len() - 1);
            self.sequences.len() - 1
        } else {
            Self::index(list.value).ok_or(TableRefuseReason::BadLayout)?
        };
        let seq = self
            .sequences
            .get_mut(index)
            .ok_or(TableRefuseReason::BadLayout)?;
        if seq.count != list.count || seq.type_id != core::any::TypeId::of::<T>() || seq.align > 16
        {
            return Err(TableRefuseReason::BadLayout);
        }
        if seq.segments.last().is_none_or(|s| s.used == 32) {
            seq.segments.push(TableSequenceSegment {
                words: self.storage.allocate(seq.size * 32),
                live: 0,
                used: 0,
            });
        }
        let segment = seq.segments.last_mut().unwrap();
        let i = segment.used;
        unsafe {
            (segment.words.as_mut_ptr() as *mut u8)
                .add(i * seq.size)
                .cast::<T>()
                .write(T::default());
        }
        segment.live |= 1 << i;
        segment.used += 1;
        list.count += 1;
        seq.count += 1;
        Ok(TableElementRef {
            collection: list.value,
            index: (seq.segments.len() - 1) * 32 + i,
            marker: core::marker::PhantomData,
        })
    }
    pub fn element<T: Copy + 'static>(&self, handle: TableElementRef<T>) -> Option<&T> {
        let seq = self.sequence(handle.collection)?;
        if seq.type_id != core::any::TypeId::of::<T>() {
            return None;
        }
        let segment = seq.segments.get(handle.index / 32)?;
        let i = handle.index % 32;
        if segment.live & (1 << i) == 0 {
            return None;
        }
        Some(unsafe { &*((segment.words.as_ptr() as *const u8).add(i * seq.size) as *const T) })
    }
    pub fn element_mut<T: Copy + 'static>(&mut self, handle: TableElementRef<T>) -> Option<&mut T> {
        let seq = self.sequence_mut(handle.collection)?;
        if seq.type_id != core::any::TypeId::of::<T>() {
            return None;
        }
        let segment = seq.segments.get_mut(handle.index / 32)?;
        let i = handle.index % 32;
        if segment.live & (1 << i) == 0 {
            return None;
        }
        Some(unsafe { &mut *((segment.words.as_mut_ptr() as *mut u8).add(i * seq.size) as *mut T) })
    }
    pub fn list_erase<T: Copy + 'static>(
        &mut self,
        list: &mut TableList<T>,
        handle: TableElementRef<T>,
    ) -> bool {
        if list.value != handle.collection || list.count <= 0 {
            return false;
        }
        let Some(seq) = self.sequence_mut(handle.collection) else {
            return false;
        };
        if seq.count != list.count || seq.type_id != core::any::TypeId::of::<T>() {
            return false;
        }
        let Some(segment) = seq.segments.get_mut(handle.index / 32) else {
            return false;
        };
        let bit = 1 << (handle.index % 32);
        if segment.live & bit == 0 {
            return false;
        }
        segment.live &= !bit;
        list.count -= 1;
        seq.count -= 1;
        true
    }
}

#[derive(Clone)]
pub enum TableSequenceCursor<'a> {
    Ordered {
        items: std::rc::Rc<Vec<*const u8>>,
        index: usize,
    },
    Empty,
    Arena {
        allocation: &'a TableSequenceAllocation,
        segment: usize,
        index: usize,
    },
    Region {
        at: *const u8,
        stride: usize,
        left: usize,
        marker: core::marker::PhantomData<&'a [u8]>,
    },
}
impl Iterator for TableSequenceCursor<'_> {
    type Item = *const u8;
    fn next(&mut self) -> Option<Self::Item> {
        match self {
            Self::Empty => None,
            Self::Ordered { items, index } => {
                let p = items.get(*index).copied()?;
                *index += 1;
                Some(p)
            }
            Self::Region {
                at, stride, left, ..
            } => {
                if *left == 0 {
                    return None;
                }
                let p = *at;
                *left -= 1;
                *at = unsafe { at.add(*stride) };
                Some(p)
            }
            Self::Arena {
                allocation,
                segment,
                index,
            } => loop {
                let s = allocation.segments.get(*segment)?;
                while *index < s.used {
                    let i = *index;
                    *index += 1;
                    if s.live & (1 << i) != 0 {
                        return Some(unsafe {
                            (s.words.as_ptr() as *const u8).add(i * allocation.size)
                        });
                    }
                }
                *segment += 1;
                *index = 0;
            },
        }
    }
}
pub struct TableListView<'a, T> {
    cursor: TableSequenceCursor<'a>,
    length: usize,
    marker: core::marker::PhantomData<&'a T>,
}
impl<'a, T> TableListView<'a, T> {
    pub fn len(&self) -> usize {
        self.length
    }
    pub fn is_empty(&self) -> bool {
        self.length == 0
    }
    pub fn get(&self, index: usize) -> Option<&'a T> {
        self.cursor
            .clone()
            .nth(index)
            .map(|p| unsafe { &*(p as *const T) })
    }
}
impl<'a, T> Iterator for TableListView<'a, T> {
    type Item = &'a T;
    fn next(&mut self) -> Option<Self::Item> {
        self.cursor.next().map(|p| {
            self.length -= 1;
            unsafe { &*(p as *const T) }
        })
    }
    fn size_hint(&self) -> (usize, Option<usize>) {
        (self.length, Some(self.length))
    }
}
impl<T> ExactSizeIterator for TableListView<'_, T> {}
impl<'a> TableContext<'a> {
    unsafe fn sequence(
        self,
        slot: *const i64,
        count: i32,
        type_id: core::any::TypeId,
        size: usize,
        align: usize,
    ) -> Option<TableSequenceCursor<'a>> {
        if count < 0 {
            return None;
        }
        if count == 0 {
            return Some(TableSequenceCursor::Empty);
        }
        let value = unsafe { slot.read() };
        if value == 0 {
            return None;
        }
        match self.source {
            TableSource::Arena(arena) => {
                let allocation = arena.sequence(value)?;
                if allocation.type_id != type_id || allocation.count != count {
                    return None;
                }
                Some(TableSequenceCursor::Arena {
                    allocation,
                    segment: 0,
                    index: 0,
                })
            }
            TableSource::Region(data) => {
                let slot_offset = (slot as usize).checked_sub(data.as_ptr() as usize)?;
                if slot_offset.checked_add(16)? > data.len() {
                    return None;
                }
                let offset = slot_offset.checked_add_signed(isize::try_from(value).ok()?)?;
                let end = offset.checked_add((count as usize).checked_mul(size)?)?;
                if end > data.len() || offset % align != 0 {
                    return None;
                }
                Some(TableSequenceCursor::Region {
                    at: unsafe { data.as_ptr().add(offset) },
                    stride: size,
                    left: count as usize,
                    marker: core::marker::PhantomData,
                })
            }
        }
    }
    pub fn list<T: Copy + 'static>(self, list: &TableList<T>) -> Option<TableListView<'a, T>> {
        let cursor = unsafe {
            self.sequence(
                &list.value,
                list.count,
                core::any::TypeId::of::<T>(),
                core::mem::size_of::<T>().max(1),
                core::mem::align_of::<T>(),
            )
        }?;
        Some(TableListView {
            cursor,
            length: list.len(),
            marker: core::marker::PhantomData,
        })
    }
}
#[allow(clippy::type_complexity)]
pub struct TableSequenceInfo {
    pub map_entry: Option<fn() -> &'static TableTypeInfo>,
    pub type_id: fn() -> core::any::TypeId,
    pub size: usize,
    pub align: usize,
    pub kind: u8,
    pub floor: usize,
    pub initialize: unsafe fn(*mut u8),
    pub save: unsafe fn(&mut TableWriter, *const u8) -> bool,
    pub load: unsafe fn(&mut TableReader, &mut TableReport, *mut u8, u8) -> bool,
    pub visit:
        unsafe fn(TableContext, *const u8, &mut dyn FnMut(*const i64, &'static TableTypeInfo)),
    pub rewrite: unsafe fn(*mut u8, *mut u8, &mut dyn FnMut(&mut i64, &'static TableTypeInfo)),
    pub extent: Option<fn(TableReader, &mut usize) -> Result<(), TableRefuseReason>>,
}
impl TableSequenceInfo {
    unsafe fn cursor<'a>(
        &self,
        context: TableContext<'a>,
        slot: *const u8,
    ) -> Option<TableSequenceCursor<'a>> {
        if self.map_entry.is_some() {
            return unsafe { table_map_order(context, slot, self) };
        }
        unsafe {
            context.sequence(
                slot as *const i64,
                (slot.add(8) as *const i32).read(),
                (self.type_id)(),
                self.size,
                self.align,
            )
        }
    }
}
pub struct TableExtent {
    base: *mut u8,
    at: core::cell::Cell<usize>,
    end: usize,
}
impl TableExtent {
    unsafe fn carve(&self, info: &TableSequenceInfo, count: usize) -> Option<*mut u8> {
        let at = table_align(self.at.get(), info.align)?;
        let end = at.checked_add(info.size.checked_mul(count)?)?;
        if end > self.end {
            return None;
        }
        self.at.set(end);
        let p = unsafe { self.base.add(at) };
        for i in 0..count {
            unsafe {
                (info.initialize)(p.add(i * info.size));
            }
        }
        Some(p)
    }
}
pub fn table_sequence_extent(
    mut body: TableReader,
    at: &mut usize,
    info: &TableSequenceInfo,
) -> Result<(), TableRefuseReason> {
    if !body.has(2) || body.get8() != info.kind {
        return Ok(());
    }
    let Some(n) = body.getleb() else {
        return Ok(());
    };
    if n > i32::MAX as u64 {
        return Err(TableRefuseReason::CountOverExtentCap);
    }
    if n > (body.buffer.len() - body.offset) as u64 / info.floor as u64 {
        return Err(TableRefuseReason::CountOverLength);
    }
    *at = table_align(*at, info.align)
        .and_then(|v| v.checked_add((n as usize).checked_mul(info.size)?))
        .ok_or(TableRefuseReason::BadLayout)?;
    if let Some(inner) = info.extent {
        for _ in 0..n {
            let Some(element) = body.extent_element(info.kind) else {
                break;
            };
            inner(element, at)?;
        }
    }
    Ok(())
}
pub(crate) unsafe fn table_sequence_save(
    w: &mut TableWriter,
    slot: *const u8,
    info: &TableSequenceInfo,
) -> bool {
    unsafe {
        let Some(nodes) = w.nodes else {
            return false;
        };
        let Some(elements) = info.cursor(nodes.context, slot) else {
            return false;
        };
        w.framed(|w| {
            w.put8(info.kind);
            w.putleb((slot.add(8) as *const i32).read() as u64);
            for element in elements.clone() {
                if !(info.save)(w, element) {
                    return false;
                }
            }
            !w.overflow
        })
    }
}

pub(crate) unsafe fn table_sequence_load(
    r: &mut TableReader,
    report: &mut TableReport,
    slot: *mut u8,
    info: &TableSequenceInfo,
) -> bool {
    unsafe {
        if info.map_entry.is_some() {
            return table_map_load(r, report, slot, info);
        }
        let Some(length) = r.length() else {
            report.malformed = true;
            return false;
        };
        let end = r.offset + length;
        if length < 2 {
            r.offset = end;
            return true;
        }
        let kind = r.get8();
        let Some(n) = r.getleb() else {
            report.malformed = true;
            r.offset = end;
            return true;
        };
        if kind != info.kind {
            if !TableReader::widens(kind, info.kind) {
                report.kind_mismatch += 1;
                r.offset = end;
                return true;
            }
            report.widened += 1;
        }
        if n > i32::MAX as u64 {
            if let Some(builder) = r.builder {
                builder
                    .refused
                    .set(Some(TableRefuseReason::CountOverExtentCap));
            }
            return false;
        }
        (slot as *mut i64).write(0);
        (slot.add(8) as *mut i32).write(0);
        if let Some(builder) = r.builder {
            if r.offset > end {
                report.malformed = true;
                r.offset = end;
                return true;
            }
            let mut body = r.sub(end - r.offset);
            for _ in 0..n {
                let Some(element) = (&mut *builder.arena).sequence_add(slot, info) else {
                    report.malformed = true;
                    break;
                };
                if !(info.load)(&mut body, report, element, kind) {
                    (&mut *builder.arena).sequence_drop_last(slot);
                    break;
                }
                if builder.refused.get().is_some() {
                    return false;
                }
            }
            r.offset = end;
            return !r.builder_refused();
        }
        let Some(extent) = r.extent else {
            report.malformed = true;
            return false;
        };
        let Some(elements) = extent.carve(info, n as usize) else {
            report.malformed = true;
            r.offset = end;
            return true;
        };
        if n != 0 {
            (slot as *mut i64).write(elements.offset_from(extent.base) as i64);
        }
        if r.offset > end {
            report.malformed = true;
            r.offset = end;
            return true;
        }
        let mut body = r.sub(end - r.offset);
        for i in 0..n as usize {
            if !(info.load)(&mut body, report, elements.add(i * info.size), kind) {
                break;
            }
            (slot.add(8) as *mut i32).write(i as i32 + 1);
        }
        r.offset = end;
        true
    }
}

impl TableArena {
    unsafe fn sequence_add(&mut self, slot: *mut u8, info: &TableSequenceInfo) -> Option<*mut u8> {
        unsafe {
            let reference = slot as *mut i64;
            let count = slot.add(8) as *mut i32;
            if reference.read() != 0 && (reference.read() as u64 >> 32) as u32 != self.owner {
                return self.owner_mut(reference.read())?.sequence_add(slot, info);
            }
            if count.read() == i32::MAX {
                return None;
            }
            if reference.read() == 0 {
                self.sequences.push(TableSequenceAllocation {
                    map_index: None,
                    count: 0,
                    segments: Vec::new(),
                    type_id: (info.type_id)(),
                    size: info.size,
                    align: info.align,
                });
                reference.write(self.handle(self.sequences.len() - 1));
            }
            let allocation = self.sequences.get_mut(Self::index(reference.read())?)?;
            if allocation.type_id != (info.type_id)()
                || allocation.count != count.read()
                || info.align > 16
            {
                return None;
            }
            if allocation.segments.last().is_none_or(|s| s.used == 32) {
                allocation.segments.push(TableSequenceSegment {
                    words: self.storage.allocate(info.size * 32),
                    live: 0,
                    used: 0,
                });
            }
            let segment = allocation.segments.last_mut().unwrap();
            let i = segment.used;
            let p = (segment.words.as_mut_ptr() as *mut u8).add(i * info.size);
            (info.initialize)(p);
            segment.live |= 1 << i;
            segment.used += 1;
            allocation.count += 1;
            count.write(count.read() + 1);
            Some(p)
        }
    }
}
unsafe fn table_json_read_sequence(
    input: &mut TableJsonIn,
    base: *mut u8,
    f: &TableFieldInfo,
    depth: i32,
    report: &mut TableReport,
) -> bool {
    unsafe {
        if input.peek() != b'[' || input.arena.is_none() {
            input.bad = true;
            return false;
        }
        input.pos += 1;
        (f.reset)(base);
        let slot = base.add(f.offset as usize);
        let info = f.sequence.unwrap()();
        let shape = table_json_element_shape(f);
        loop {
            if input.peek() == b']' {
                input.pos += 1;
                return true;
            }
            let Some(element) = input.arena.as_deref_mut().unwrap().sequence_add(slot, info) else {
                input.bad = true;
                return false;
            };
            if input.value_shape() != shape && !(f.kind == 17 && input.value_shape() == b'z') {
                report.kind_mismatch += 1;
                if !table_json_skip_value(input, depth + 1, report) {
                    return false;
                }
            } else if !table_json_read_scalar(input, element, f, depth + 1, report) {
                return false;
            }
            match input.peek() {
                b',' => input.pos += 1,
                b']' => {
                    input.pos += 1;
                    return true;
                }
                _ => {
                    input.bad = true;
                    return false;
                }
            }
        }
    }
}
unsafe fn table_json_write_sequence(
    out: &mut TableJsonOut,
    slot: *const u8,
    f: &TableFieldInfo,
    depth: i32,
) -> bool {
    unsafe {
        let Some(context) = out.context else {
            return false;
        };
        let info = f.sequence.unwrap()();
        let Some(elements) = info.cursor(context, slot) else {
            return false;
        };
        let mut first = true;
        out.put(b'[');
        for element in elements {
            if !first {
                out.put(b',');
            }
            first = false;
            out.line(depth + 1);
            if !table_json_write_scalar(out, element, f, depth + 1) {
                return false;
            }
        }
        if !first {
            out.line(depth);
        }
        out.put(b']');
        true
    }
}

pub const fn table_sequence_size<T>() -> usize {
    let n = core::mem::size_of::<T>();
    if n == 0 { 1 } else { n }
}
impl<'a> TableReader<'a> {
    pub fn extent_element(&mut self, kind: u8) -> Option<Self> {
        if kind == 13 {
            return self.take();
        }
        if kind != 15 {
            return None;
        }
        let start = *self;
        if self.getid()?.is_some() {
            if !self.has(1) {
                return None;
            }
            self.get8();
            self.take()?;
        }
        Some(start.sub(self.offset - start.offset))
    }
}
`
