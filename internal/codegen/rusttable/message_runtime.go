package rusttable

// Shared connection vocabulary and low-bit-first batch primitives (§3.3).
const messageRuntime = `/// Message refusals are connection or batch outcomes. Damage is separate.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum TableMessageReason {
    #[default]
    None,
    NewerForm,
    NoVocabulary,
    SecondAnnouncement,
    VocabularyTooLarge,
    BatchTooLarge,
    MessageFormAsFile,
    RetainMessage,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TableMessageError {
    Refused(TableMessageReason),
    Damaged,
    BufferTooSmall,
 Authoring(TableRefuseReason),
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct TableMessageShape {
    pub(crate) kind: u8,
    pub(crate) packing: u8,
    pub(crate) value_bits: i16,
    pub(crate) base: u128,
    pub(crate) min: u64,
    pub(crate) max: u64,
    pub(crate) qmin: f32,
    pub(crate) qdelta: f32,
    pub(crate) qcount: u32,
}
impl Default for TableMessageShape {
    fn default() -> Self {
        Self {
            kind: 0,
            packing: 0,
            value_bits: -1,
            base: 0,
            min: 0,
            max: 0,
            qmin: 0.0,
            qdelta: 0.0,
            qcount: 0,
        }
    }
}
#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub struct TableMessageEntry {
    pub(crate) id: u64,
    pub(crate) shape: TableMessageShape,
    pub(crate) element: TableMessageShape,
}
impl TableMessageEntry {
    pub fn id(&self) -> u64 {
        self.id
    }
    pub fn kind(&self) -> u8 {
        self.shape.kind
    }
}
pub(crate) fn table_message_bits(max: u64) -> usize {
    (64 - max.leading_zeros()) as usize
}
fn table_message_quantization(min: f32, max: f32, res: f32) -> Option<(f32, u32, i16)> {
    if !(min < max) || !(res > 0.0) {
        return None;
    }
    let delta = max - min;
    let values = delta / res;
    if !delta.is_finite() || !values.is_finite() {
        return None;
    }
    let count = values.clamp(1.0, 4294967040.0).ceil() as u32;
    Some((delta, count, table_message_bits(count as u64) as i16))
}
impl TableMessageShape {
    fn read(r: &mut TableReader, kind: u8) -> Option<(Self, u8)> {
        if kind > 33 {
            return None;
        }
        let mut s = Self {
            kind,
            ..Self::default()
        };
        let mut elem = 0;
        let numeric = (2..=9).contains(&kind) || (18..=29).contains(&kind);
        if numeric || kind == 10 {
            if !r.has(1) {
                return None;
            }
            s.packing = r.get8();
            match s.packing {
                0 => s.value_bits = (TableReader::width(kind) * 8) as i16,
                1 if numeric => {
                    let bits = r.getleb()?;
                    if bits > (TableReader::width(kind) * 8) as u64 {
                        return None;
                    }
                    s.value_bits = bits as i16;
                    if kind >= 18 {
                        if !r.has(16) {
                            return None;
                        }
                        s.base = r.get128();
                    } else {
                        let base = r.getleb()?;
                        s.base = if (2..=5).contains(&kind) {
                            (((base >> 1) as i64) ^ -((base & 1) as i64)) as i128 as u128
                        } else {
                            base as u128
                        };
                    }
                }
                2 if kind == 10 => {
                    if !r.has(12) {
                        return None;
                    }
                    s.qmin = f32::from_bits(r.get32());
                    let max = f32::from_bits(r.get32());
                    let res = f32::from_bits(r.get32());
                    (s.qdelta, s.qcount, s.value_bits) =
                        table_message_quantization(s.qmin, max, res)?;
                }
                _ => return None,
            }
        } else {
            match kind {
                1 => s.value_bits = 1,
                11 => s.value_bits = 64,
                12 | 33 => {
                    s.max = r.getleb()?;
                    if s.max > i32::MAX as u64 {
                        return None;
                    }
                }
                14 | 16 => {
                    if kind == 14 {
                        s.min = r.getleb()?;
                    }
                    s.max = r.getleb()?;
                    if s.max > u32::MAX as u64 || s.min > s.max || !r.has(1) {
                        return None;
                    }
                    elem = r.get8();
                    if elem > 33 || elem == 12 || elem == 33 {
                        return None;
                    }
                }
                _ => {}
            }
        }
        Some((s, elem))
    }
}
impl TableMessageEntry {
    fn read(r: &mut TableReader) -> Option<Self> {
        if !r.has(9) {
            return None;
        }
        let id = r.get64();
        let kind = r.get8();
        let (shape, kind) = TableMessageShape::read(r, kind)?;
        let element = if shape.kind == 14 || shape.kind == 16 {
            TableMessageShape::read(r, kind)?.0
        } else {
            TableMessageShape::default()
        };
        Some(Self { id, shape, element })
    }
}
/// One direction of one connection. The caller owns entry storage and can
/// release announcement bytes as soon as announce_read returns.
pub struct TableVocabulary<'a> {
    entries: &'a mut [TableMessageEntry],
    count: usize,
    ref_bits: usize,
    build_version: u64,
    attempted: bool,
    announced: bool,
    pub max_bytes: usize,
}
impl<'a> TableVocabulary<'a> {
    pub fn new(entries: &'a mut [TableMessageEntry]) -> Self {
        Self {
            entries,
            count: 0,
            ref_bits: 1,
            build_version: 0,
            attempted: false,
            announced: false,
            max_bytes: 65536,
        }
    }
    pub fn is_announced(&self) -> bool {
        self.announced
    }
    pub fn build_version(&self) -> u64 {
        self.build_version
    }
    pub fn entries(&self) -> &[TableMessageEntry] {
        &self.entries[..self.count]
    }
    pub fn reference_bits(&self) -> usize {
        self.ref_bits
    }
    pub(crate) fn entry(&self, reference: u64) -> Option<TableMessageEntry> {
        self.entries()
            .get(usize::try_from(reference).ok()?.checked_sub(1)?)
            .copied()
    }
    pub(crate) fn name(&self, reference: u64) -> Option<u64> {
        let e = self.entry(reference)?;
        (e.id < u64::MAX - 2 && e.shape.kind == 0).then_some(e.id)
    }
    pub(crate) fn arm(&self, reference: u64) -> Option<TableMessageEntry> {
        let e = self.entry(reference)?;
        (e.id < u64::MAX - 2 && e.shape.kind != 0).then_some(e)
    }
    pub fn announce_read(
        &mut self,
        bytes: &[u8],
        report: &mut TableReport,
    ) -> Result<(), TableMessageError> {
        let result = if self.attempted {
            Err(TableMessageError::Refused(
                TableMessageReason::SecondAnnouncement,
            ))
        } else {
            self.attempted = true;
            self.read_once(bytes, report)
        };
        if let Err(error) = result {
            table_message_report(error, report);
        }
        result
    }
    fn read_once(
        &mut self,
        bytes: &[u8],
        report: &mut TableReport,
    ) -> Result<(), TableMessageError> {
        use TableMessageError::{Damaged, Refused};
        if let Some(&form) = bytes.first() {
            if form != 1 {
                return Err(Refused(if form == 2 {
                    TableMessageReason::MessageFormAsFile
                } else {
                    TableMessageReason::NewerForm
                }));
            }
        }
        let mut r = TableReader::open(bytes).map_err(|_| Damaged)?;
        if r.ends_early() {
            return Err(Damaged);
        }
        let (mut versions, mut vocabularies) = (0, 0);
        let mut words = &[][..];
        loop {
            let Some(id) = r.getid().ok_or(Damaged)? else {
                break;
            };
            if !r.has(1) {
                return Err(Damaged);
            }
            let kind = r.get8();
            match id {
                x if x == u64::MAX - 1 => {
                    if kind != 9 || !r.has(8) {
                        return Err(Damaged);
                    }
                    self.build_version = r.get64();
                    versions += 1;
                }
                x if x == u64::MAX - 2 => {
                    if kind != 14 {
                        return Err(Damaged);
                    }
                    let mut field = r.take().ok_or(Damaged)?;
                    if !field.has(1) || field.get8() != 6 {
                        return Err(Damaged);
                    }
                    let n = field.getleb().ok_or(Damaged)?;
                    if n != (field.buffer.len() - field.offset) as u64 {
                        return Err(Damaged);
                    }
                    if n > self.max_bytes as u64 {
                        return Err(Refused(TableMessageReason::VocabularyTooLarge));
                    }
                    words = &field.buffer[field.offset..];
                    vocabularies += 1;
                }
                _ => {
                    report.unknown += 1;
                    if !r.skip(kind) {
                        return Err(Damaged);
                    }
                }
            }
        }
        if versions != 1 || vocabularies != 1 {
            return Err(Damaged);
        }
        let mut r = TableReader::new(words);
        let (mut count, mut node_slots) = (0, 0);
        while r.offset < words.len() {
            if count == self.entries.len() {
                return Err(Refused(TableMessageReason::VocabularyTooLarge));
            }
            let parsed = TableMessageEntry::read(&mut r).ok_or(Damaged)?;
            if parsed.id == u64::MAX - 1 || parsed.id == u64::MAX - 2 {
                return Err(Damaged);
            }
            if parsed.id == u64::MAX {
                node_slots += 1;
                if node_slots > 1 {
                    return Err(Damaged);
                }
            }
            if self.entries[..count].contains(&parsed) {
                return Err(Damaged);
            }
            self.entries[count] = parsed;
            count += 1;
        }
        self.count = count;
        self.ref_bits = table_message_bits(count as u64).max(1);
        self.announced = true;
        Ok(())
    }
}
fn table_message_report(error: TableMessageError, report: &mut TableReport) {
    match error {
        TableMessageError::Refused(reason) => {
            report.verdict = TableOpenVerdict::Refused;
            report.reason = reason;
        }
        TableMessageError::Damaged => {
            report.malformed = true;
            report.verdict = TableOpenVerdict::BodyStopped;
        }
        TableMessageError::BufferTooSmall | TableMessageError::Authoring(_) => {}
    }
}
pub fn announce_read(
    vocabulary: &mut TableVocabulary,
    bytes: &[u8],
    report: &mut TableReport,
) -> Result<(), TableMessageError> {
    vocabulary.announce_read(bytes, report)
}

/// The stream counts bits from the batch's first byte, low bit first.
pub struct TableBitWriter<'a> {
    buffer: Option<&'a mut [u8]>,
    pub(crate) offset: usize,
    pub(crate) overflow: bool,
}
impl<'a> TableBitWriter<'a> {
    pub fn new(buffer: Option<&'a mut [u8]>) -> Self {
        Self {
            buffer,
            offset: 0,
            overflow: false,
        }
    }
    pub fn bits_written(&self) -> usize {
        self.offset
    }
    pub fn is_overflow(&self) -> bool {
        self.overflow
    }
    pub fn put(&mut self, value: u64, bits: usize) {
        if bits > 64 {
            self.overflow = true;
            return;
        }
        let Some(end) = self.offset.checked_add(bits) else {
            self.overflow = true;
            return;
        };
        if let Some(buffer) = self.buffer.as_deref_mut() {
            if end.div_ceil(8) > buffer.len() {
                self.overflow = true;
                self.offset = end;
                return;
            }
            let byte = self.offset / 8;
            if buffer.len() - byte >= 9 {
                let current = u64::from_le_bytes(buffer[byte..byte + 8].try_into().unwrap())
                    as u128
                    | ((buffer[byte + 8] as u128) << 64);
                let mask = ((1u128 << bits) - 1) << (self.offset % 8);
                let next = (current & !mask) | (((value as u128) << (self.offset % 8)) & mask);
                buffer[byte..byte + 8].copy_from_slice(&(next as u64).to_le_bytes());
                buffer[byte + 8] = (next >> 64) as u8;
                self.offset = end;
                return;
            }
            let (mut at, mut left, mut value) = (self.offset, bits, value);
            while left != 0 {
                let shift = at % 8;
                let take = left.min(8 - shift);
                let mask = ((1u16 << take) - 1) as u8;
                buffer[at / 8] =
                    (buffer[at / 8] & !(mask << shift)) | ((value as u8 & mask) << shift);
                at += take;
                left -= take;
                value >>= take;
            }
        }
        self.offset = end;
    }
    pub fn put128(&mut self, value: u128, bits: usize) {
        if bits > 128 {
            self.overflow = true;
            return;
        }
        self.put(value as u64, bits.min(64));
        if bits > 64 {
            self.put((value >> 64) as u64, bits - 64);
        }
    }
    pub fn align(&mut self) {
        let n = (8 - self.offset % 8) % 8;
        self.put(0, n);
    }
    pub fn raw(&mut self, bytes: &[u8]) {
        if self.offset % 8 != 0 {
            self.overflow = true;
            return;
        }
        let Some(end) = self.offset.checked_add(bytes.len().saturating_mul(8)) else {
            self.overflow = true;
            return;
        };
        if let Some(buffer) = self.buffer.as_deref_mut() {
            if end / 8 > buffer.len() {
                self.overflow = true;
            } else {
                buffer[self.offset / 8..end / 8].copy_from_slice(bytes);
            }
        }
        self.offset = end;
    }
    pub fn finish(mut self) -> Result<usize, TableMessageError> {
        self.align();
        if self.overflow {
            Err(TableMessageError::BufferTooSmall)
        } else {
            Ok(self.offset / 8)
        }
    }
}
#[derive(Clone, Copy)]
pub struct TableBitReader<'a> {
    buffer: &'a [u8],
    pub(crate) offset: usize,
}
impl<'a> TableBitReader<'a> {
    pub fn new(buffer: &'a [u8]) -> Self {
        Self { buffer, offset: 0 }
    }
    pub fn bits_read(&self) -> usize {
        self.offset
    }
    pub fn remaining(&self) -> usize {
        self.buffer
            .len()
            .saturating_mul(8)
            .saturating_sub(self.offset)
    }
    pub fn get(&mut self, bits: usize) -> Option<u64> {
        if bits > 64 || bits > self.remaining() {
            return None;
        }
        let byte = self.offset / 8;
        if self.buffer.len() - byte >= 9 {
            let word = u64::from_le_bytes(self.buffer[byte..byte + 8].try_into().unwrap()) as u128
                | ((self.buffer[byte + 8] as u128) << 64);
            let value = (word >> (self.offset % 8)) & ((1u128 << bits) - 1);
            self.offset += bits;
            return Some(value as u64);
        }
        let (mut at, mut left, mut shift, mut value) = (self.offset, bits, 0, 0u64);
        while left != 0 {
            let take = left.min(8 - at % 8);
            let mask = ((1u16 << take) - 1) as u8;
            value |= ((self.buffer[at / 8] >> (at % 8) & mask) as u64) << shift;
            at += take;
            left -= take;
            shift += take;
        }
        self.offset = at;
        Some(value)
    }
    pub fn get128(&mut self, bits: usize) -> Option<u128> {
        if bits > 128 || bits > self.remaining() {
            return None;
        }
        let lo = self.get(bits.min(64))? as u128;
        Some(if bits > 64 {
            lo | ((self.get(bits - 64)? as u128) << 64)
        } else {
            lo
        })
    }
    pub fn skip(&mut self, bits: usize) -> bool {
        if bits > self.remaining() {
            return false;
        }
        self.offset += bits;
        true
    }
    pub fn align(&mut self) -> bool {
        let bits = (8 - self.offset % 8) % 8;
        self.get(bits) == Some(0)
    }
    pub fn raw(&mut self, bytes: usize) -> Option<&'a [u8]> {
        if self.offset % 8 != 0 || bytes > self.remaining() / 8 {
            return None;
        }
        let at = self.offset / 8;
        self.offset += bytes * 8;
        Some(&self.buffer[at..at + bytes])
    }
    pub fn finish(&mut self) -> bool {
        self.align() && self.remaining() == 0
    }
}
/// A value and its owner for a variable-class batch. The context resolves its
/// references; all referenced nodes must belong to that arena or region.
pub struct TableMessageValue<'a,T:TableRecord>{value:&'a T,context:TableContext<'a>}
impl<'a,T:TableRecord> TableMessageValue<'a,T>{pub fn new(value:&'a T,context:TableContext<'a>)->Self{Self{value,context}}}
pub struct TableMessageWriter<'a,'b>{pub(crate) bits:&'a mut TableBitWriter<'b>,nodes:Option<&'a TableNumbering<'a>>,index_bits:usize}
impl TableMessageWriter<'_,'_> {
    pub(crate) fn body_bits(&self,write:impl FnOnce(&mut TableMessageWriter)->bool)->Option<usize> {
        let mut bits=TableBitWriter::new(None);bits.offset=self.bits.offset;
        let mut child=TableMessageWriter{bits:&mut bits,nodes:self.nodes,index_bits:self.index_bits};
        if !write(&mut child)||bits.overflow{return None;}Some(bits.offset-self.bits.offset)
    }
    pub(crate) fn putref<T:TableRecord>(&mut self,reference:&TableRef<T>)->bool {
        let Some(nodes)=self.nodes else{return false;};
        let Some(index)=nodes.index(&reference.value,T::table_info()) else{return false;};
        self.bits.put(index,self.index_bits);!self.bits.overflow
    }
}
pub(crate) fn table_message_quantize(value:f32,min:f32,delta:f32,count:u32)->u32 {
    let raw=(value-min)/delta;let normalized=if raw.is_nan(){0.0}else{raw.clamp(0.0,1.0)};
    let scaled=normalized*count as f32;
    ((scaled+0.5) as u32).min(count)
}
fn table_message_batch_write(count:usize,report:&mut TableReport)->Result<(),TableMessageError> {
    if !(1..=256).contains(&count){let error=TableMessageError::Refused(TableMessageReason::BatchTooLarge);table_message_report(error,report);return Err(error);}Ok(())
}
pub(crate) fn table_message_write_fixed<T:TableRecord>(values:&[T],buffer:Option<&mut[u8]>,report:&mut TableReport)->Result<usize,TableMessageError> {
    table_message_batch_write(values.len(),report)?;
    let mut bits=TableBitWriter::new(buffer);bits.put(2,8);bits.put(values.len() as u64-1,8);
    for value in values {
        let mut w=TableMessageWriter{bits:&mut bits,nodes:None,index_bits:1};
        if !unsafe{(T::table_info().message_save)(&mut w,value as *const T as *const u8)} {
            return Err(if bits.overflow{TableMessageError::BufferTooSmall}else{TableMessageError::Authoring(TableRefuseReason::BadLayout)});
        }
    }
    bits.finish()
}
pub(crate) fn table_message_write_graph<T:TableRecord>(values:&[TableMessageValue<T>],buffer:Option<&mut[u8]>,report:&mut TableReport)->Result<usize,TableMessageError> {
    table_message_batch_write(values.len(),report)?;
    let mut bits=TableBitWriter::new(buffer);bits.put(2,8);bits.put(values.len() as u64-1,8);
    for value in values {
        let numbering=TableNumbering::new(value.context,value.value as *const T as *const u8,T::table_info()).map_err(TableMessageError::Authoring)?;
        let count=numbering.entries.len()-1;
        if count>u32::MAX as usize{return Err(TableMessageError::Authoring(TableRefuseReason::CountOverExtentCap));}
        let mut w=TableMessageWriter{bits:&mut bits,nodes:Some(&numbering),index_bits:table_message_bits(count as u64+1)};
        if count!=0 {
            w.bits.put(crate::TABLE_MESSAGE_NODE_SLOT,crate::TABLE_MESSAGE_REF_BITS);w.bits.put(count as u64,32);
            for entry in &numbering.entries[1..] {
                w.bits.put(entry.info.message_slot,crate::TABLE_MESSAGE_REF_BITS);
                if !unsafe{(entry.info.message_save)(&mut w,entry.node)}{return Err(TableMessageError::Authoring(TableRefuseReason::BadLayout));}
            }
        }
        if !unsafe{(T::table_info().message_save)(&mut w,value.value as *const T as *const u8)} {
            return Err(if bits.overflow{TableMessageError::BufferTooSmall}else{TableMessageError::Authoring(TableRefuseReason::BadLayout)});
        }
    }
    bits.finish()
}
pub(crate) unsafe fn table_message_sequence_save(w:&mut TableMessageWriter,slot:*const u8,info:&TableSequenceInfo)->bool {
    unsafe {
        let Some(nodes)=w.nodes else{return false;};
        let Some(elements)=info.cursor(nodes.context,slot) else{return false;};
        w.bits.put((slot.add(8) as *const i32).read() as u64,32);
        if info.kind==6{w.bits.align();}
        for element in elements{if !(info.message_save)(w,element){return false;}}
        !w.bits.overflow
    }
}
`
