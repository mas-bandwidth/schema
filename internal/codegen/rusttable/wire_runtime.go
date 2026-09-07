package rusttable

// This runtime is independent of the unit. The generated entry point supplies
// a stack array sized to the unit's complete vocabulary; readers borrow the
// file's trailer directly and therefore need no schema-sized read capacity.
const wireRuntime = `
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum TableOpenVerdict {
    #[default]
    Ok,
    Refused,
    Damaged,
    BodyStopped,
}

pub struct TableWriter<'a> {
    buffer: Option<&'a mut [u8]>,
    ids: &'a mut [u64],
    count: usize,
    pub offset: usize,
    pub overflow: bool,
}

impl<'a> TableWriter<'a> {
    #[inline(always)]
    pub fn new(buffer: Option<&'a mut [u8]>, ids: &'a mut [u64]) -> Self {
        Self { buffer, ids, count: 0, offset: 0, overflow: false }
    }

    #[inline(always)]
    pub fn raw(&mut self, bytes: &[u8]) {
        let Some(end) = self.offset.checked_add(bytes.len()) else { self.overflow = true; return; };
        if let Some(buffer) = self.buffer.as_deref_mut() {
            if end > buffer.len() { self.overflow = true; return; }
            buffer[self.offset..end].copy_from_slice(bytes);
        }
        self.offset = end;
    }
    #[inline(always)]
    pub fn put8(&mut self, v: u8) { self.raw(&[v]); }
    #[inline(always)]
    pub fn put16(&mut self, v: u16) { self.raw(&v.to_le_bytes()); }
    #[inline(always)]
    pub fn put32(&mut self, v: u32) { self.raw(&v.to_le_bytes()); }
    #[inline(always)]
    pub fn put64(&mut self, v: u64) { self.raw(&v.to_le_bytes()); }
    #[inline(always)]
    pub fn putleb(&mut self, mut v: u64) {
        while v >= 128 { self.put8((v as u8 & 127) | 128); v >>= 7; }
        self.put8(v as u8);
    }
    #[inline(always)]
    pub fn putid(&mut self, id: u64) {
        // A zero HASH is an ordinary identity. Only a zero REFERENCE is None.
        let index = match self.ids[..self.count].iter().position(|v| *v == id) {
            Some(i) => i,
            None => {
                if self.count == self.ids.len() { self.overflow = true; return; }
                let i = self.count;
                self.ids[i] = id;
                self.count += 1;
                i
            }
        };
        self.putleb(index as u64 + 1);
    }
    pub fn framed(&mut self, body: impl Fn(&mut TableWriter) -> bool) -> bool {
        // Measure in the vocabulary at this exact position. The speculative
        // suffix is overwritten on replay, so the real walk owns first use.
        let mut probe = TableWriter::new(None, self.ids);
        probe.count = self.count;
        if !body(&mut probe) || probe.overflow { return false; }
        let size = probe.offset;
        self.putleb(size as u64);
        body(self) && !self.overflow
    }
    pub fn finish(&mut self) -> i64 {
        for i in 0..self.count { self.put64(self.ids[i]); }
        self.put64(self.count as u64);
        if self.overflow { -1 } else { i64::try_from(self.offset).unwrap_or(-1) }
    }
}

#[derive(Clone, Copy)]
pub struct TableReader<'a> {
    pub buffer: &'a [u8],
    pub offset: usize,
    ids: &'a [u8],
    nested: bool,
}

impl<'a> TableReader<'a> {
    #[inline(always)]
    pub fn new(buffer: &'a [u8]) -> Self {
        Self { buffer, offset: 0, ids: &[], nested: true }
    }
    pub fn open(bytes: &'a [u8]) -> Result<Self, TableOpenVerdict> {
        if bytes.is_empty() { return Err(TableOpenVerdict::Damaged); }
        if bytes[0] != 1 { return Err(TableOpenVerdict::Refused); }
        if bytes.len() < 9 { return Err(TableOpenVerdict::Damaged); }
        let count = u64::from_le_bytes(bytes[bytes.len()-8..].try_into().unwrap());
        if count > ((bytes.len()-9)/8) as u64 { return Err(TableOpenVerdict::Damaged); }
        let at = bytes.len()-8-count as usize*8;
        let ids = &bytes[at..bytes.len()-8];
        for i in 0..count as usize {
            for j in 0..i {
                if ids[i*8..i*8+8] == ids[j*8..j*8+8] { return Err(TableOpenVerdict::Damaged); }
            }
        }
        let r = Self { buffer: &bytes[1..at], offset: 0, ids, nested: false };
        if r.ends_early() { return Err(TableOpenVerdict::Damaged); }
        Ok(r)
    }
    #[inline(always)]
    pub fn has(&self, bytes: u64) -> bool { bytes <= self.buffer.len().saturating_sub(self.offset) as u64 }
    #[inline(always)]
    pub fn get8(&mut self) -> u8 { let v = self.buffer[self.offset]; self.offset += 1; v }
    #[inline(always)]
    pub fn get16(&mut self) -> u16 { let v = u16::from_le_bytes(self.buffer[self.offset..self.offset+2].try_into().unwrap()); self.offset += 2; v }
    #[inline(always)]
    pub fn get32(&mut self) -> u32 { let v = u32::from_le_bytes(self.buffer[self.offset..self.offset+4].try_into().unwrap()); self.offset += 4; v }
    #[inline(always)]
    pub fn get64(&mut self) -> u64 { let v = u64::from_le_bytes(self.buffer[self.offset..self.offset+8].try_into().unwrap()); self.offset += 8; v }
    #[inline(always)]
    pub fn getleb(&mut self) -> Option<u64> {
        let at = self.offset;
        let mut value = 0;
        for i in 0..10 {
            if !self.has(1) { break; }
            let b = self.get8();
            if i == 9 && b > 1 { break; }
            value |= ((b & 127) as u64) << (7*i);
            if b & 128 == 0 {
                if i > 0 && b == 0 { break; }
                return Some(value);
            }
        }
        self.offset = at;
        None
    }
    // None is damage, Some(None) the zero reference, Some(Some(id)) an id.
    #[inline(always)]
    pub fn getid(&mut self) -> Option<Option<u64>> {
        let reference = self.getleb()?;
        if reference == 0 { return Some(None); }
        if reference > (self.ids.len()/8) as u64 { return None; }
        let at = (reference as usize-1)*8;
        Some(Some(u64::from_le_bytes(self.ids[at..at+8].try_into().unwrap())))
    }
    #[inline(always)]
    pub fn length(&mut self) -> Option<usize> {
        let size = self.getleb()?;
        if !self.has(size) { return None; }
        Some(size as usize)
    }
    #[inline(always)]
    pub fn sub(&self, bytes: usize) -> Self {
        Self { buffer: &self.buffer[self.offset..self.offset+bytes], offset: 0, ids: self.ids, nested: true }
    }
    #[inline(always)]
    pub fn take(&mut self) -> Option<Self> {
        let size = self.length()?;
        let sub = self.sub(size);
        self.offset += size;
        Some(sub)
    }
    pub fn skip(&mut self, kind: u8) -> bool {
        let width = match kind {
            1|2|6|20|25 => 1,
            3|7|21|26 => 2,
            4|8|10|22|27 => 4,
            5|9|11|23|28 => 8,
            18|19|24|29 => 16,
            17|30 => return self.getleb().is_some(),
            12|13|14|16|31|32|33 => return self.take().is_some(),
            15 => {
                match self.getleb() {
                    Some(0) => return true,
                    Some(_) => {},
                    None => return false,
                }
                if !self.has(1) { return false; }
                self.offset += 1;
                return self.take().is_some();
            },
            _ => return false,
        };
        if !self.has(width) { return false; }
        self.offset += width as usize;
        true
    }
    pub fn ends_early(&self) -> bool {
        let mut r = *self;
        loop {
            match r.getid() {
                Some(None) => return r.offset != r.buffer.len(),
                Some(Some(_)) => {},
                None => return false,
            }
            if !r.has(1) { return false; }
            let kind = r.get8();
            if !r.skip(kind) { return false; }
        }
    }
    pub fn widens(kind: u8, declared: u8) -> bool {
        (kind >= 2 && kind < declared && declared <= 5) ||
        (kind >= 6 && kind < declared && declared <= 9) ||
        (kind == 10 && declared == 11)
    }
    pub fn widened(&mut self, kind: u8) -> Option<u64> {
        let width = match kind { 2|6 => 1, 3|7 => 2, 4|8|10 => 4, _ => return None };
        if !self.has(width) { return None; }
        Some(match kind {
            2 => self.get8() as i8 as i64 as u64,
            3 => self.get16() as i16 as i64 as u64,
            4 => self.get32() as i32 as i64 as u64,
            6 => self.get8() as u64,
            7 => self.get16() as u64,
            8 => self.get32() as u64,
            10 => {
                let bits = self.get32();
                if bits & 0x7f800000 == 0x7f800000 {
                    // Preserve the sign, quiet/signalling bit and entire NaN
                    // payload rather than passing them through FP conversion.
                    ((bits as u64 & 0x80000000) << 32) | 0x7ff0000000000000 | ((bits as u64 & 0x7fffff) << 29)
                } else { (f32::from_bits(bits) as f64).to_bits() }
            },
            _ => unreachable!(),
        })
    }
    #[inline(always)]
    pub fn reserved(&self, id: u64) -> bool {
        // Announcement-only identities cannot appear in a file body; a node
        // table belongs to the root alone, including on fixed-class readers.
        id == u64::MAX-1 || id == u64::MAX-2 || (self.nested && id == u64::MAX)
    }
}
`
