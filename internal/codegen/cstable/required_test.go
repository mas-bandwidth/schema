package cstable

import (
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	// The independent generated-Go tests required*, keyedrequired and
	// requiredoffbranch pin this contract: one event per refused body, names in
	// declaration order, compatible wire reads versus named JSON key presence.
	schema := `package probe
table Root {}
table RequiredValue {
 a int32 | required
 b int32 | required
}
table RequiredDefault { a int32 = 9 | required }
table Exact { ver uint8 | min = 7, max = 7, required }
table ExactFloat { amount float32 | min = 0.5, max = 1.5, resolution = 0.1, required }
table ExactWide { amount int128 | min = 7, max = 8, required }
table RequiredRows { values [..4]Other | required }
table RequiredArray { values [..4]int32 | required }
table Other { ver uint8 }
table RequiredEmpty { value Other | required }
table Inner { id int32 | required }
table Outer { inner Inner }
table Sequence { children []Inner }
table Mapping { items map[uint8]Inner }
table Graph { child *Inner }
table Guarded {
 active bool
 if active { req int32 | required }
}
enum Key { A, B }
table Keyed { values [Key]int32 | required }
table EmptyFields {
 text string(8) | required
 values [..4]int32 | required
 items map[uint8]int32 | required
}
`
	oracle := requiredGoWitnesses(t, schema)
	runCarryCS(t, schema, []string{`{}`}, strings.NewReplacer("GO_ZERO", oracle["zero"], "GO_EMPTY", oracle["empty"], "GO_BAD_ARRAY", oracle["bad_array"]).Replace(`unsafe {
 void CheckJSON(TableTypeInfo type, string text, string missing, int mismatch = 0) {
  var value = type.Create(); var report = new TableReport();
  bool ok = Schema.TableJson.Read(value, type, Encoding.UTF8.GetBytes(text), report);
  if (ok != (missing == null) || report.MissingRequired != (missing == null ? 0 : 1) || report.KindMismatch != mismatch || report.Malformed || report.Reason != (missing == null ? null : "missing_required: " + missing)) throw new Exception("required JSON " + type.Name + " " + text + " => " + report.Reason);
 }
 byte[] Wire(byte[] body, params ulong[] ids) {
  byte[] bytes = new byte[1 + body.Length + ids.Length * 8 + 8]; bytes[0] = 1; body.CopyTo(bytes, 1);
  for (int i = 0; i < ids.Length; i++) System.Buffers.Binary.BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(1 + body.Length + i * 8, 8), ids[i]);
  System.Buffers.Binary.BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(bytes.Length - 8), (ulong)ids.Length); return bytes;
 }
 void CheckWire(TableTypeInfo type, byte[] bytes, string missing, int mismatch = 0) {
  var value = type.Create(); var report = new TableReport();
  var verdict = Schema.TableWire.Load(value, type, bytes, report);
  if (verdict != (missing == null ? Schema.TableWire.Verdict.Ok : Schema.TableWire.Verdict.Refused) || report.MissingRequired != (missing == null ? 0 : 1) || report.KindMismatch != mismatch || report.Malformed || report.Reason != (missing == null ? null : "missing_required: " + missing)) throw new Exception("required wire " + type.Name + " => " + report.Reason);
  long n = Schema.TableWire.LoadMeasure(type, bytes, out string why);
  if (n < 0) throw new Exception("required native measure " + why);
  void* memory = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((n+15)&~15L), 16);
  try {
   var nativeReport = new TableReport();
   var result = Schema.TableWire.LoadRegion(type, (IntPtr)memory, n, bytes, nativeReport);
   if ((result != IntPtr.Zero) != (missing == null) || nativeReport.MissingRequired != report.MissingRequired || nativeReport.Reason != report.Reason || nativeReport.Malformed) throw new Exception("required native " + type.Name + " => " + nativeReport.Reason);
   if (missing == null) {
    Span<ulong> vocabulary = stackalloc ulong[4096];
    long size = Schema.TableWire.SaveRegion(result, type, Span<byte>.Empty, vocabulary, true);
    byte[] written = new byte[size];
    if (Schema.TableWire.SaveRegion(result, type, written, vocabulary, false) != bytes.Length || !written.AsSpan().SequenceEqual(bytes)) throw new Exception("required native save " + type.Name);
   }
   TableRetain retain = default;
   var retainedReport = new TableReport();
   result = Schema.TableWire.LoadRetainRegion(type, (IntPtr)memory, n, bytes, ref retain, retainedReport);
   if ((result != IntPtr.Zero) != (missing == null) || retainedReport.MissingRequired != report.MissingRequired || retainedReport.Reason != report.Reason || retainedReport.Malformed) throw new Exception("required retained " + type.Name + " => " + retainedReport.Reason);
   if (missing == null) {
    Span<ulong> vocabulary = stackalloc ulong[4096]; Span<int> slots = stackalloc int[4096];
    long size = Schema.TableWire.SaveRetainRegion(result, type, Span<byte>.Empty, vocabulary, slots, ref retain, null, true);
    byte[] written = new byte[size]; var saveReport = new TableReport();
    if (Schema.TableWire.SaveRetainRegion(result, type, written, vocabulary, slots, ref retain, saveReport, false) != bytes.Length || !written.AsSpan().SequenceEqual(bytes)) throw new Exception("required retained save " + type.Name);
   }
  } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(memory); }
 }
 var type = Schema.RequiredValueTableType();
 byte[] absent = Wire(new byte[]{0});
 byte[] zero = Convert.FromHexString("GO_ZERO");
 CheckJSON(type, """{}""", "a, b");
 CheckJSON(type, """{"a":0}""", "b");
 CheckJSON(type, """{"a":0,"b":0}""", null);
 CheckJSON(type, """{"a":null,"b":0}""", null, 1);
 CheckWire(type, absent, "a, b");
 CheckWire(Schema.RequiredArrayTableType(), Convert.FromHexString("GO_BAD_ARRAY"), "values", 1);
 CheckJSON(Schema.RequiredArrayTableType(), """{"values":{}}""", null, 1);
 CheckWire(type, zero, null);
 CheckWire(type, Wire(new byte[]{1,1,1,2,4,0,0,0,0,0}, type.Fields[0].Id,type.Fields[1].Id), "a", 1);
 var defaultValue = new RequiredValue();
 byte[] saved = new byte[Schema.RequiredValueMeasure(defaultValue)];
 if (Schema.RequiredValueSave(defaultValue,saved) != zero.Length || !saved.AsSpan().SequenceEqual(zero)) throw new Exception("required zero fields elided");
 using (var builder = new RequiredValueBuilder()) {
  var bad = new TableReport();
  if (builder.Load(absent, bad) || bad.MissingRequired != 1 || bad.Reason != "missing_required: a, b") throw new Exception("required builder accepted absence");
  if (!builder.Load(zero, bad) || bad.Refused || bad.Reason != null || bad.MissingRequired != 1) throw new Exception("reused builder report poisoned clean read");
  byte[] result = new byte[builder.Measure()];
  if (builder.Save(result) != zero.Length || !result.AsSpan().SequenceEqual(zero)) throw new Exception("required builder zero fields elided");
 }
 var reuse = new TableReport();
 Schema.RequiredValueFromJson(new RequiredValue(), Encoding.UTF8.GetBytes("""{}"""), reuse);
 if (!Schema.RequiredValueFromJson(new RequiredValue(), Encoding.UTF8.GetBytes("""{"a":0,"b":0}"""), reuse) || reuse.Refused || reuse.Reason != null || reuse.MissingRequired != 1) throw new Exception("reused JSON report poisoned clean read");
 CheckJSON(Schema.RequiredDefaultTableType(), """{}""", "a");
 var declaredDefault = new RequiredDefault(); byte[] declaredWire = new byte[Schema.RequiredDefaultMeasure(declaredDefault)];
 Schema.RequiredDefaultSave(declaredDefault, declaredWire);
 byte[] expectedDefault = Wire(new byte[]{1,4,9,0,0,0,0}, Schema.RequiredDefaultTableType().Fields[0].Id);
 if (!declaredWire.AsSpan().SequenceEqual(expectedDefault)) throw new Exception("required declared default elided");
 CheckWire(Schema.RequiredDefaultTableType(), declaredWire, null);
 CheckJSON(Schema.ExactTableType(), """{}""", "ver");
 CheckJSON(Schema.ExactTableType(), """{"ver":7}""", null);
 if (Schema.ExactMeasure(new Exact()) != -1 || Schema.ExactToJsonMeasure(new Exact()) != -1) throw new Exception("fresh required no-default value saved");
 using (var fresh = new ExactBuilder()) { if (fresh.Measure() != -1) throw new Exception("fresh required native no-default value saved"); }
 if (Schema.ExactFloatMeasure(new ExactFloat()) != -1 || Schema.ExactFloatToJsonMeasure(new ExactFloat()) != -1 || Schema.ExactWideMeasure(new ExactWide()) != -1 || Schema.ExactWideToJsonMeasure(new ExactWide()) != -1) throw new Exception("fresh required ranged float/wide saved");
 CheckJSON(Schema.ExactFloatTableType(), """{"amount":1}""", null);
 CheckJSON(Schema.ExactWideTableType(), """{"amount":7}""", null);
 var rows = new RequiredRows(); byte[] rowWire = new byte[Schema.RequiredRowsMeasure(rows)];
 Schema.RequiredRowsSave(rows, rowWire); CheckWire(Schema.RequiredRowsTableType(), rowWire, null);
 CheckWire(Schema.RequiredRowsTableType(), absent, "values");
 CheckJSON(Schema.OtherTableType(), """{}""", null);
 var emptyChild = new RequiredEmpty(); byte[] childWire = new byte[Schema.RequiredEmptyMeasure(emptyChild)];
 Schema.RequiredEmptySave(emptyChild, childWire); CheckWire(Schema.RequiredEmptyTableType(), childWire, null);
 CheckWire(Schema.RequiredEmptyTableType(), absent, "value");
 CheckJSON(Schema.OuterTableType(), """{"inner":{}}""", "id");
 CheckJSON(Schema.SequenceTableType(), """{"children":[{}]}""", "id");
 CheckJSON(Schema.MappingTableType(), """{"items":{"1":{}}}""", "id");
 CheckJSON(Schema.GraphTableType(), """{"child":{}}""", "id");
 CheckJSON(Schema.GraphTableType(), """{"child":{"id":0}}""", null);
 CheckJSON(Schema.GuardedTableType(), """{}""", null);
 CheckJSON(Schema.GuardedTableType(), """{"active":false}""", null);
 CheckJSON(Schema.GuardedTableType(), """{"active":true}""", "req");
 CheckJSON(Schema.GuardedTableType(), """{"req":0,"active":true}""", null);
 CheckWire(Schema.GuardedTableType(), absent, null);
 CheckWire(Schema.GuardedTableType(), Wire(new byte[]{1,1,1,0}, Schema.GuardedTableType().Fields[0].Id), "req");
 CheckWire(Schema.OuterTableType(), Wire(new byte[]{1,13,1,0,0}, Schema.OuterTableType().Fields[0].Id), "id");
 CheckWire(Schema.SequenceTableType(), Wire(new byte[]{1,14,4,13,1,1,0,0}, Schema.SequenceTableType().Fields[0].Id), "id");
 var mapField = Schema.MappingTableType().Fields[0];
 byte[] badMap = Wire(new byte[]{1,14,11,13,1,8,2,6,1,3,13,1,0,0,0}, mapField.Id, mapField.Table.Fields[0].Id, mapField.Table.Fields[1].Id);
 CheckWire(Schema.MappingTableType(), badMap, "id");
 byte[] badGraph = Wire(new byte[]{1,17,2,2,12,4,1,3,1,0,0}, Schema.GraphTableType().Fields[0].Id, ulong.MaxValue, Schema.InnerTableType().Id);
 CheckWire(Schema.GraphTableType(), badGraph, "id");
 using (var builder = new GraphBuilder()) {
  var bad = new TableReport();
  if (builder.Load(badGraph, bad) || bad.MissingRequired != 1 || bad.Reason != "missing_required: id" || bad.Malformed) throw new Exception("required pointer builder accepted absence");
 }
 CheckJSON(Schema.KeyedTableType(), """{}""", "values");
 CheckJSON(Schema.KeyedTableType(), """{"values":{}}""", null);
 var keyed = new Keyed(); byte[] keyedWire = new byte[Schema.KeyedMeasure(keyed)];
 Schema.KeyedSave(keyed, keyedWire);
 byte[] expectedKeyed = Convert.FromHexString("01011002040000fb674d1acf7b27210100000000000000");

 CheckWire(Schema.KeyedTableType(), keyedWire, null);
 CheckJSON(Schema.EmptyFieldsTableType(), """{}""", "text, values, items");
 CheckJSON(Schema.EmptyFieldsTableType(), """{"text":"","values":[],"items":{}}""", null);
 var empties = new EmptyFields(); byte[] emptyWire = new byte[Schema.EmptyFieldsMeasure(empties)];
 Schema.EmptyFieldsSave(empties, emptyWire); CheckWire(Schema.EmptyFieldsTableType(), emptyWire, null);
 if (!emptyWire.AsSpan().SequenceEqual(Convert.FromHexString("GO_EMPTY"))) throw new Exception("required empty fields differ from generated Go");
 if (!keyedWire.AsSpan().SequenceEqual(expectedKeyed)) throw new Exception("required keyed zero collection differs: " + Convert.ToHexString(keyedWire) + " vs " + Convert.ToHexString(expectedKeyed));
 }`))
}

