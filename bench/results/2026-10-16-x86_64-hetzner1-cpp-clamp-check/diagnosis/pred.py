# scratch: derive <T>FixedInRange from each emitted <T>FixedClampBody (prototype of the emitter change)
import re, sys
def transform(s):
    out = []
    pos = 0
    hdr = re.compile(r"^inline void (\w+)FixedClampBody\( (\w+) & value, int32_t & TABLE_CLAMP_RESTRICT clamped, int32_t & TABLE_CLAMP_RESTRICT damaged \)\n\{\n", re.M)
    for m in hdr.finditer(s):
        name, typ = m.group(1), m.group(2)
        end = s.index("\n}\n", m.end()) + 3
        body = s[m.end():end-3]
        lines = body.split("\n")
        o = []
        i = 0
        while i < len(lines):
            l = lines[i]; st = l.strip()
            ind = l[:len(l)-len(l.lstrip())]
            if st.startswith("(void) value;"):
                o.append(ind + "(void) value;"); i += 1; continue
            mm = re.match(r"clamped \+= (.*);$", st)
            if mm: o.append(ind + "bad |= " + mm.group(1) + ";"); i += 1; continue
            if re.match(r"value\.[\w.\[\]]+ = ", st): i += 1; continue
            mm = re.match(r"if \( (.*) \) \{ .* clamped\+\+; \}$", st)
            if mm: o.append(ind + "bad |= (int) ( " + mm.group(1) + " );"); i += 1; continue
            mm = re.match(r"(\w+)FixedClampBody\( (.*), clamped, damaged \);$", st)
            if mm: o.append(ind + "bad |= (int) !%sFixedInRange( %s );" % (mm.group(1), mm.group(2))); i += 1; continue
            mm = re.match(r"if \( !TableUtf8Valid\( (.*) \) \)$", st)
            if mm:
                o.append(ind + "bad |= (int) !TableUtf8Valid( " + mm.group(1) + " );")
                # skip the { ... } block
                assert lines[i+1].strip() == "{", lines[i+1]
                j = i + 2
                while lines[j].strip() != "}": j += 1
                i = j + 1; continue
            o.append(l); i += 1
        fn = ("TABLE_FIXED_INLINE bool %sFixedInRange( const %s & value )\n{\n    int bad = 0;\n" % (name, typ)) + "\n".join(o) + "\n    return bad == 0;\n}\n\n"
        out.append((m.start(), fn))
    for at, fn in reversed(out):
        s = s[:at] + fn + s[at:]
    return s, len(out)
for p in sys.argv[1:]:
    s = open(p).read()
    s, n = transform(s)
    if "FixedTableFixedClampBody( value, clamped, damaged );\n    // A CONTENT" in s:
        s = s.replace("    FixedTableFixedClampBody( value, clamped, damaged );\n    // A CONTENT", "    if ( !FixedTableFixedInRange( value ) ) { FixedTableFixedClampBody( value, clamped, damaged ); }\n    // A CONTENT", 1)
    open(p, "w").write(s)
    print(p, n)
