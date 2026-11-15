package cstable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"testing"
)

const mapValuePresenceSchema = `package probe
table Root {
 plain map[string(8)]int32
 optional map[string(8)]?int32
 scalar int32
}
`

func TestMapValuePresenceCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t15RunCS(t, mapValuePresenceSchema, `
 void Check(string input,bool present,int mismatch,int duplicate,int expectedValue=0){
  // THE STRICT READ (docs/SPEC-TABLES.md §4, §6.5, §16.2): a changed kind and a
  // repeated key each refuse the whole load, by name.
  var value=new Probe.Root();var report=new Probe.TableReport();
  bool ok=Probe.Schema.RootFromJson(value,System.Text.Encoding.UTF8.GetBytes(input),report);
  bool accept=mismatch==0&&duplicate==0;
  if(ok!=accept||(accept&&(report.Damaged||report.Refused))||(!accept&&report.Reason==null))throw new Exception("map scalar shape verdict "+input+" ok="+ok+" reason="+report.Reason);
  if(!accept){return;}
  if(value.OptionalCount!=1||value.Optional[0].ValuePresent!=present||value.Optional[0].Value!=expectedValue)throw new Exception("optional map-value presence "+input);
  byte[] wire=new byte[Probe.Schema.RootMeasure(value)];if(Probe.Schema.RootSave(value,wire)!=wire.Length)throw new Exception("map value save");
  var back=new Probe.Root();if(!Probe.Schema.RootLoad(back,wire,new Probe.TableReport())||back.OptionalCount!=1||back.Optional[0].ValuePresent!=present||back.Optional[0].Value!=expectedValue)throw new Exception("map value wire presence");
 }
 Check("""{"optional":{"a":null}}""",false,0,0);
 Check("""{"optional":{"a":7}}""",true,0,0,7);
 Check("""{"optional":{"a":"bad"}}""",true,1,0);
 Check("""{"optional":{"a":[]}}""",true,1,0);
 Check("""{"optional":{"a":7,"a":"bad"}}""",true,1,1);
 Check("""{"optional":{"a":7,"a":null}}""",false,0,1);
 foreach(string token in new[]{"null","\"bad\"","[]","{}","true"}){
  var value=new Probe.Root();var report=new Probe.TableReport();
  string input="{\"plain\":{\"a\":"+token+"},\"scalar\":"+token+"}";
  if(Probe.Schema.RootFromJson(value,System.Text.Encoding.UTF8.GetBytes(input),report)||report.Reason==null)throw new Exception("plain scalar mismatched shape must refuse "+token);
 }
`)
}
