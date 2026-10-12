package rusttable

// THE VARIABLE FORM'S TEXT (docs/SPEC-TABLES.md §16, §16.2, §16.7): the Rust
// JSON text form in and out for exactly the card record's constructs. It is the
// THIRD STAGE of the port, emitted beside the wire codec: a pretty-printed
// writer that spells one card as the committed card-populated.json byte for
// byte, and a tolerant reader that places the same values back so the wire
// round-trips (docs/SPEC-TABLES.md §16.5).

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitVariableText emits the JSON text form for every card-boundary variable
// table in the file: the shared helpers, each enum's name mapping, and, per
// table, the write, read and the two public roots (docs/SPEC-TABLES.md §16.1).
func (g *gen) emitVariableText(b *strings.Builder) {
	names := cardVariableTableNames(g)
	if len(names) == 0 {
		return
	}
	b.WriteString("\n// ---- the JSON text form (docs/SPEC-TABLES.md §16, §16.2) ----\n")
	b.WriteString(textHelpers())

	// enums, once each
	seen := map[string]bool{}
	for _, n := range names {
		for _, f := range g.unit.Tables[n].Fields {
			if e, ok := f.Type.Ref.(*ir.Enum); ok && !seen[e.Name] {
				seen[e.Name] = true
				g.emitEnumText(b, e)
			}
		}
	}

	for _, n := range names {
		g.emitTextWrite(b, g.unit.Tables[n])
	}
	for _, n := range names {
		g.emitTextRead(b, g.unit.Tables[n])
	}
	for _, n := range names {
		g.emitTextRoots(b, g.unit.Tables[n])
	}
}

