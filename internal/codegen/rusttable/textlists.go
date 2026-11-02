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
	textListTotalCheck(b, f, x, ind, refusal)
	fmt.Fprintf(b, "%sfor e in &%s {\n", ind, x)
	if ir.TextListOptional(f) {
		fmt.Fprintf(b, "%s    let Some(text) = &e.value else { continue; };\n", ind)
	} else {
		fmt.Fprintf(b, "%s    let text = &e.value;\n", ind)
	}
	fmt.Fprintf(b, "%s    if text.as_bytes().contains(&0) || text.len() > %d { %s }\n%s}\n", ind, rustTextBound(ir.TextListValue(f)), refusal, ind)
}

func emitPlainTextListElement(b *strings.Builder, f *ir.Field, field, ind string) {
	fmt.Fprintf(b, "%slet Some(length) = leb_read(wire, off) else { report.malformed = true; break; };\n", ind)
	fmt.Fprintf(b, "%sif *off > sub || length > (sub - *off) as u64 { report.malformed = true; break; }\n", ind)
	fmt.Fprintf(b, "%slet bytes = &wire[*off..*off + length as usize]; *off += length as usize;\n", ind)
	fmt.Fprintf(b, "%sif !text_valid(bytes) { report.malformed = true; break; }\n", ind)
	fmt.Fprintf(b, "%slet text = std::str::from_utf8(bytes).unwrap();\n%slet mut cut = text.len().min(%d);\n", ind, ind, rustTextBound(ir.TextListValue(f)))
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
	if f.TextTotal > 0 {
		length := "e.value.len()"
		if ir.TextListOptional(f) {
			length = "e.value.as_ref().map_or(0, |text| text.len())"
		}
		fmt.Fprintf(b, "%slet text_len = %s as u64;\n%sif text_len > %d - text_spent { jr.clamped += 1; text_total_hit = true; } else { text_spent += text_len; %s.push(e); }\n", ind, length, ind, f.TextTotal, field)
	} else {
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	}
}

// A subtraction against the remaining budget cannot overflow for authored lists.
func textListTotalCheck(b *strings.Builder, f *ir.Field, x, ind, refusal string) {
	if !f.IsTextList() || f.TextTotal <= 0 {
		return
	}
	fmt.Fprintf(b, "%s{ let mut text_left = %du64;\n%sfor e in &%s {\n", ind, f.TextTotal, ind, x)
	length := "e.value.len()"
	if ir.TextListOptional(f) {
		length = "e.value.as_ref().map_or(0, |text| text.len())"
	}
	fmt.Fprintf(b, "%s    let length = %s as u64;\n%s    if length > text_left { %s }\n%s    text_left -= length;\n%s} }\n", ind, length, ind, refusal, ind, ind)
}

func rustTextBound(f *ir.Field) int64 {
	if f.Type.TextUnbounded() {
		return 0xffffffff
	}
	return f.Type.Size
}
