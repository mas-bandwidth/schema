// Packet arm selection constructs a fresh payload before reading its body
// (SPEC §4.8). These controls restore the former zero-only selection rule;
// ordinary application constructors and explicit Init helpers remain intact.
// Each copied emitter is compiled through an overlay by make/checks/packet-arm-defaults.mk.
package main

import "maps"

func init() {
	maps.Copy(sabotages, packetArmSabotages)
}

var packetArmSabotages = map[string][]edit{
	"packet-arm-zero-cpp": {{
		old: `		g.pf("            ::new ( (void*) &value.%s ) %s{};\n", v.Name, v.Type)`,
		new: `		g.pf("            ::new ( (void*) &value.%s ) %s{};\n", v.Name, v.Type)
		// SABOTAGED: keep the payload's lifetime valid, then erase its defaults.
		g.needsCstring = true
		g.pf("            memset( (void*) &value.%s, 0, sizeof( value.%s ) );\n", v.Name, v.Name)`,
	}},
	"packet-arm-zero-c": {{
		old: `			g.pf("            value->as.%s = new_%s();\n", v.Name, snake(v.Type))`,
		new: `			// SABOTAGED: selected storage starts at zero, losing declared defaults.
			g.pf("            memset( &value->as.%s, 0, sizeof( value->as.%s ) );\n", v.Name, v.Name)`,
	}},
	"packet-arm-zero-go": {{
		old: `			g.pf("\t\tvalue.%s = New%s() // fresh payload on every selection (SPEC §4.8)\n", ir.GoExportName(v.Name), v.Type)`,
		new: `			g.pf("\t\tvalue.%s = %s{} // SABOTAGED: zero instead of construction defaults\n", ir.GoExportName(v.Name), v.Type)`,
	}},
	"packet-arm-zero-rust": {{
		old: `			g.pf("            let mut arm = %s::new();\n", v.Type)`,
		new: `			g.pf("            let mut arm = %s::default(); // SABOTAGED: zero instead of construction defaults\n", v.Type)`,
	}},
	"packet-arm-zero-cs": {{
		old: `				g.sf("            Init%s(value.%s); // every selection starts from construction defaults\n", v.Type, ir.GoExportName(v.Name))`,
		new: `				g.sf("            Zero%s(value.%s); // SABOTAGED: zero instead of construction defaults\n", v.Type, ir.GoExportName(v.Name))`,
	}},
	"packet-arm-zero-java": {{
		old: `			g.emitInitializeField(nf, arm, ind+"        ", false, true)`,
		new: `			g.emitInitializeField(nf, arm, ind+"        ", false, false) // SABOTAGED: selected storage loses its defaults`,
	}},
	"packet-arm-zero-dart": {{
		old: `			g.emitInitializeField(nf, arm, ind+"    ", false, true)`,
		new: `			g.emitInitializeField(nf, arm, ind+"    ", false, false) // SABOTAGED: selected storage loses its defaults`,
	}},
	"packet-arm-zero-js-runtime": {{
		old: `		g.addRef(v.Type, "Init"+v.Type, "Read"+v.Type)
		g.pf("      Init%s(value.%s); // fresh declared defaults on every selection\n", v.Type, ir.GoExportName(v.Name))`,
		new: `		g.addRef(v.Type, "Zero"+v.Type, "Read"+v.Type)
		g.pf("      Zero%s(value.%s); // SABOTAGED: zero instead of construction defaults\n", v.Type, ir.GoExportName(v.Name))`,
	}},
	"packet-arm-zero-js-flat": {{
		old: `		init := &gen{unit: g.unit, file: g.file, inlineInit: true}
		for _, nf := range vr.Ref.Fields {
			init.emitInitField(nf, arm, ind+"    ")
		}
		g.pf("%s", init.body.String())`,
		new: `		// SABOTAGED: only the flat reader restores the zero-only selection rule.
		for _, nf := range vr.Ref.Fields {
			g.emitZeroFieldFlat(nf, arm, ind+"    ")
		}`,
	}},
}