// textHelpers is the shared JSON machinery the generated body codecs ride on: a
// pretty-print newline, the string escape (RFC 8259, §16.2), a code-point
// clamp, and the tolerant cursor (§16.2's report counters).
func textHelpers() string {
	return `
fn json_newline(out: &mut String, depth: usize) {
    out.push('\n');
    for _ in 0..depth {
        out.push_str("  ");
    }
}

/// Escape one string as a JSON string (docs/SPEC-TABLES.md §16.2): the control
/// characters JSON spells, the quote and the backslash; every other code point
/// rides raw, so multibyte text stays byte for byte.
fn json_escape(s: &str, out: &mut String) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{0008}' => out.push_str("\\b"),
            '\u{000c}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => {
                out.push_str("\\u00");
                let h = format!("{:02x}", c as u32);
                out.push_str(&h);
            }
            c => out.push(c),
        }
    }
    out.push('"');
}

/// Clamp a string to n bytes at a code point boundary (docs/SPEC-TABLES.md
/// §16.2): longer than n is clamped, counted, never cut through a multibyte
/// character.
fn json_clamp(s: String, n: usize, clamped: &mut u64) -> String {
    if s.len() <= n {
        return s;
    }
    let mut end = n;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    *clamped += 1;
    s[..end].to_string()
}

/// The tolerant JSON cursor (docs/SPEC-TABLES.md §16.2): it skips whitespace,
/// reads a string, a null and an integer token, and skips a whole value it
/// cannot place. Its counters fold into the caller's report.
struct Jr<'a> {
    b: &'a [u8],
    p: usize,
    unknown: u64,
    kind_mismatch: u64,
    clamped: u64,
    malformed: bool,
}

impl<'a> Jr<'a> {
    fn space(&mut self) {
        while self.p < self.b.len() {
            match self.b[self.p] {
                b' ' | b'\t' | b'\n' | b'\r' => self.p += 1,
                _ => break,
            }
        }
    }
    fn peek(&mut self) -> u8 {
        self.space();
        if self.p < self.b.len() {
            self.b[self.p]
        } else {
            0
        }
    }
    fn hex4(&mut self) -> i32 {
        if self.p + 4 > self.b.len() {
            return -1;
        }
        let mut v = 0i32;
        for i in 0..4 {
            let c = self.b[self.p + i];
            let d = match c {
                b'0'..=b'9' => (c - b'0') as i32,
                b'a'..=b'f' => (c - b'a' + 10) as i32,
                b'A'..=b'F' => (c - b'A' + 10) as i32,
                _ => return -1,
            };
            v = v * 16 + d;
        }
        self.p += 4;
        v
    }
    fn read_string(&mut self) -> Option<String> {
        self.space();
        if self.p >= self.b.len() || self.b[self.p] != b'"' {
            self.malformed = true;
            return None;
        }
        self.p += 1;
        let mut out: Vec<u8> = Vec::new();
        loop {
            if self.p >= self.b.len() {
                self.malformed = true;
                return None;
            }
            let c = self.b[self.p];
            if c == b'"' {
                self.p += 1;
                break;
            }
            if c == b'\\' {
                self.p += 1;
                if self.p >= self.b.len() {
                    self.malformed = true;
                    return None;
                }
                let e = self.b[self.p];
                self.p += 1;
                match e {
                    b'"' => out.push(b'"'),
                    b'\\' => out.push(b'\\'),
                    b'/' => out.push(b'/'),
                    b'b' => out.push(0x08),
                    b'f' => out.push(0x0c),
                    b'n' => out.push(b'\n'),
                    b'r' => out.push(b'\r'),
                    b't' => out.push(b'\t'),
                    b'u' => {
                        let v = self.hex4();
                        if v < 0 {
                            self.malformed = true;
                            return None;
                        }
                        let cp = v as u32;
                        if cp < 0x80 {
                            out.push(cp as u8);
                        } else if cp < 0x800 {
                            out.push(0xc0 | (cp >> 6) as u8);
                            out.push(0x80 | (cp & 0x3f) as u8);
                        } else if cp < 0x10000 {
                            out.push(0xe0 | (cp >> 12) as u8);
                            out.push(0x80 | ((cp >> 6) & 0x3f) as u8);
                            out.push(0x80 | (cp & 0x3f) as u8);
                        } else {
                            out.push(0xf0 | (cp >> 18) as u8);
                            out.push(0x80 | ((cp >> 12) & 0x3f) as u8);
                            out.push(0x80 | ((cp >> 6) & 0x3f) as u8);
                            out.push(0x80 | (cp & 0x3f) as u8);
                        }
                    }
                    _ => {
                        self.malformed = true;
                        return None;
                    }
                }
            } else {
                out.push(c);
                self.p += 1;
            }
        }
        Some(String::from_utf8_lossy(&out).into_owned())
    }
    fn read_null(&mut self) -> bool {
        self.space();
        if self.p + 4 <= self.b.len() && &self.b[self.p..self.p + 4] == b"null" {
            self.p += 4;
            true
        } else {
            self.malformed = true;
            false
        }
    }
    fn read_number(&mut self) -> Option<i64> {
        self.space();
        let mut neg = false;
        if self.p < self.b.len() && self.b[self.p] == b'-' {
            neg = true;
            self.p += 1;
        }
        let mut v: i64 = 0;
        let mut any = false;
        while self.p < self.b.len() && self.b[self.p].is_ascii_digit() {
            v = v * 10 + (self.b[self.p] - b'0') as i64;
            any = true;
            self.p += 1;
        }
        if !any {
            self.malformed = true;
            return None;
        }
        if self.p < self.b.len() && (self.b[self.p] == b'.' || self.b[self.p] == b'e' || self.b[self.p] == b'E') {
            while self.p < self.b.len()
                && !matches!(self.b[self.p], b' ' | b'\t' | b'\n' | b'\r' | b',' | b'}' | b']')
            {
                self.p += 1;
            }
            self.kind_mismatch += 1;
            return None;
        }
        if neg {
            Some(-v)
        } else {
            Some(v)
        }
    }
    fn skip_value(&mut self) {
        self.space();
        if self.p >= self.b.len() {
            self.malformed = true;
            return;
        }
        match self.b[self.p] {
            b'"' => {
                let _ = self.read_string();
            }
            b'{' => {
                self.p += 1;
                loop {
                    self.space();
                    if self.p >= self.b.len() {
                        self.malformed = true;
                        return;
                    }
                    if self.b[self.p] == b'}' {
                        self.p += 1;
                        return;
                    }
                    let _ = self.read_string();
                    self.space();
                    if self.p < self.b.len() && self.b[self.p] == b':' {
                        self.p += 1;
                    }
                    self.skip_value();
                    self.space();
                    if self.p < self.b.len() && self.b[self.p] == b',' {
                        self.p += 1;
                    }
                }
            }
            b'[' => {
                self.p += 1;
                loop {
                    self.space();
                    if self.p >= self.b.len() {
                        self.malformed = true;
                        return;
                    }
                    if self.b[self.p] == b']' {
                        self.p += 1;
                        return;
                    }
                    self.skip_value();
                    self.space();
                    if self.p < self.b.len() && self.b[self.p] == b',' {
                        self.p += 1;
                    }
                }
            }
            _ => {
                while self.p < self.b.len()
                    && !matches!(self.b[self.p], b' ' | b'\t' | b'\n' | b'\r' | b',' | b'}' | b']')
                {
                    self.p += 1;
                }
            }
        }
    }
}
`
}

