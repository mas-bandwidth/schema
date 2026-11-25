// The single-pass equivalence gate's planted faults (make/c.mk
// tables-single-pass-drop-else-negative-control and
// tables-single-pass-hoist-define-negative-control), in the rewrite
// ccommon.SinglePassConditionals:
//
//   - single-pass-drop-else drops the first #else branch of each file: its
//     directive and its body are not written, so where every earlier
//     condition is false the file keeps nothing that its #else kept.
//   - single-pass-hoist-define moves a guarded #if ahead of the #define
//     between it and its twin: the first "#ifndef N / #define N" whose N is
//     tested again by a later "#ifndef N" loses that #define, which is written
//     after the later #ifndef instead, so the second block is read as if the
//     first had never defined N.
package main

func init() {
	sabotages["single-pass-drop-else"] = []edit{
		{
			old: "func SinglePassConditionals(src string) (string, error) {\n",
			new: "var sabotageElseDropped bool // SABOTAGED\n\nfunc SinglePassConditionals(src string) (string, error) {\n\tsabotageElseDropped = false\n",
		},
		{
			old: "\tfor i, br := range n.branches {\n",
			new: "\tfor i, br := range n.branches {\n\t\tif br.word == \"else\" && !sabotageElseDropped { // SABOTAGED: drop this #else branch\n\t\t\tsabotageElseDropped = true\n\t\t\tbreak\n\t\t}\n",
		},
	}
	sabotages["single-pass-hoist-define"] = []edit{{
		old: "\treturn b.String(), nil\n",
		new: `	return func(s string) string { // SABOTAGED: hoist a guarded #if past the #define before it
		ls := strings.SplitAfter(s, "\n")
		for i := 0; i+1 < len(ls); i++ {
			t := strings.TrimSpace(ls[i])
			if !strings.HasPrefix(t, "#ifndef ") {
				continue
			}
			name := strings.TrimSpace(strings.TrimPrefix(t, "#ifndef "))
			if strings.TrimSpace(ls[i+1]) != "#define "+name {
				continue
			}
			for j := i + 2; j < len(ls); j++ {
				if strings.TrimSpace(ls[j]) == t {
					def := ls[i+1]
					out := append(append(append([]string{}, ls[:i+1]...), ls[i+2:j+1]...), def)
					return strings.Join(append(out, ls[j+1:]...), "")
				}
			}
		}
		return s
	}(b.String()), nil
`,
	}}
}
