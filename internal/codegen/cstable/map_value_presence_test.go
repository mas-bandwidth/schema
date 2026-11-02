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
  var value=new Probe.Root();var report=new Probe.TableReport();
  if(!Probe.Schema.RootFromJson(value,System.Text.Encoding.UTF8.GetBytes(input),report)||report.Malformed||report.Refused||report.KindMismatch!=mismatch||report.Duplicate!=duplicate)throw new Exception("map scalar shape report "+input+" mismatch="+report.KindMismatch+" malformed="+report.Malformed);
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
  if(!Probe.Schema.RootFromJson(value,System.Text.Encoding.UTF8.GetBytes(input),report)||report.Malformed||report.KindMismatch!=2||value.PlainCount!=1||value.Plain[0].Value!=0||value.Scalar!=0)throw new Exception("plain scalar mismatched shape "+token);
 }
`)
}