// emitEnumText emits one enum's JSON name maps: variant -> schema name for the
// writer, schema name -> variant for the reader (docs/SPEC-TABLES.md §16.2, the
// enum row's "the variant NAME").
func (g *gen) emitEnumText(b *strings.Builder, e *ir.Enum) {
	write := fn(e.Name, "json_name")
	fmt.Fprintf(b, "fn %s(v: %s) -> &'static str {\n", write, e.Name)
	fmt.Fprintf(b, "    match v {\n")
	fmt.Fprintf(b, "        %s::None => \"None\",\n", e.Name)
	for i, v := range e.Variants {
		fmt.Fprintf(b, "        %s::%s => %q,\n", e.Name, rustVariant(v), v)
		_ = i
	}
	fmt.Fprintf(b, "    }\n}\n\n")

	read := fn(e.Name, "from_json_name")
	fmt.Fprintf(b, "fn %s(s: &str) -> Option<%s> {\n", read, e.Name)
	fmt.Fprintf(b, "    match s {\n")
	fmt.Fprintf(b, "        \"None\" => Some(%s::None),\n", e.Name)
	for _, v := range e.Variants {
		fmt.Fprintf(b, "        %q => Some(%s::%s),\n", v, e.Name, rustVariant(v))
	}
	fmt.Fprintf(b, "        _ => None,\n")
	fmt.Fprintf(b, "    }\n}\n\n")
}

// emitTextWrite emits one table's JSON write: every field in declaration order,
// defaults included (docs/SPEC-TABLES.md §16.1).
func (g *gen) emitTextWrite(b *strings.Builder, st *ir.Struct) {
	write := fn(st.Name, "json_write")
	fmt.Fprintf(b, "fn %s(v: &%s, out: &mut String, depth: usize) {\n", write, st.Name)
	fmt.Fprintf(b, "    out.push('{');\n")
	for i, f := range st.Fields {
		if i > 0 {
			fmt.Fprintf(b, "    out.push(',');\n")
		}
		fmt.Fprintf(b, "    json_newline(out, depth + 1);\n")
		fmt.Fprintf(b, "    json_escape(%q, out);\n", g.fieldKey(f))
		fmt.Fprintf(b, "    out.push_str(\": \");\n")
		g.emitTextWriteValue(b, f, "v."+ir.RustSnake(f.Name), "    ")
	}
	fmt.Fprintf(b, "    json_newline(out, depth);\n")
	fmt.Fprintf(b, "    out.push('}');\n")
	fmt.Fprintf(b, "}\n\n")
}

func (g *gen) fieldKey(f *ir.Field) string {
	if f.JsonKey != "" {
		return f.JsonKey
	}
	return f.Name
}

// emitTextWriteValue emits the value writer for one field.
func (g *gen) emitTextWriteValue(b *strings.Builder, f *ir.Field, expr, ind string) {
	switch {
	case f.IsList():
		elem := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "%sif %s.is_empty() {\n", ind, expr)
		fmt.Fprintf(b, "%s    out.push_str(\"[]\");\n", ind)
		fmt.Fprintf(b, "%s} else {\n", ind)
		fmt.Fprintf(b, "%s    out.push('[');\n", ind)
		fmt.Fprintf(b, "%s    let mut first = true;\n", ind)
		fmt.Fprintf(b, "%s    for e in %s.iter() {\n", ind, expr)
		fmt.Fprintf(b, "%s        if !first { out.push(','); }\n", ind)
		fmt.Fprintf(b, "%s        first = false;\n", ind)
		fmt.Fprintf(b, "%s        json_newline(out, depth + 2);\n", ind)
		fmt.Fprintf(b, "%s        %s(e, out, depth + 2);\n", ind, fn(elem.Name, "json_write"))
		fmt.Fprintf(b, "%s    }\n", ind)
		fmt.Fprintf(b, "%s    json_newline(out, depth + 1);\n", ind)
		fmt.Fprintf(b, "%s    out.push(']');\n", ind)
		fmt.Fprintf(b, "%s}\n", ind)
	case f.Type.Blob():
		fmt.Fprintf(b, "%smatch &%s {\n", ind, expr)
		fmt.Fprintf(b, "%s    Some(s) => json_escape(s, out),\n", ind)
		fmt.Fprintf(b, "%s    None => out.push_str(\"null\"),\n", ind)
		fmt.Fprintf(b, "%s}\n", ind)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "%sjson_escape(&%s, out);\n", ind, expr)
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "%sout.push_str(if %s { \"true\" } else { \"false\" });\n", ind, expr)
	case f.Type.Kind == ir.TInt:
		fmt.Fprintf(b, "%sout.push_str(&%s.to_string());\n", ind, expr)
	case f.Type.Kind == ir.TNamed:
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "%sjson_escape(%s(%s), out);\n", ind, fn(e.Name, "json_name"), expr)
		}
	}
}

