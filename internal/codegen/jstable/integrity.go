package jstable

// CRC-32C framing is one file/batch trailer. The root's declaration selects
// it; nested records never append trailers of their own (SPEC-TABLES 2.14).
const jsIntegrityRuntime = `
const TableIntegrityTable = (() => {
  const table = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    let crc = i;
    for (let bit = 0; bit < 8; bit++) { crc = (crc >>> 1) ^ ((crc & 1) ? 0x82f63b78 : 0); }
    table[i] = crc >>> 0;
  }
  return table;
})();
export function TableIntegritySum(bytes, count) {
  let crc = 0xffffffff;
  for (let i = 0; i < count; i++) { crc = TableIntegrityTable[(crc ^ bytes[i]) & 255] ^ (crc >>> 8); }
  return (crc ^ 0xffffffff) >>> 0;
}
export function TableIntegrityHolds(bytes, minimum) {
  const end = bytes.length - 4;
  if (end < minimum) { return false; }
  const expected = (bytes[end] | (bytes[end + 1] << 8) | (bytes[end + 2] << 16) | (bytes[end + 3] << 24)) >>> 0;
  return expected === TableIntegritySum(bytes, end);
}
export function TableIntegritySeal(bytes, count) {
  if (count < 0 || bytes.length - count < 4) { return -1; }
  new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength).setUint32(count, TableIntegritySum(bytes, count), true);
  return count + 4;
}
`
