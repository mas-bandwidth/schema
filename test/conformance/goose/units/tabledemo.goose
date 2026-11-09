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
// <T>FromJson, and appends the conformance verdict
// (test/conformance/README.md, json-hostile): the §4 report and the §16 text
// W, the root's <T>ToJson, writes of the value the read left, or `refused`.
fn verdict_of<T, F, W>(v: T&, text: const u8[:], out: u8[>..]&) {
    var r = schema_rt::json_report();
    let ok = F(v, text, r);
    if ok && !r.damaged {
        format(out, r.unknown, ",", r.mismatch, ",", r.widened, ",", r.oor, ",", r.repeated, ",false,read\n");
        if W(v, out) < 0 { out.append("!no value"); }
    } else {
        out.append("refused\n");
    }
}

// unit_json_verdict answers false for a root this unit does not carry.
fn unit_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&) -> bool {
    if root == "ArchiveConfig" {
        var v = tabledemo::NewArchiveConfig();
        verdict_of(v, text, out, tabledemo::ArchiveConfigFromJson, tabledemo::ArchiveConfigToJson);
        return true;
    }
    if root == "KeyedConfig" {
        var v = tabledemo::NewKeyedConfig();
        verdict_of(v, text, out, tabledemo::KeyedConfigFromJson, tabledemo::KeyedConfigToJson);
        return true;
    }
    if root == "LoadoutConfig" {
        var v = tabledemo::NewLoadoutConfig();
        verdict_of(v, text, out, tabledemo::LoadoutConfigFromJson, tabledemo::LoadoutConfigToJson);
        return true;
    }
    if root == "PackConfig" {
        var v = tabledemo::NewPackConfig();
        verdict_of(v, text, out, tabledemo::PackConfigFromJson, tabledemo::PackConfigToJson);
        return true;
    }
    if root == "ProfileConfig" {
        var v = tabledemo::NewProfileConfig();
        verdict_of(v, text, out, tabledemo::ProfileConfigFromJson, tabledemo::ProfileConfigToJson);
        return true;
    }
    if root == "RangedSigned" {
        var v = tabledemo::NewRangedSigned();
        verdict_of(v, text, out, tabledemo::RangedSignedFromJson, tabledemo::RangedSignedToJson);
        return true;
    }
    if root == "RangedUnsigned" {
        var v = tabledemo::NewRangedUnsigned();
        verdict_of(v, text, out, tabledemo::RangedUnsignedFromJson, tabledemo::RangedUnsignedToJson);
        return true;
    }
    if root == "RangedWidths" {
        var v = tabledemo::NewRangedWidths();
        verdict_of(v, text, out, tabledemo::RangedWidthsFromJson, tabledemo::RangedWidthsToJson);
        return true;
    }
    if root == "RootConfig" {
        var v = tabledemo::NewRootConfig();
        verdict_of(v, text, out, tabledemo::RootConfigFromJson, tabledemo::RootConfigToJson);
        return true;
    }
    if root == "WideBlob" {
        var v = tabledemo::NewWideBlob();
        verdict_of(v, text, out, tabledemo::WideBlobFromJson, tabledemo::WideBlobToJson);
        return true;
    }
    return false;
}
