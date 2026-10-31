package javatable

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Exact decimal conversion and raw-scale clamping are independent of double
// precision and of the caller's ability to spell an enormous exponent (§15, §16.2).
func TestJavaVariableWideScalarReports(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
table Root {
 quant fixed(16,16) | min=-8,max=8
 wide int128 | min=-5000000000,max=5000000000
 entity uint128
 tail int32
}
`)
	runVariableProbe(t, u, `package probe;
public final class Driver {
 static void check(boolean ok,String message){if(!ok)throw new AssertionError(message);}
 static RootVariable.Value read(String text,TableVariable.Report report){return RootVariable.fromJson(text.getBytes(java.nio.charset.StandardCharsets.UTF_8),report);}
 public static void main(String[] args){
  TableVariable.Report report=new TableVariable.Report();RootVariable.Value v=read("{\"quant\":1.5,\"quant\":0.1,\"wide\":1e40,\"entity\":340282366920938463463374607431768211455,\"tail\":17}",report);
  check(v!=null&&!report.malformed&&report.kindMismatch==1&&report.clamped==2&&v.quant==98304&&v.wide.hi==0&&v.wide.lo==5000000000L&&v.entity.hi==-1L&&v.entity.lo==-1L&&v.tail==17,"exact fraction, preserved repeat, saturation and reader bound");
  report=new TableVariable.Report();v=read("{\"wide\":1e999999999999,\"entity\":0e999999999999,\"quant\":1e-999999999999}",report);
  check(v!=null&&!report.malformed&&report.clamped==2&&report.kindMismatch==1&&v.wide.lo==5000000000L&&v.entity.hi==0&&v.entity.lo==0,"bounded exponent classification");
  TableVariable.Writer w=new TableVariable.Writer(new byte[256],new TableVariable.Ids(64));
  w.put8(1);w.putLeb(w.ref(RootVariable.F_quant));w.put8(22);w.put32(0x7fffffff);
  w.putLeb(w.ref(RootVariable.F_wide));w.put8(5);w.put64(-42);
  w.putLeb(w.ref(RootVariable.F_tail));w.put8(4);w.put32(17);w.put8(0);w.trailer();
  report=new TableVariable.Report();v=RootVariable.load(java.util.Arrays.copyOf(w.buf,w.off),report);
  check(v!=null&&!report.malformed&&!report.refused&&report.clamped==1&&report.widened==1&&v.quant==524288&&v.wide.hi==-1&&v.wide.lo==-42&&v.tail==17,"raw clamp and signed widening to 128 bits");
  System.out.println("exact fixed/128 fractions, saturation, hostile exponents, raw bounds and widening pass");
 }
}`)
}