func TestRequiredCSharpBeyond512(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	var schema strings.Builder
	schema.WriteString("package probe\ntable Root {}\ntable Many {\n")
	for i := range 512 {
		fmt.Fprintf(&schema, " f%d int32\n", i)
	}
	schema.WriteString(" last int32 | required\n}\n")
	runCarryCS(t, schema.String(), []string{`{}`}, `{
 var value = new Many(); var report = new TableReport();
 if (Schema.ManyFromJson(value, Encoding.UTF8.GetBytes("""{}"""), report) || report.MissingRequired != 1 || report.Reason != "missing_required: last") throw new Exception("required field past bitmap window silently ignored");
 report = new TableReport();
 if (!Schema.ManyFromJson(value, Encoding.UTF8.GetBytes("""{"last":0}"""), report) || report.MissingRequired != 0) throw new Exception("required field past bitmap window not seen");
 byte[] absent = {1,0,0,0,0,0,0,0,0,0}; report = new TableReport();
 if (Schema.ManyLoad(value, absent, report) || report.MissingRequired != 1 || report.Reason != "missing_required: last") throw new Exception("required wire field past bitmap window silently ignored");
 byte[] bytes = new byte[Schema.ManyMeasure(value)]; Schema.ManySave(value, bytes); report = new TableReport();
 if (!Schema.ManyLoad(value, bytes, report) || report.MissingRequired != 0) throw new Exception("required wire field past bitmap window not seen");
 }`)
}

