package rusttable

const mapRuntime = `/// A map slot. Keys can only be changed by erase and emplace.
#[repr(transparent)]
pub struct TableMap<T> {
    pub(crate) storage: TableList<T>,
}
impl<T> Copy for TableMap<T> {}
impl<T> Clone for TableMap<T> {
    fn clone(&self) -> Self {
        *self
    }
}
impl<T> Default for TableMap<T> {
    fn default() -> Self {
        Self {
            storage: TableList::default(),
        }
    }
}
impl<T> PartialEq for TableMap<T> {
    fn eq(&self, other: &Self) -> bool {
        self.storage == other.storage
    }
}
impl<T> Eq for TableMap<T> {}
impl<T> core::fmt::Debug for TableMap<T> {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        self.storage.fmt(f)
    }
}
impl<T> TableMap<T> {
    pub fn len(&self) -> usize {
        self.storage.len()
    }
    pub fn is_empty(&self) -> bool {
        self.storage.is_empty()
    }
}
/// The generated entry binds its key, value view and storage descriptor.
/// # Safety
/// The descriptor must describe Self exactly and value_mut must expose only
/// the value field and its companions, never the entry's key or padding.
pub unsafe trait TableMapEntry: Copy + Default + Send + Sync + 'static {
    type ValueMut<'a>
    where
        Self: 'a;
    fn sequence_info() -> &'static TableSequenceInfo;
    fn value_mut(&mut self) -> Self::ValueMut<'_>;
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct TableMapEntryRef<T> {
    element: TableElementRef<T>,
}
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
enum TableOwnedKey {
    Text(Vec<u8>),
    Signed(i64),
    Unsigned(u64),
}
impl TableMapKey<'_> {
    fn owned(self) -> TableOwnedKey {
        match self {
            Self::Text(v) => TableOwnedKey::Text(v.to_vec()),
            Self::Signed(v) => TableOwnedKey::Signed(v),
            Self::Unsigned(v) => TableOwnedKey::Unsigned(v),
        }
    }
}
impl<'a> From<&'a str> for TableMapKey<'a> {
    fn from(v: &'a str) -> Self {
        Self::Text(v.as_bytes())
    }
}
impl From<i64> for TableMapKey<'_> {
    fn from(v: i64) -> Self {
        Self::Signed(v)
    }
}
impl From<u64> for TableMapKey<'_> {
    fn from(v: u64) -> Self {
        Self::Unsigned(v)
    }
}
impl TableArena {
    pub fn map_emplace<E: TableMapEntry>(
        &mut self,
        map: &mut TableMap<E>,
        key: TableMapKey,
    ) -> Result<TableMapEntryRef<E>, TableRefuseReason> {
        unsafe { self.map_place(map as *mut _ as *mut u8, E::sequence_info(), key) }
            .ok_or(TableRefuseReason::BadLayout)?;
        self.map_find(map, key).ok_or(TableRefuseReason::BadLayout)
    }
    pub fn map_find<E: TableMapEntry>(
        &self,
        map: &TableMap<E>,
        key: TableMapKey,
    ) -> Option<TableMapEntryRef<E>> {
        if !table_map_key_fits(key, &E::sequence_info().map_entry?().fields[0]) {
            return None;
        }
        let collection = map.storage.value;
        let allocation = self.sequence(collection)?;
        if allocation.type_id != core::any::TypeId::of::<E>()
            || allocation.count != map.storage.count
        {
            return None;
        }
        let index = *allocation.map_index.as_ref()?.get(&key.owned())?;
        Some(TableMapEntryRef {
            element: TableElementRef {
                collection,
                index,
                marker: core::marker::PhantomData,
            },
        })
    }
    pub fn map_entry<E: TableMapEntry>(&self, handle: TableMapEntryRef<E>) -> Option<&E> {
        self.element(handle.element)
    }
    pub fn map_value_mut<E: TableMapEntry>(
        &mut self,
        handle: TableMapEntryRef<E>,
    ) -> Option<E::ValueMut<'_>> {
        self.element_mut(handle.element).map(E::value_mut)
    }
    pub fn map_erase<E: TableMapEntry>(&mut self, map: &mut TableMap<E>, key: TableMapKey) -> bool {
        let Some(handle) = self.map_find(map, key) else {
            return false;
        };
        if !self.list_erase(&mut map.storage, handle.element) {
            return false;
        }
        self.sequence_mut(handle.element.collection)
            .unwrap()
            .map_index
            .as_mut()
            .unwrap()
            .remove(&key.owned());
        true
    }
}
pub struct TableMapView<'a, E: TableMapEntry> {
    entries: TableListView<'a, E>,
}
impl<'a, E: TableMapEntry> TableMapView<'a, E> {
    pub fn len(&self) -> usize {
        self.entries.len()
    }
    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }
    pub fn get(&self, key: TableMapKey) -> Option<&'a E> {
        let field = &E::sequence_info().map_entry?().fields[0];
        if !table_map_key_fits(key, field) {
            return None;
        }
        let (mut low, mut high) = (0, self.len());
        while low < high {
            let middle = low + (high - low) / 2;
            let entry = self.entries.get(middle)?;
            match unsafe { table_map_entry_key(entry as *const E as *const u8, field) }?
                .compare(key)
            {
                core::cmp::Ordering::Less => low = middle + 1,
                core::cmp::Ordering::Greater => high = middle,
                core::cmp::Ordering::Equal => return Some(entry),
            }
        }
        None
    }
}
impl<'a, E: TableMapEntry> Iterator for TableMapView<'a, E> {
    type Item = &'a E;
    fn next(&mut self) -> Option<Self::Item> {
        self.entries.next()
    }
    fn size_hint(&self) -> (usize, Option<usize>) {
        self.entries.size_hint()
    }
}
impl<E: TableMapEntry> ExactSizeIterator for TableMapView<'_, E> {}
impl<'a> TableContext<'a> {
    pub fn map<E: TableMapEntry>(self, map: &TableMap<E>) -> Option<TableMapView<'a, E>> {
        let cursor =
            unsafe { table_map_order(self, map as *const _ as *const u8, E::sequence_info()) }?;
        Some(TableMapView {
            entries: TableListView {
                cursor,
                length: map.len(),
                marker: core::marker::PhantomData,
            },
        })
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TableMapKey<'a> {
    Text(&'a [u8]),
    Signed(i64),
    Unsigned(u64),
}
impl TableMapKey<'_> {
    fn compare(self, other: Self) -> core::cmp::Ordering {
        match (self, other) {
            (Self::Text(a), Self::Text(b)) => a.cmp(b),
            (Self::Signed(a), Self::Signed(b)) => a.cmp(&b),
            (Self::Unsigned(a), Self::Unsigned(b)) => a.cmp(&b),
            _ => core::cmp::Ordering::Equal,
        }
    }
}
unsafe fn table_map_entry_key<'a>(
    entry: *const u8,
    key: &TableFieldInfo,
) -> Option<TableMapKey<'a>> {
    unsafe {
        let p = entry.add(key.offset as usize);
        if key.kind == 12 {
            let n = (entry.add(key.count_offset as usize) as *const i32).read();
            if n < 0 || n > key.array_bound {
                return None;
            }
            let bytes = core::slice::from_raw_parts(p, n as usize);
            if bytes.contains(&0) || core::str::from_utf8(bytes).is_err() {
                return None;
            }
            Some(TableMapKey::Text(bytes))
        } else if (2..=5).contains(&key.kind) {
            Some(TableMapKey::Signed(table_json_get_signed(p, key.elem_size)))
        } else {
            Some(TableMapKey::Unsigned(table_json_get_raw(p, key.elem_size)))
        }
    }
}
pub(crate) unsafe fn table_map_order<'a>(
    context: TableContext<'a>,
    slot: *const u8,
    info: &TableSequenceInfo,
) -> Option<TableSequenceCursor<'a>> {
    unsafe {
        let cursor = context.sequence(
            slot as *const i64,
            (slot.add(8) as *const i32).read(),
            (info.type_id)(),
            info.size,
            info.align,
        )?;
        if !matches!(context.source, TableSource::Arena(_)) {
            return Some(cursor);
        }
        let key = &info.map_entry?().fields[0];
        let mut entries: Vec<_> = cursor.collect();
        for &p in &entries {
            table_map_entry_key(p, key)?;
        }
        entries.sort_unstable_by(|&a, &b| {
            table_map_entry_key(a, key)
                .unwrap()
                .compare(table_map_entry_key(b, key).unwrap())
        });
        if entries.windows(2).any(|p| {
            table_map_entry_key(p[0], key).unwrap() == table_map_entry_key(p[1], key).unwrap()
        }) {
            return None;
        }
        Some(TableSequenceCursor::Ordered {
            items: std::rc::Rc::new(entries),
            index: 0,
        })
    }
}
struct TableMapKeyRead<'a> {
    key: TableMapKey<'a>,
    kind_bad: bool,
    widened: bool,
    over: bool,
    malformed: bool,
}
fn table_map_read_key<'a>(
    mut body: TableReader<'a>,
    field: &TableFieldInfo,
) -> TableMapKeyRead<'a> {
    let mut out = TableMapKeyRead {
        key: if field.kind == 12 {
            TableMapKey::Text(&[])
        } else if field.kind <= 5 {
            TableMapKey::Signed(0)
        } else {
            TableMapKey::Unsigned(0)
        },
        kind_bad: false,
        widened: false,
        over: false,
        malformed: false,
    };
    loop {
        let id = match body.getid() {
            Some(None) => {
                out.malformed = body.offset != body.buffer.len();
                return out;
            }
            Some(Some(id)) => id,
            None => {
                out.malformed = true;
                return out;
            }
        };
        if !body.has(1) {
            out.malformed = true;
            return out;
        }
        let kind = body.get8();
        if id == field.id {
            if kind != field.kind && TableReader::widens(kind, field.kind) {
                out.kind_bad = false;
                out.widened = true;
                let Some(v) = body.widened(kind) else {
                    out.malformed = true;
                    return out;
                };
                out.key = if field.kind <= 5 {
                    TableMapKey::Signed(v as i64)
                } else {
                    TableMapKey::Unsigned(v)
                };
                continue;
            }
            out.kind_bad = kind != field.kind;
            if !out.kind_bad {
                if kind == 12 {
                    let Some(text) = body.take() else {
                        out.malformed = true;
                        return out;
                    };
                    if text.buffer.contains(&0) || core::str::from_utf8(text.buffer).is_err() {
                        out.malformed = true;
                        return out;
                    }
                    out.key = TableMapKey::Text(text.buffer);
                    out.over = text.buffer.len() > field.array_bound as usize;
                } else {
                    let Some(v) = body.widened(kind) else {
                        out.malformed = true;
                        return out;
                    };
                    out.key = if field.kind <= 5 {
                        TableMapKey::Signed(v as i64)
                    } else {
                        TableMapKey::Unsigned(v)
                    };
                }
                continue;
            }
        }
        if !body.skip(kind) {
            out.malformed = true;
            return out;
        }
    }
}
unsafe fn table_map_load(
    r: &mut TableReader,
    report: &mut TableReport,
    slot: *mut u8,
    info: &TableSequenceInfo,
) -> bool {
    unsafe {
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
        if kind != 13 {
            report.kind_mismatch += 1;
            r.offset = end;
            return true;
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
        let count = slot.add(8) as *mut i32;
        count.write(0);
        let entries = if r.builder.is_some() {
            core::ptr::null_mut()
        } else {
            let Some(extent) = r.extent else {
                report.malformed = true;
                return false;
            };
            let Some(entries) = extent.carve(info, n as usize) else {
                report.malformed = true;
                r.offset = end;
                return true;
            };
            if n != 0 {
                (slot as *mut i64).write(entries.offset_from(extent.base) as i64);
            }
            entries
        };
        if r.offset > end {
            report.malformed = true;
            r.offset = end;
            return true;
        }
        let mut body = r.sub(end - r.offset);
        let entry_info = info.map_entry.unwrap()();
        let key_info = &entry_info.fields[0];
        let mut last = None;
        let mut widened = false;
        for _ in 0..n {
            let Some(mut element) = body.take() else {
                report.malformed = true;
                break;
            };
            let read = table_map_read_key(element, key_info);
            if read.widened && !widened {
                widened = true;
                report.widened += 1;
            }
            if read.kind_bad {
                report.kind_mismatch += 1;
                (slot as *mut i64).write(0);
                count.write(0);
                break;
            }
            if read.malformed {
                report.malformed = true;
                break;
            }
            if read.over {
                report.clamped += 1;
                continue;
            }
            let order = last
                .map(|k: TableMapKey| k.compare(read.key))
                .unwrap_or(core::cmp::Ordering::Less);
            if order == core::cmp::Ordering::Greater {
                report.malformed = true;
                break;
            }
            if order == core::cmp::Ordering::Equal {
                report.duplicate += 1;
            }
            let entry = if let Some(builder) = r.builder {
                let Some(entry) = (&mut *builder.arena).map_place(slot, info, read.key) else {
                    report.malformed = true;
                    break;
                };
                entry
            } else {
                let at = if order == core::cmp::Ordering::Equal {
                    count.read() - 1
                } else {
                    let at = count.read();
                    count.write(at + 1);
                    at
                };
                entries.add(at as usize * info.size)
            };
            (entry_info.load_body)(&mut element, report, entry);
            if r.builder_refused() {
                return false;
            }
            last = Some(read.key);
        }
        r.offset = end;
        true
    }
}

fn table_map_key_fits(key: TableMapKey, field: &TableFieldInfo) -> bool {
    match key {
        TableMapKey::Text(bytes) => {
            field.kind == 12
                && bytes.len() <= field.array_bound as usize
                && !bytes.contains(&0)
                && core::str::from_utf8(bytes).is_ok()
        }
        TableMapKey::Signed(v) => {
            let bits = field.elem_size * 8;
            (2..=5).contains(&field.kind)
                && (bits == 64 || v >= -(1i64 << (bits - 1)) && v < (1i64 << (bits - 1)))
        }
        TableMapKey::Unsigned(v) => {
            (6..=9).contains(&field.kind)
                && (field.elem_size == 8 || v < (1u64 << (field.elem_size * 8)))
        }
    }
}
unsafe fn table_map_set_key(entry: *mut u8, field: &TableFieldInfo, key: TableMapKey) {
    unsafe {
        let p = entry.add(field.offset as usize);
        match key {
            TableMapKey::Text(bytes) => {
                core::ptr::copy_nonoverlapping(bytes.as_ptr(), p, bytes.len());
                (entry.add(field.count_offset as usize) as *mut i32).write(bytes.len() as i32);
            }
            TableMapKey::Signed(v) => table_json_set_raw(p, field.elem_size, v as u64),
            TableMapKey::Unsigned(v) => table_json_set_raw(p, field.elem_size, v),
        }
    }
}
impl TableArena {
    unsafe fn map_place(
        &mut self,
        slot: *mut u8,
        info: &TableSequenceInfo,
        key: TableMapKey,
    ) -> Option<*mut u8> {
        unsafe {
            let entry_info = info.map_entry?();
            let key_field = &entry_info.fields[0];
            if !table_map_key_fits(key, key_field) {
                return None;
            }
            let reference = (slot as *const i64).read();
            let count = (slot.add(8) as *const i32).read();
            let owned = key.owned();
            let found = if reference == 0 {
                None
            } else {
                let allocation = self.sequence(reference)?;
                if allocation.type_id != (info.type_id)() || allocation.count != count {
                    return None;
                }
                allocation.map_index.as_ref()?.get(&owned).copied()
            };
            let p = if let Some(index) = found {
                let allocation = self.sequence_mut(reference)?;
                let segment = &mut allocation.segments[index / 32];
                let p = (segment.words.as_mut_ptr() as *mut u8).add(index % 32 * info.size);
                (entry_info.fields[1].reset)(p);
                p
            } else {
                let p = self.sequence_add(slot, info)?;
                let allocation = self.sequence_mut((slot as *const i64).read())?;
                let index =
                    (allocation.segments.len() - 1) * 32 + allocation.segments.last()?.used - 1;
                allocation
                    .map_index
                    .get_or_insert_with(std::collections::HashMap::new)
                    .insert(owned, index);
                p
            };
            table_map_set_key(p, key_field, key);
            Some(p)
        }
    }
}
// Interpret a syntactically checked decimal spelling exactly. Fractions are
// rejected as identities; a double must never merge distinct integer keys.
fn table_map_integer(token: &[u8], field: &TableFieldInfo) -> Option<TableMapKey<'static>> {
    let negative = token.first() == Some(&b'-');
    let begin = usize::from(negative);
    let mut i = begin;
    while i < token.len() && token[i].is_ascii_digit() {
        i += 1;
    }
    let whole = i - begin;
    let mut fraction = 0;
    let mut fraction_begin = i;
    if token.get(i) == Some(&b'.') {
        i += 1;
        fraction_begin = i;
        while i < token.len() && token[i].is_ascii_digit() {
            i += 1;
        }
        fraction = i - fraction_begin;
    }
    let mut exponent = 0i64;
    if matches!(token.get(i), Some(b'e' | b'E')) {
        i += 1;
        let neg = token.get(i) == Some(&b'-');
        if matches!(token.get(i), Some(b'-' | b'+')) {
            i += 1;
        }
        while i < token.len() {
            exponent = (exponent * 10 + (token[i] - b'0') as i64).min(100000);
            i += 1;
        }
        if neg {
            exponent = -exponent;
        }
    }
    let digit = |k: usize| {
        if k < whole {
            token[begin + k] - b'0'
        } else {
            token[fraction_begin + k - whole] - b'0'
        }
    };
    let mut start = 0;
    let mut end = whole + fraction;
    let mut point = whole as i64 + exponent;
    while start < end && digit(start) == 0 {
        start += 1;
        point -= 1;
    }
    while start < end && digit(end - 1) == 0 {
        end -= 1;
    }
    let mut value = 0u64;
    if start < end {
        if point < (end - start) as i64 || point > 20 {
            return None;
        }
        for k in 0..point as usize {
            value = value.checked_mul(10)?.checked_add(if start + k < end {
                digit(start + k) as u64
            } else {
                0
            })?;
        }
    }
    let key = if field.kind <= 5 {
        if negative {
            if value > 1u64 << 63 {
                return None;
            }
            TableMapKey::Signed((value as i64).wrapping_neg())
        } else {
            TableMapKey::Signed(i64::try_from(value).ok()?)
        }
    } else {
        if negative && value != 0 {
            return None;
        }
        TableMapKey::Unsigned(value)
    };
    table_map_key_fits(key, field).then_some(key)
}
unsafe fn table_json_read_map(
    input: &mut TableJsonIn,
    base: *mut u8,
    f: &TableFieldInfo,
    depth: i32,
    report: &mut TableReport,
) -> bool {
    unsafe {
        if input.peek() != b'{' || input.arena.is_none() || depth + 1 > TABLE_JSON_MAX_DEPTH {
            input.bad = true;
            return false;
        }
        input.pos += 1;
        let slot = base.add(f.offset as usize);
        let info = f.sequence.unwrap()();
        let entry_info = info.map_entry.unwrap()();
        let key_info = &entry_info.fields[0];
        let value_info = &entry_info.fields[1];
        loop {
            if input.peek() == b'}' {
                input.pos += 1;
                return true;
            }
            let mut token = TableJsonKey::new();
            let mut scan_report = TableReport::default();
            if !table_json_scan_key(input, &mut token, &mut scan_report) || input.peek() != b':' {
                input.bad = true;
                return false;
            }
            input.pos += 1;
            let key = if key_info.kind == 12 {
                if scan_report.clamped != 0 || token.length > key_info.array_bound as usize {
                    report.clamped += 1;
                    None
                } else {
                    Some(TableMapKey::Text(&token.bytes[..token.length]))
                }
            } else if scan_report.clamped != 0 {
                report.kind_mismatch += 1;
                None
            } else {
                let bytes = &token.bytes[..token.length];
                let mut probe = TableJsonIn {
                    arena: None,
                    labels: std::collections::HashMap::new(),
                    text: bytes,
                    pos: 0,
                    bad: false,
                };
                if bytes.first().is_none_or(|c| c.is_ascii_whitespace())
                    || table_json_walk_number(&mut probe).is_none()
                    || probe.pos != bytes.len()
                {
                    input.bad = true;
                    return false;
                }
                let key = table_map_integer(bytes, key_info);
                if key.is_none() {
                    report.kind_mismatch += 1;
                }
                key
            };
            if let Some(key) = key {
                let before = (slot.add(8) as *const i32).read();
                let Some(entry) = input
                    .arena
                    .as_deref_mut()
                    .unwrap()
                    .map_place(slot, info, key)
                else {
                    input.bad = true;
                    return false;
                };
                if (slot.add(8) as *const i32).read() == before {
                    report.duplicate += 1;
                }
                let shape = input.value_shape();
                if shape != table_json_shape(value_info)
                    && !(value_info.kind == 17 && !value_info.is_array && shape == b'z')
                {
                    report.kind_mismatch += 1;
                    if !table_json_skip_value(input, depth + 1, report) {
                        return false;
                    }
                } else if !table_json_read_field(input, entry, value_info, depth + 1, report) {
                    return false;
                }
            } else if !table_json_skip_value(input, depth + 1, report) {
                return false;
            }
            match input.peek() {
                b',' => input.pos += 1,
                b'}' => {
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
unsafe fn table_json_write_map(
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
        let entry_info = info.map_entry.unwrap()();
        let Some(entries) = table_map_order(context, slot, info) else {
            return false;
        };
        let mut first = true;
        out.put(b'{');
        for entry in entries {
            if !first {
                out.put(b',');
            }
            first = false;
            out.line(depth + 1);
            let Some(key) = table_map_entry_key(entry, &entry_info.fields[0]) else {
                return false;
            };
            match key {
                TableMapKey::Text(bytes) => table_json_write_string_bytes(out, bytes),
                TableMapKey::Signed(v) => {
                    out.put(b'"');
                    table_json_write_signed(out, v);
                    out.put(b'"');
                }
                TableMapKey::Unsigned(v) => {
                    out.put(b'"');
                    table_json_write_unsigned(out, v);
                    out.put(b'"');
                }
            }
            out.raw(b": ");
            if !table_json_write_field(out, entry, &entry_info.fields[1], depth + 1) {
                return false;
            }
        }
        if !first {
            out.line(depth);
        }
        out.put(b'}');
        true
    }
}
`
