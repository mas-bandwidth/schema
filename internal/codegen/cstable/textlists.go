package cstable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitTextListSurface(st *ir.Struct) {
	for index, f := range st.Fields {
		if !f.IsTextList() {
			continue
		}
		prefix := st.Name + member(f)
		descriptor := fmt.Sprintf("%sTableType().Fields[%d]", st.Name, index)
		g.pf("public static ReadOnlySpan<byte> %sTextAt(%s value, int index, out bool present) { return TableWire.TextAt(value, %s, index, out present); }\n", prefix, st.Name, descriptor)
		g.pf("public static bool %sAppend(%s value, ReadOnlySpan<byte> text) { return TableWire.TextAppend(value, %s, text, true); }\n", prefix, st.Name, descriptor)
		if ir.TextListOptional(f) {
			g.pf("public static bool %sAppendAbsent(%s value) { return TableWire.TextAppend(value, %s, ReadOnlySpan<byte>.Empty, false); }\n", prefix, st.Name, descriptor)
		}
		g.pf("public static unsafe ReadOnlySpan<byte> %sTextAt(%sRow* value, int index, out bool present) { return TableWire.TextAtNative((IntPtr)value, %s, index, out present); }\n", prefix, st.Name, descriptor)
		args, worker := "", "null"
		if f.IsList() || ir.TextListUnbounded(f) {
			args, worker = "TableWorker worker, ", "worker"
		}
		g.pf("public static unsafe bool %sAppend(%s%sRow* value, ReadOnlySpan<byte> text) { return TableWire.TextAppendNative(%s, (IntPtr)value, %s, text, true); }\n", prefix, args, st.Name, worker, descriptor)
		if ir.TextListOptional(f) {
			g.pf("public static unsafe bool %sAppendAbsent(%s%sRow* value) { return TableWire.TextAppendNative(%s, (IntPtr)value, %s, ReadOnlySpan<byte>.Empty, false); }\n", prefix, args, st.Name, worker, descriptor)
		}
	}
}

const tableTextListSource = `
public static partial class TableWire
{
    internal static bool TextFits(object value, TableFieldInfo field, int extra = 0)
    {
        if(field.TextTotal==0){return true;}long spent=extra;
        TableFieldInfo text=field.Table.Fields[0];int count=Count(value,field);
        for(int i=0;i<count;i++){object child=field.GetChild(value,i);if(!text.Optional||text.GetPresent(child)){int length=Count(child,text);if(length<0){return false;}spent+=length;if(spent>field.TextTotal){return false;}}}
        return spent<=field.TextTotal;
    }
    internal static bool TextTotalKeep(TableFieldInfo field, int length, ref long spent, TableReport report)
    {
        if(field.TextTotal==0){return true;}spent+=length;
        if(spent<=field.TextTotal){return true;}TextTotalRefuse(report,field);return false;
    }
    public static ReadOnlySpan<byte> TextAt(object value, TableFieldInfo field, int index, out bool present)
    {
        present=false;
        if(value==null||index<0||index>=Count(value,field)){return default;}
        object child=field.GetChild(value,index);TableFieldInfo text=field.Table.Fields[0];
        if(text.Optional&&!text.GetPresent(child)){return default;}
        present=true;return text.GetBuffer(child).AsSpan(0,Count(child,text));
    }
    public static bool TextAppend(object value, TableFieldInfo field, ReadOnlySpan<byte> bytes, bool present)
    {
        if(value==null){return false;}
        TableFieldInfo text=field.Table.Fields[0];int count=Count(value,field);
        if(count<0||count>=field.ArrayBound||!present&&!text.Optional||present&&(bytes.Length>text.ArrayBound||!TextValid(bytes))){return false;}
        if(!TextFits(value,field,present?bytes.Length:0)){return false;}
        if(field.Dynamic){field.EnsureCount(value,count+1);}
        object child=field.GetChild(value,count);field.Table.Reset(child);
        if(present){if(text.Unbounded){text.EnsureCount(child,bytes.Length);}bytes.CopyTo(text.GetBuffer(child));text.SetCount(child,bytes.Length);}
        if(text.Optional){text.SetPresent(child,present);}field.SetCount(value,count+1);return true;
    }
}
`

const tableNativeTextListSource = `
    static unsafe bool TextFitsRegion(NativeValue value, TableFieldInfo field, int extra = 0)
    {
        if(field.TextTotal==0){return true;}long spent=extra;
        TableFieldInfo text=field.Table.Fields[0];int count=value.Count(field);
        for(int i=0;i<count;i++){NativeValue child=value.Child(field,i);if(!text.Optional||child.Present(text)){int length=child.Count(text);if(length<0){return false;}spent+=length;if(spent>field.TextTotal){return false;}}}
        return spent<=field.TextTotal;
    }
    public static unsafe ReadOnlySpan<byte> TextAtNative(IntPtr pointer, TableFieldInfo field, int index, out bool present)
    {
        present=false;if(pointer==IntPtr.Zero||index<0){return default;}
        NativeValue value=new NativeValue((byte*)pointer,0){Mutable=true};
        if(index>=value.Count(field)){return default;}
        NativeValue child=value.Child(field,index);TableFieldInfo text=field.Table.Fields[0];
        if(text.Optional&&!child.Present(text)){return default;}
        present=true;return child.Buffer(text).Slice(0,child.Count(text));
    }
    public static unsafe bool TextAppendNative(TableWorker worker, IntPtr pointer, TableFieldInfo field, ReadOnlySpan<byte> bytes, bool present)
    {
        if(pointer==IntPtr.Zero||worker!=null&&worker.Arena.Closed){return false;}
        NativeValue value=new NativeValue((byte*)pointer,0){Mutable=true};TableFieldInfo text=field.Table.Fields[0];int count=value.Count(field);
        if(count<0||count>=field.ArrayBound||!present&&!text.Optional||present&&(bytes.Length>text.ArrayBound||!TextValid(bytes))){return false;}
        if(!TextFitsRegion(value,field,present?bytes.Length:0)){return false;}
        if(field.Dynamic&&(worker==null||!TableListStorage.Reserve(worker,(TableCookList*)(value.Base+value.At+field.NativeOffset),field,count+1))){return false;}
        NativeValue child=value.Child(field,count);NativeReset(child,field.Table);
        if(present){if(text.Unbounded){if(!BuilderTextSet(worker,child,text,bytes)){return false;}}else{bytes.CopyTo(child.Buffer(text));child.SetCount(text,bytes.Length);}}
        if(text.Optional){child.SetPresent(text,present);}value.SetCount(field,count+1);return true;
    }
`
