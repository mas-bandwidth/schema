package rusttable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"strings"
	"testing"
)

func TestRustRequiredRangeWritersNative(t *testing.T) {
	slowtest.Gate(t, "rustc (Required active range preflight)")
	keyedRequireRustc(t)
	source, e := os.ReadFile("../../../testdata/conformance/feature-witnesses/required/range-writers/Probe.schema")
	if e != nil {
		t.Fatal(e)
	}
	u := unitFrom(t, string(source))
	probe := `fn good()->R {R{ints:vec![5],wide:vec![9007199254740993,9007199254740993],keyed:vec![5,5],mask:5}}
 fn refuse(v:&R){assert!(r_save_checked(v).is_err());assert!(r_to_json_checked(v).is_none());for order in [1,2]{assert!(r_cook_write(v,order).is_err());}let mut ret=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};assert!(std::panic::catch_unwind(std::panic::AssertUnwindSafe(||r_save_retain(v,&mut ret,&mut TableReport::default()))).is_err());}
 {let v=good();assert!(r_save_checked(&v).is_ok());assert!(r_cook_write(&v,1).is_ok());let _=r_to_json(&v);}
 {let mut v=good();v.ints[0]=4;refuse(&v);}
 {let mut v=good();v.wide[0]=9007199254740992;refuse(&v);}
 {let mut v=good();v.keyed[1]=0;refuse(&v);}
 {let mut v=good();v.keyed.pop();refuse(&v);}
 {let mut v=good();v.wide.pop();refuse(&v);}
 {let mut v=good();v.mask=16777216;refuse(&v);}
 {let v=Guarded::default();assert!(guarded_save_checked(&v).is_ok());assert!(guarded_cook_write(&v,1).is_ok());let _=guarded_to_json(&v);let v=Guarded{on:true,n:0};assert!(guarded_save_checked(&v).is_err());assert!(guarded_cook_write(&v,1).is_err());assert!(guarded_to_json_checked(&v).is_none());}
 {let v=Holder{value:Some(std::rc::Rc::new(R::default()))};assert!(holder_save_checked(&v).is_err());assert!(holder_cook_write(&v,1).is_err());assert!(holder_to_json_checked(&v).is_none());}
 println!("required ranges ok");`
	out, e := rustConstructProbe(t, u, probe)
	if e != nil || !strings.Contains(out, "required ranges ok") {
		t.Fatalf("required ranges: %v\n%s", e, out)
	}
}
