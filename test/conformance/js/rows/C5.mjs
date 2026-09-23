// row js/C5 — "wide text code units" (docs/roadmap.sexp js/C5;
// docs/FIXED-FORM-ALGORITHM.md:333, the 'text' row of §4.5's scatter).
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:333:
//   "wide code units over v, never 2N, an astral pair counting two (fix 7)."
//
// The JS leg cannot generate wide-text table code at all:
//   `bin/schema generate --lang js` refuses any schema containing wstring(N)
//   in a table closure, with the message "table wide text is C, C++, C#, Dart
//   and Go only today". The FXW.schema fixture (test/tables/FXW.schema) has
//   wstring(8) and wstring(4) fields and cannot be compiled for JS.
//
// This means the law is mechanically untestable for JS on this tip.
// The test file is kept as a committed RED test with the derivation below.
//
// WOULD-BE WIRE VECTOR (if JS could generate FxWide):
//   FxWide body (FXW.schema):
//     offset  0: caption length int32 (code units) = 10
//     offset  4: caption bytes 2*10 = U+1F4A9 repeated (astral pair)
//     offset 24: label length int32 = 4
//     offset 28: label bytes 1*4 = "test"
//     offset 32: inner FxCaption (nested wstring(4))
//     offset 40: seq uint32 = 1
//   An astral pair (U+1F4A9) is 2 wide code units (0xD83D 0xDCA9) and the
//   length field counts code units, not bytes or characters.
//
// THE TEST: would assert that a length of 2 code units for U+1F4A9 is
// accepted, and that the same character counted as 4 bytes would clamp.
// Since the JS leg cannot compile wstring, this test always goes RED.

console.log("FAIL: js/C5 is untestable — the JS leg cannot compile wstring(N) in table closures (bin/schema refuses); wide code units are not implemented for this leg");
process.exit(1);
