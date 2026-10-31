// THE tabledemo UNIT's glue for the Goose conformance leg (main.goose; the
// shell driver copies this file beside the unit's generated modules as
// unit.goose): which roots of tables/examples answer the text surfaces, and
// through which generated calls. A root this file does not name is ABSENT.
import .TablesTable;
import .PackTable;
import .NestedTable;
import .KeyedTable;
import .RangesTable;
import .WideTable;
import .schema_json;

// verdict_of reads one text into a fresh root through F, the root's
// <T>FromJsonTolerant, and appends the conformance verdict
// (test/conformance/README.md, json-hostile): the §4 report, or `refused`.
fn verdict_of<T, F>(v: T&, text: const u8[:], out: u8[>..]&) {
    var r = schema_rt::json_report();
    let ok = F(v, text, r);
    if ok && !r.malformed {
        format(out, r.unknown, ",", r.kind_mismatch, ",", r.widened, ",", r.clamped, ",", r.duplicate, ",false,read\n");
    } else {
        out.append("refused\n");
    }
}

// unit_json_verdict answers false for a root this unit does not carry.
fn unit_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&) -> bool {
    if root == "ArchiveConfig" {
        var v = tabledemo::NewArchiveConfig();
        verdict_of(v, text, out, tabledemo::ArchiveConfigFromJsonTolerant);
        return true;
    }
    if root == "KeyedConfig" {
        var v = tabledemo::NewKeyedConfig();
        verdict_of(v, text, out, tabledemo::KeyedConfigFromJsonTolerant);
        return true;
    }
    if root == "LoadoutConfig" {
        var v = tabledemo::NewLoadoutConfig();
        verdict_of(v, text, out, tabledemo::LoadoutConfigFromJsonTolerant);
        return true;
    }
    if root == "PackConfig" {
        var v = tabledemo::NewPackConfig();
        verdict_of(v, text, out, tabledemo::PackConfigFromJsonTolerant);
        return true;
    }
    if root == "ProfileConfig" {
        var v = tabledemo::NewProfileConfig();
        verdict_of(v, text, out, tabledemo::ProfileConfigFromJsonTolerant);
        return true;
    }
    if root == "RangedSigned" {
        var v = tabledemo::NewRangedSigned();
        verdict_of(v, text, out, tabledemo::RangedSignedFromJsonTolerant);
        return true;
    }
    if root == "RangedUnsigned" {
        var v = tabledemo::NewRangedUnsigned();
        verdict_of(v, text, out, tabledemo::RangedUnsignedFromJsonTolerant);
        return true;
    }
    if root == "RangedWidths" {
        var v = tabledemo::NewRangedWidths();
        verdict_of(v, text, out, tabledemo::RangedWidthsFromJsonTolerant);
        return true;
    }
    if root == "RootConfig" {
        var v = tabledemo::NewRootConfig();
        verdict_of(v, text, out, tabledemo::RootConfigFromJsonTolerant);
        return true;
    }
    if root == "WideBlob" {
        var v = tabledemo::NewWideBlob();
        verdict_of(v, text, out, tabledemo::WideBlobFromJsonTolerant);
        return true;
    }
    return false;
}
