package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// Validate authored list storage before a wire or cooked writer can discard an
// over-bound suffix. Rust String already guarantees UTF-8; embedded NUL does not.
func textListStorageCheck(b *strings.Builder, f *ir.Field, x, ind, refusal string) {
	if !f.IsTextList() {
		return
	}
	if f.Array == ir.ArrayCounted {
		fmt.Fprintf(b, "%sif %s.len() > %d { %s }\n", ind, x, f.ArrayBound, refusal)
	}
	fmt.Fprintf(b, "%sfor e in &%s {\n", ind, x)
	if ir.TextListOptional(f) {
		fmt.Fprintf(b, "%s    let Some(text) = &e.value else { continue; };\n", ind)
	} else {
		fmt.Fprintf(b, "%s    let text = &e.value;\n", ind)
	}
	fmt.Fprintf(b, "%s    if text.as_bytes().contains(&0) || text.len() > %d { %s }\n%s}\n", ind, ir.TextListValue(f).Type.Size, refusal, ind)
}

func emitPlainTextListElement(b *strings.Builder, f *ir.Field, field, ind string) {
	fmt.Fprintf(b, "%slet Some(length) = leb_read(wire, off) else { report.malformed = true; break; };\n", ind)
	fmt.Fprintf(b, "%sif *off > sub || length > (sub - *off) as u64 { report.malformed = true; break; }\n", ind)
	fmt.Fprintf(b, "%slet bytes = &wire[*off..*off + length as usize]; *off += length as usize;\n", ind)
	fmt.Fprintf(b, "%sif !text_valid(bytes) { report.malformed = true; break; }\n", ind)
	fmt.Fprintf(b, "%slet text = std::str::from_utf8(bytes).unwrap();\n%slet mut cut = text.len().min(%d);\n", ind, ind, ir.TextListValue(f).Type.Size)
	fmt.Fprintf(b, "%swhile !text.is_char_boundary(cut) { cut -= 1; }\n", ind)
	fmt.Fprintf(b, "%sif cut != text.len() { report.clamped += 1; }\n", ind)
	fmt.Fprintf(b, "%s%s.push(%s { value: text[..cut].to_string() });\n", ind, field, f.Type.Name)
}

func (g *gen) emitTextListJSONElement(b *strings.Builder, f *ir.Field, field, ind string) {
	fmt.Fprintf(b, "%slet mut e = %s::default();\n", ind, f.Type.Name)
	value := *ir.TextListValue(f)
	if value.Type.Optional {
		value.Type.Optional = false
		fmt.Fprintf(b, "%sif jr.peek() == b'\\\"' {\n%s    let mut text = String::new();\n", ind, ind)
		g.emitTextReadScalar(b, &value, "text", ind+"    ")
		fmt.Fprintf(b, "%s    e.value = Some(text);\n%s} else if jr.peek() == b'n' { jr.read_null(); } else { jr.kind_mismatch += 1; jr.skip_value(); }\n", ind, ind)
	} else {
		g.emitTextReadScalar(b, &value, "e.value", ind)
	}
	fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
}
