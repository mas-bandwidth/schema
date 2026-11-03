package jstable

// Text lists retain their synthetic record storage; only wire and JSON project
// those records to scalar text. Bounds and totals are checked in linear time.
const jsTextListRuntime = `
function TableVariableTextListLength(record) {
  return record.value === null ? 0 : TableVariableUtf8Encode(record.value).length;
}
function TableVariableTextListValid(fd, values) {
  if (!Array.isArray(values) || (!fd.list && values.length > fd.bound) || values.length > 0x7fffffff) { return false; }
  const vf = fd.e.c.fields[0];
  let total = 0;
  for (const record of values) {
    if (record === null || typeof record !== "object") { return false; }
    const text = record.value;
    if (text === null && vf.opt) { continue; }
    if (typeof text !== "string") { return false; }
    const bytes = TableVariableUtf8Encode(text);
    if ((vf.bound >= 0 && bytes.length > vf.bound) || !TableVariableUtf8Decode(bytes).ok) { return false; }
    // TextEncoder replaces lone surrogates; the stored value must itself be
    // well-formed, not merely encodable after substitution.
    for (let i = 0; i < text.length; i++) {
      const a = text.charCodeAt(i);
      if (a >= 0xd800 && a <= 0xdbff) {
        const b = text.charCodeAt(++i);
        if (!(b >= 0xdc00 && b <= 0xdfff)) { return false; }
      } else if (a >= 0xdc00 && a <= 0xdfff) { return false; }
    }
    total += bytes.length;
    if (fd.total > 0 && total > fd.total) { return false; }
  }
  return true;
}
function TableVariableTextListClamp(fd, report) {
  report.clamped++;
  if (!report.clampedField) { report.clampedField = fd.field; }
}
function TableVariableTextListRead(fd, inp, v, depth, graph, report) {
  if (inp.peek() !== "[") { inp.bad = true; return false; }
  // the text list is one level below its holder, and its elements one below that
  if (depth + 1 > 128) { inp.bad = true; return false; }
  inp.p++;
  const out = [], vf = fd.e.c.fields[0];
  v[fd.n] = out;
  const bound = fd.list ? 0x7fffffff : fd.bound;
  let count = 0, total = 0, dropped = false;
  for (;;) {
    let ch = inp.peek();
    if (ch === "]") { inp.p++; return true; }
    if (ch === "") { inp.bad = true; return false; }
    if (count >= bound || dropped) {
      if (!dropped && count >= bound) { TableVariableTextListClamp(fd, report); }
      if (!inp.skip(depth + 2, graph)) { return false; }
    } else {
      const record = fd.e.c.make(), shape = inp.shape();
      if (shape === "z" && vf.opt) {
        if (!inp.literal("null")) { return false; }
      } else if (shape !== "s") {
        report.kindMismatch++;
        if (!inp.skip(depth + 2, graph)) { return false; }
      } else {
        const before = report.clamped;
        if (!TableVariableTextField(vf, inp, record, depth + 1, graph, report)) { return false; }
        if (report.clamped !== before && !report.clampedField) { report.clampedField = fd.field; }
      }
      total += TableVariableTextListLength(record);
      if (fd.total > 0 && total > fd.total) { dropped = true; TableVariableTextListClamp(fd, report); }
      else { out.push(record); }
    }
    count++;
    ch = inp.peek();
    if (ch === ",") { inp.p++; continue; }
    if (ch === "]") { inp.p++; return true; }
    inp.bad = true;
    return false;
  }
}
`