// emitTextRead emits one table's JSON read: the tolerant object scan that
// places every key it can name and skips the rest (docs/SPEC-TABLES.md §16.2).
func (g *gen) emitTextRead(b *strings.Builder, st *ir.Struct) {
	read := fn(st.Name, "json_read")
	fmt.Fprintf(b, "fn %s(jr: &mut Jr, v: &mut %s) {\n", read, st.Name)
	fmt.Fprintf(b, "    jr.space();\n")
	fmt.Fprintf(b, "    if jr.peek() != b'{' { jr.malformed = true; return; }\n")
	fmt.Fprintf(b, "    jr.p += 1;\n")
	fmt.Fprintf(b, "    loop {\n")
	fmt.Fprintf(b, "        jr.space();\n")
	fmt.Fprintf(b, "        if jr.peek() == b'}' { jr.p += 1; return; }\n")
	fmt.Fprintf(b, "        let Some(key) = jr.read_string() else { jr.malformed = true; return; };\n")
	fmt.Fprintf(b, "        jr.space();\n")
	fmt.Fprintf(b, "        if jr.peek() != b':' { jr.malformed = true; return; }\n")
	fmt.Fprintf(b, "        jr.p += 1;\n")
	fmt.Fprintf(b, "        match key.as_str() {\n")
	for _, f := range st.Fields {
		g.emitTextReadField(b, f, "v."+ir.RustSnake(f.Name), "            ")
	}
	fmt.Fprintf(b, "            _ => { jr.unknown += 1; jr.skip_value(); }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        jr.space();\n")
	fmt.Fprintf(b, "        let c = jr.peek();\n")
	fmt.Fprintf(b, "        if c == b',' { jr.p += 1; continue; }\n")
	fmt.Fprintf(b, "        if c == b'}' { jr.p += 1; return; }\n")
	fmt.Fprintf(b, "        jr.malformed = true; return;\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "}\n\n")
}

