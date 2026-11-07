package rusttable

// CRC-32C uses a compile-time lookup table: no allocation or lazy setup on a read.
const integrityRuntimeModule = "integrity_runtime"
const integrityRuntimeRust = `
const CRC32C: [u32; 256] = {
    let mut table = [0u32; 256];
    let mut i = 0usize;
    while i < 256 {
        let mut value = i as u32;
        let mut bit = 0;
        while bit < 8 {
            value = (value >> 1) ^ (0x82f63b78 & 0u32.wrapping_sub(value & 1));
            bit += 1;
        }
        table[i] = value;
        i += 1;
    }
    table
};

pub fn table_integrity_checksum(bytes: &[u8]) -> u32 {
    let mut sum = !0u32;
    for &byte in bytes {
        sum = (sum >> 8) ^ CRC32C[((sum ^ u32::from(byte)) & 255) as usize];
    }
    !sum
}

pub fn table_integrity_body(bytes: &[u8], minimum: usize) -> Option<&[u8]> {
    if bytes.len() < minimum.max(4) { return None; }
    let end = bytes.len() - 4;
    let sum = u32::from_le_bytes(bytes[end..].try_into().ok()?);
    if table_integrity_checksum(&bytes[..end]) != sum { return None; }
    Some(&bytes[..end])
}

pub fn table_integrity_seal(bytes: &mut Vec<u8>) {
    let sum = table_integrity_checksum(bytes);
    bytes.extend_from_slice(&sum.to_le_bytes());
}
`
