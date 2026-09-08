// One printer, with no declaration names: UnitView is its only registry.
// U is supplied by the corpus build, never by a list in this program.
using System;
using System.Globalization;

static class Program
{
    static bool unflattened;
    static string Bool(bool value) => value ? "true" : "false";
    static string Hex(ulong value) => value.ToString("x16",CultureInfo.InvariantCulture);
    static void Annotation(ReadOnlyMemory<string> tags,string doc)
    {
        Console.Write(" tags=["+string.Join(",",tags.ToArray())+"] doc=");
        Console.WriteLine(unflattened?doc:doc.Replace("\\","\\\\").Replace("\n","\\n"));
    }
    static void Vocabulary(string what,U.ViewVocabulary v,string row)
    {
        Console.Write($"{what} {v.Name} file={v.File} max={v.Max} bits={v.StorageBits} variants={v.NumVariants}");
        Annotation(v.Tags,v.Doc);
        foreach(var r in v.Variants.Span)
        {
            Console.Write($"{what} {v.Name} {row} {r.Value} {r.Name} id={Hex(r.Id)}");
            Annotation(r.Tags,r.Doc);
        }
    }
    static U.TableFieldInfo Holder(U.UnitViewInfo unit,string name)
    {
        foreach(var entries in new[]{unit.Tables,unit.Types})
        {
            foreach(var entry in entries.Span)
            {
                foreach(var field in entry.Type.Fields)
                { if(field.Arms!=null && field.TypeName==name) { return field; } }
            }
        }
        return null;
    }
    static void Union(U.UnitViewInfo unit,U.ViewVocabulary v)
    {
        Console.Write($"union {v.Name} file={v.File} max={v.Max} bits={v.StorageBits} variants={v.NumVariants}");
        Annotation(v.Tags,v.Doc);
        var holder=Holder(unit,v.Name);
        foreach(var r in v.Variants.Span)
        {
            Console.Write($"union {v.Name} arm {r.Value} {r.Name} id={Hex(r.Id)} payload={r.PayloadName??"-"} record={(r.Payload!=null?"yes":"no")} field={(r.Field!=null?"yes":"no")}");
            if(r.Field==null) { Console.Write(" kind=- bound=- overlay=-"); }
            else
            {
                Console.Write($" kind={r.Field.Kind} bound={r.Field.ArrayBound} overlay=");
                // Native field offsets are relative to the union payload.
                if(holder==null) { Console.Write("-"); }
                else { Console.Write(r.Field.NativeOffset); }
            }
            Annotation(r.Tags,r.Doc);
        }
    }
    static void Declaration(string what,U.ViewType entry)
    {
        Console.Write($"{what} {entry.Name} file={entry.File} fields={entry.Type.Fields.Length}");
        Annotation(entry.Tags,entry.Doc);
        foreach(var f in entry.Type.Fields)
        {
            Console.Write($"{what} {entry.Name} field {f.Name} json={f.Json??"-"} type={f.TypeName} id={Hex(f.Id)} optional={Bool(f.Optional)}");
            Annotation(f.Tags,f.Doc);
        }
    }
    static void Main(string[] args)
    {
        CultureInfo.CurrentCulture=CultureInfo.InvariantCulture;
        unflattened=args.Length!=0 && args[0]=="unflattened";
        var unit=U.Schema.UnitView();
        Console.WriteLine($"unit package={unit.Package} protocol={Hex(unit.ProtocolId)}");
        foreach(var c in unit.Constants.Span)
        {
            Console.Write($"constant {c.Name} file={c.File} type={c.TypeName} float={Bool(c.IsFloat)} int={c.IntValue} real={Hex(c.IsFloat?unchecked((ulong)BitConverter.DoubleToInt64Bits(c.FloatValue)):0)}");
            Annotation(c.Tags,c.Doc);
        }
        foreach(var v in unit.Enums.Span) { Vocabulary("enum",v,"variant"); }
        foreach(var v in unit.Flags.Span) { Vocabulary("flags",v,"bit"); }
        foreach(var v in unit.Unions.Span) { Union(unit,v); }
        foreach(var v in unit.Types.Span) { Declaration("type",v); }
        foreach(var v in unit.Tables.Span) { Declaration("table",v); }
    }
}