// Compile and run the independent Go generator against the same schema. These
// zero-valued witnesses make required-field elision observable in exact bytes.
func requiredGoWitnesses(t *testing.T, schema string) map[string]string {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	output := filepath.Join(dir, "witnesses.json")
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["oracle_test.go"] = []byte(fmt.Sprintf(`package probe
import("encoding/binary";"encoding/hex";"encoding/json";"os";"testing")
func TestRequiredOracle(t *testing.T) {
 values:=map[string]string{}
 var zero RequiredValue
 b:=make([]byte,RequiredValueMeasure(&zero));if RequiredValueSave(&zero,b)!=int64(len(b)){t.Fatal("zero save")};values["zero"]=hex.EncodeToString(b)
 var empty EmptyFields
 b=make([]byte,EmptyFieldsMeasure(&empty));if EmptyFieldsSave(&empty,b)!=int64(len(b)){t.Fatal("empty save")};values["empty"]=hex.EncodeToString(b)
 bad:=[]byte{1,1,14,2,1,0,0};var id [8]byte;binary.LittleEndian.PutUint64(id[:],RequiredArrayTableFields[0].ID);bad=append(bad,id[:]...);bad=append(bad,1,0,0,0,0,0,0,0)
 var builder RequiredArrayBuilder;if !builder.Init(){t.Fatal("init")};defer builder.Shutdown();var report TableReport
 if RequiredArrayLoadBuilderTolerant(&builder,bad,&report)||report.MissingRequired!=1||report.KindMismatch!=1||report.Malformed||report.Reason!="missing_required: values" {t.Fatal("required array element-kind contract",report)}
 values["bad_array"]=hex.EncodeToString(bad)
 encoded,err:=json.Marshal(values);if err!=nil{t.Fatal(err)};if err=os.WriteFile(%q,encoded,0600);err!=nil{t.Fatal(err)}
}
`, output))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "-run", "^TestRequiredOracle$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("independent Go oracle: %v\n%s", err, out)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	return values
}