// emitTextReadField emits one field's decode arm inside the read match.
func (g *gen) emitTextReadField(b *strings.Builder, f *ir.Field, field, ind string) {
	fmt.Fprintf(b, "%s%q => {\n", ind, g.fieldKey(f))
	inner := ind + "    "
	switch {
	case f.IsList():
		elem := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "%sjr.space();\n", inner)
		fmt.Fprintf(b, "%sif jr.peek() == b'[' {\n", inner)
		fmt.Fprintf(b, "%s    jr.p += 1;\n", inner)
		fmt.Fprintf(b, "%s    loop {\n", inner)
		fmt.Fprintf(b, "%s        jr.space();\n", inner)
		fmt.Fprintf(b, "%s        if jr.peek() == b']' { jr.p += 1; break; }\n", inner)
		fmt.Fprintf(b, "%s        let mut e = %s::default();\n", inner, elem.Name)
		fmt.Fprintf(b, "%s        %s(jr, &mut e);\n", inner, fn(elem.Name, "json_read"))
		fmt.Fprintf(b, "%s        %s.push(e);\n", inner, field)
		fmt.Fprintf(b, "%s        jr.space();\n", inner)
		fmt.Fprintf(b, "%s        if jr.peek() == b',' { jr.p += 1; continue; }\n", inner)
		fmt.Fprintf(b, "%s        if jr.peek() == b']' { jr.p += 1; break; }\n", inner)
		fmt.Fprintf(b, "%s        jr.malformed = true; break;\n", inner)
		fmt.Fprintf(b, "%s    }\n", inner)
		fmt.Fprintf(b, "%s} else { jr.kind_mismatch += 1; jr.skip_value(); }\n", inner)
	case f.Type.Blob():
		fmt.Fprintf(b, "%sjr.space();\n", inner)
		fmt.Fprintf(b, "%sif jr.peek() == b'n' { jr.read_null(); %s = None; }\n", inner, field)
		fmt.Fprintf(b, "%selse if jr.peek() == b'\"' { if let Some(s) = jr.read_string() { %s = Some(s); } }\n", inner, field)
		fmt.Fprintf(b, "%selse { jr.kind_mismatch += 1; jr.skip_value(); }\n", inner)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "%sif let Some(s) = jr.read_string() { %s = json_clamp(s, %d, &mut jr.clamped); }\n", inner, field, f.Type.Size)
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "%sjr.space();\n", inner)
		fmt.Fprintf(b, "%sif jr.peek() == b't' { jr.p += 4; %s = true; }\n", inner, field)
		fmt.Fprintf(b, "%selse if jr.peek() == b'f' { jr.p += 5; %s = false; }\n", inner, field)
		fmt.Fprintf(b, "%selse { jr.kind_mismatch += 1; jr.skip_value(); }\n", inner)
	case f.Type.Kind == ir.TInt:
		fmt.Fprintf(b, "%sif let Some(n) = jr.read_number() {\n", inner)
		fmt.Fprintf(b, "%s    let mut x = n;\n", inner)
		if f.HasIntRange {
			fmt.Fprintf(b, "%s    if x < %s { x = %s; jr.clamped += 1; }\n", inner, f.IntMin.String(), f.IntMin.String())
			fmt.Fprintf(b, "%s    if x > %s { x = %s; jr.clamped += 1; }\n", inner, f.IntMax.String(), f.IntMax.String())
		}
		fmt.Fprintf(b, "%s    %s = x as %s;\n", inner, field, rustInt(f.Type.Width))
		fmt.Fprintf(b, "%s}\n", inner)
	case f.Type.Kind == ir.TNamed:
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "%sif let Some(s) = jr.read_string() {\n", inner)
			fmt.Fprintf(b, "%s    %s = match %s(&s) {\n", inner, field, fn(e.Name, "from_json_name"))
			fmt.Fprintf(b, "%s        Some(x) => x,\n", inner)
			fmt.Fprintf(b, "%s        None => { jr.unknown += 1; %s::None },\n", inner, e.Name)
			fmt.Fprintf(b, "%s    };\n", inner)
			fmt.Fprintf(b, "%s}\n", inner)
		}
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitTextRoots emits the two public roots: <name>_to_json and <name>_from_json
// (docs/SPEC-TABLES.md §16.1).
func (g *gen) emitTextRoots(b *strings.Builder, st *ir.Struct) {
	write := fn(st.Name, "to_json")
	fmt.Fprintf(b, "/// %s — one %s as one pretty-printed text (docs/SPEC-TABLES.md §16.1).\n", write, st.Name)
	fmt.Fprintf(b, "pub fn %s(v: &%s) -> String {\n", write, st.Name)
	fmt.Fprintf(b, "    let mut out = String::new();\n")
	fmt.Fprintf(b, "    %s(v, &mut out, 0);\n", fn(st.Name, "json_write"))
	fmt.Fprintf(b, "    out.push('\\n');\n")
	fmt.Fprintf(b, "    out\n")
	fmt.Fprintf(b, "}\n\n")

	read := fn(st.Name, "from_json")
	fmt.Fprintf(b, "/// %s — the tolerant read of one text into one %s (docs/SPEC-TABLES.md §16.2).\n", read, st.Name)
	fmt.Fprintf(b, "pub fn %s(text: &str, report: &mut TableReport) -> Option<%s> {\n", read, st.Name)
	fmt.Fprintf(b, "    let mut jr = Jr { b: text.as_bytes(), p: 0, unknown: 0, kind_mismatch: 0, clamped: 0, malformed: false };\n")
	fmt.Fprintf(b, "    let mut v = %s::default();\n", st.Name)
	fmt.Fprintf(b, "    %s(&mut jr, &mut v);\n", fn(st.Name, "json_read"))
	fmt.Fprintf(b, "    jr.space();\n")
	fmt.Fprintf(b, "    report.unknown += jr.unknown;\n")
	fmt.Fprintf(b, "    report.kind_mismatch += jr.kind_mismatch;\n")
	fmt.Fprintf(b, "    report.clamped += jr.clamped;\n")
	fmt.Fprintf(b, "    report.malformed |= jr.malformed;\n")
	fmt.Fprintf(b, "    if jr.malformed { None } else { Some(v) }\n")
	fmt.Fprintf(b, "}\n\n")
}
