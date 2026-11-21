// Package rubytable emits the Ruby target's TABLE SURFACE
// (docs/SPEC-TABLES.md; docs/PORT-RUBY.md): one generated class per table,
// its runtime descriptors, and the pure-Ruby table runtime under <pkg>/_table/.
package rubytable

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"math/big"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/genheader"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v3/ir"
)

//go:embed runtime/_table.rb runtime/_fixed.rb all:runtime/_table
var runtimeFS embed.FS

//go:embed runtime/_table.rbs
var tableRBSSource string

const runtimeUnitToken = "module SchemaRuntimeUnit\n"

// Generate emits the complete Ruby package for a unit that declares at least
// one table.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateSurfaceLineage(u, DefaultSurfaces(), nil)
}

// GenerateSurface emits the selected surfaces for a table unit.
func GenerateSurface(u *ir.Unit, surfaces Surfaces) (map[string][]byte, error) {
	return GenerateSurfaceLineage(u, surfaces, nil)
}

// GenerateSurfaceLineage emits the selected surfaces and fixed lineage entries.
func GenerateSurfaceLineage(u *ir.Unit, surfaces Surfaces, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if err := ValidateSurfaces(u, surfaces); err != nil {
		return nil, err
	}
	if err := ValidateAvailableSurfaces(surfaces); err != nil {
		return nil, err
	}
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := checkTableNames(u, surfaces); err != nil {
		return nil, err
	}

	mod := ruby.ModuleName(u.Package)
	pkg := u.Package
	blocks := ir.Blocks(u)
	plans := filePlans(u)
	out := map[string][]byte{}

	bases := make([]string, 0, len(plans))
	for _, plan := range plans {
		bases = append(bases, plan.base)
		out[path.Join(pkg, plan.base+".rb")] = []byte(emitFileModule(u, mod, plan, blocks, surfaces))
	}
	sort.Strings(bases)

	out[path.Join(pkg, "_retain_known.rb")] = []byte(emitRetainKnown(u, mod))

	hasMessage := false
	if surfaces.Message {
		if msgMod := messageModule(u, mod); msgMod != "" {
			out[path.Join(pkg, "_message_unit.rb")] = []byte(msgMod)
			hasMessage = true
		}
	}

	if surfaces.Fixed {
		fixedData, derr := emitRubyFixedData(u, mod, lineage)
		if derr != nil {
			return nil, derr
		}
		out[path.Join(pkg, "_fixed_data.rb")] = []byte(fixedData)
	}

	if surfaces.View {
		out[path.Join(pkg, "_view.rb")] = []byte(emitViewModule(u, mod))
	}

	err := fs.WalkDir(runtimeFS, "runtime", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel := strings.TrimPrefix(p, "runtime/")
		if rel == "_table.rbs" {
			return nil
		}
		if rel == "_fixed.rb" && !surfaces.Fixed {
			return nil
		}
		if rel == "_table/message.rb" && !hasMessage {
			return nil
		}
		raw, rerr := runtimeFS.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[path.Join(pkg, rel)] = []byte(scopeRuntime(string(raw), mod))
		return nil
	})
	if err != nil {
		return nil, err
	}

	rtFiles, err := ruby.RuntimeFiles(mod)
	if err != nil {
		return nil, err
	}
	for rel, data := range rtFiles {
		out[path.Join(pkg, rel)] = data
	}

	out[pkg+".rb"] = []byte(emitUnitEntry(u, mod, bases, surfaces, hasMessage))
	out[pkg+".rbs"] = []byte(emitRBS(u, mod, plans, surfaces, hasMessage))
	return out, nil
}

func scopeRuntime(src, mod string) string {
	return strings.Replace(src, runtimeUnitToken, "module "+mod+"\n", 1)
}

var reservedTableFiles = map[string]bool{
	"_table":        true,
	"_retain_known": true,
	"_message_unit": true,
	"_view":         true,
	"_fixed":        true,
	"_fixed_data":   true,
}

var reservedTableConstants = map[string]bool{
	"SchemaTable":          true,
	"FixedLoadReport":      true,
	"BlobRef":              true,
	"TableRetain":          true,
	"TextError":            true,
	"BuildVersionMismatch": true,
	"MessageVocabulary":    true,
	"ANNOUNCEMENT":         true,
	"BUILD_VERSION":        true,
}

var reservedTableInstanceMembers = map[string]bool{
	"measure":      true,
	"load_text":    true,
	"save_text":    true,
	"cook":         true,
	"cook_measure": true,
}

func checkTableNames(u *ir.Unit, surfaces Surfaces) error {
	if err := ruby.CheckNames(u); err != nil {
		return err
	}
	for _, f := range u.Files {
		if reservedTableFiles[strings.ToLower(f.Base)] {
			return fmt.Errorf("ruby: %s.schema uses the reserved runtime file name %s; rename the file", f.Base, f.Base)
		}
		for _, t := range f.Tables {
			if t.IsMapEntry() || t.IsTextListElem() {
				continue
			}
			if reservedTableConstants[t.Name] {
				return fmt.Errorf("ruby: %s.schema: table %s collides with the generated table runtime constant %s; rename the table", f.Base, t.Name, t.Name)
			}
			for _, fld := range t.Fields {
				if strings.HasPrefix(fld.Name, "__") {
					return fmt.Errorf("ruby: %s.schema: %s.%s starts with __, which Ruby reserves", f.Base, t.Name, fld.Name)
				}
				if strings.HasPrefix(fld.Name, ruby.HelperPrefix) {
					return fmt.Errorf("ruby: %s.schema: %s.%s starts with %s, which generated helpers reserve", f.Base, t.Name, fld.Name, ruby.HelperPrefix)
				}
				if reservedTableInstanceMembers[fld.Name] {
					return fmt.Errorf("ruby: %s.schema: %s's field %s is the name of a generated table method; rename the field", f.Base, t.Name, fld.Name)
				}
				if surfaces.Retain && (fld.Name == "load_retain" || fld.Name == "save_retain" || fld.Name == "measure_retain") {
					return fmt.Errorf("ruby: %s.schema: %s's field %s collides with the generated retention API; rename the field", f.Base, t.Name, fld.Name)
				}
				if surfaceCarries(surfaces, t, "json") && (fld.Name == "from_json" || fld.Name == "to_json") {
					return fmt.Errorf("ruby: %s.schema: %s's field %s is the name of a generated JSON method; rename the field", f.Base, t.Name, fld.Name)
				}
				if surfaces.TextForm && surfaceCarries(surfaces, t, "json") && (fld.Name == "from_text_form" || fld.Name == "read_text") {
					return fmt.Errorf("ruby: %s.schema: %s's field %s is the name of a generated text-form method; rename the field", f.Base, t.Name, fld.Name)
				}
			}
		}
		for _, un := range f.TableUnions {
			if reservedTableConstants[un.Name] {
				return fmt.Errorf("ruby: %s.schema: union %s collides with the generated table runtime constant %s; rename the union", f.Base, un.Name, un.Name)
			}
			for _, v := range un.Variants {
				if v.Name == "type" {
					return fmt.Errorf("ruby: %s.schema: union %s arm %s collides with union#type; rename the arm", f.Base, un.Name, v.Name)
				}
			}
		}
	}
	if surfaces.View {
		for _, colliding := range []bool{
			u.Consts["UnitView"] != nil,
			u.Enums["UnitView"] != nil,
			u.Flags["UnitView"] != nil,
			u.Structs["UnitView"] != nil,
			u.Tables["UnitView"] != nil,
			u.Unions["UnitView"] != nil,
			u.TableUnions["UnitView"] != nil,
		} {
			if colliding {
				return fmt.Errorf("ruby: the unit declares UnitView, which collides with the generated UnitView entry point; rename the declaration or omit the view surface")
			}
		}
	}
	return nil
}

func emitUnitEntry(u *ir.Unit, mod string, bases []string, surfaces Surfaces, hasMessage bool) string {
	var b strings.Builder
	b.WriteString("# frozen_string_literal: true\n\n")
	fmt.Fprintf(&b, "# Code generated by the schema compiler from the unit %s. DO NOT EDIT.\n", u.Package)
	b.WriteString(genheader.Line("#") + "\n")
	fmt.Fprintf(&b, "module %s\n  PROTOCOL_ID = 0x%016x\n  BUILD_VERSION = 0x%016x\nend\n\n", mod, u.ProtocolId, ir.BuildVersion(u))
	fmt.Fprintf(&b, "require_relative '%s/_schema'\n", u.Package)
	fmt.Fprintf(&b, "require_relative '%s/serialize'\n", u.Package)
	fmt.Fprintf(&b, "require_relative '%s/_table'\n", u.Package)
	fmt.Fprintf(&b, "require_relative '%s/_retain_known'\n", u.Package)
	if surfaces.Fixed {
		fmt.Fprintf(&b, "require_relative '%s/_fixed'\n", u.Package)
	}
	for _, base := range bases {
		fmt.Fprintf(&b, "require_relative '%s/%s'\n", u.Package, base)
	}
	if surfaces.Fixed {
		fmt.Fprintf(&b, "require_relative '%s/_fixed_data'\n", u.Package)
	}
	if hasMessage {
		fmt.Fprintf(&b, "require_relative '%s/_message_unit'\n", u.Package)
	}
	if surfaces.View {
		fmt.Fprintf(&b, "require_relative '%s/_view'\n", u.Package)
	}
	return b.String()
}

func emitRetainKnown(u *ir.Unit, mod string) string {
	var b strings.Builder
	b.WriteString("# frozen_string_literal: true\n\n")
	fmt.Fprintf(&b, "# Code generated by the schema compiler from the unit %s. DO NOT EDIT.\n", u.Package)
	b.WriteString(genheader.Line("#") + "\n")
	fmt.Fprintf(&b, "module %s\n  module SchemaTable\n    KNOWN = SchemaTable.map_h\n", mod)
	for _, id := range ir.TableWireIds(u) {
		fmt.Fprintf(&b, "    KNOWN[0x%016x] = true\n", id)
	}
	b.WriteString("    KNOWN.freeze\n  end\nend\n")
	return b.String()
}

func emitFileModule(u *ir.Unit, mod string, plan *filePlan, blocks *ir.BlockUnit, surfaces Surfaces) string {
	var b strings.Builder
	b.WriteString("# frozen_string_literal: true\n\n")
	fmt.Fprintf(&b, "# Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", plan.base)
	b.WriteString(genheader.Line("#") + "\n")
	fmt.Fprintf(&b, "module %s\n", mod)

	host := ruby.NewHost(u, fileFor(u, plan.base), mod)
	host.Emit(func(d ir.Decl) bool {
		st, ok := d.(*ir.Struct)
		return !ok || !st.IsTable
	})
	if body := host.Body(); body != "" {
		b.WriteString(body)
	}

	closure := ir.TableClosure(u)
	reachedUnions := tableUnionsReached(u)

	for _, un := range plan.packetUnions {
		if !reachedUnions[un.Name] {
			continue
		}
		fmt.Fprintf(&b, "  class %s\n", un.Name)
		b.WriteString("    class << self\n      attr_accessor :descriptor\n    end\n\n")
		b.WriteString("    def _s_reset\n      _s_zero\n    end\n")
		b.WriteString("  end\n")
		emitUnionDescriptor(&b, u, un)
		fmt.Fprintf(&b, "  SchemaTable.register_union(%s)\n", un.Name)
		if surfaces.Fixed {
			fmt.Fprintf(&b, "  _s_fixed_classes[%s] = %s\n", rbQuote(un.Name), un.Name)
		}
		b.WriteString("\n")
	}

	for _, st := range plan.packets {
		if !closure[st.Name] {
			continue
		}
		fmt.Fprintf(&b, "  class %s\n", st.Name)
		b.WriteString("    class << self\n      attr_accessor :descriptor, :block_row\n    end\n\n")
		b.WriteString("    def _s_reset\n")
		for _, f := range st.Fields {
			fmt.Fprintf(&b, "      @%s = %s\n", f.Name, defaultExprForClass(st.Name, f, true))
		}
		b.WriteString("    end\n")
		b.WriteString("  end\n")
		emitTableDescriptor(&b, u, st, true, surfaces)
		emitValueBlockRow(&b, u, st)
		if surfaces.Fixed {
			fmt.Fprintf(&b, "  _s_fixed_classes[%s] = %s\n\n", rbQuote(st.Name), st.Name)
		}
	}

	for _, un := range orderUnions(plan.unions) {
		emitTableUnion(&b, u, un)
		if surfaces.Fixed {
			fmt.Fprintf(&b, "  _s_fixed_classes[%s] = %s\n\n", rbQuote(un.Name), un.Name)
		}
	}

	for _, st := range orderStructs(plan.tables) {
		for _, f := range st.Fields {
			if f.KeyEnum != "" {
				emitKeyedClass(&b, st.Name, f, surfaces)
			}
		}
		emitTable(&b, u, mod, st, blocks, surfaces)
	}

	s := strings.TrimRight(b.String(), "\n")
	return s + "\nend\n"
}

func keyedClassName(tableName string, f *ir.Field) string {
	return tableName + ir.GoExportName(f.Name) + "KeyedArray"
}

func emitKeyedClass(b *strings.Builder, tableName string, f *ir.Field, surfaces Surfaces) {
	kname := keyedClassName(tableName, f)
	elem := *f
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	elem.ArrayMin = 0
	elem.KeyEnum = ""
	elem.KeyEnumRef = nil
	elemDef := defaultExprForClass(tableName, &elem, false)
	fmt.Fprintf(b, "  class %s < SchemaTable::KeyedArray\n", kname)
	fmt.Fprintf(b, "    def initialize(_key_max = %d, &block)\n", f.ArrayBound)
	fmt.Fprintf(b, "      _s_len = _key_max\n      super(_s_len) { |k| block ? block.call(k) : (%s) }\n", elemDef)
	b.WriteString("    end\n")
	b.WriteString("  end\n")
	if surfaces.Fixed {
		fmt.Fprintf(b, "  _s_fixed_classes[%s] = %s\n", rbQuote(kname), kname)
	}
	b.WriteString("\n")
}

func emitTableUnion(b *strings.Builder, u *ir.Unit, un *ir.Union) {
	fmt.Fprintf(b, "  module %sType\n", un.Name)
	b.WriteString("    None = 0\n")
	for i, v := range un.Variants {
		fmt.Fprintf(b, "    %s = %d\n", ir.GoExportName(v.Name), i+1)
	}
	fmt.Fprintf(b, "    Max = %d\n", un.Max)
	b.WriteString("    NAMES = { 0 => 'None'")
	for i, v := range un.Variants {
		fmt.Fprintf(b, ", %d => %s", i+1, rbQuote(v.Name))
	}
	b.WriteString(" }.freeze\n")
	b.WriteString("  end\n\n")

	fmt.Fprintf(b, "  class %s\n", un.Name)
	b.WriteString("    class << self\n      attr_accessor :descriptor\n    end\n\n")
	syms := []string{":type"}
	hasObjectID := false
	for _, v := range un.Variants {
		if v.F != nil {
			syms = append(syms, ":"+v.Name)
			if v.Name == "object_id" {
				hasObjectID = true
			}
		}
	}
	emitAttrAccessor(b, syms, hasObjectID)
	b.WriteString("    def initialize\n      _s_reset\n    end\n\n")
	b.WriteString("    def _s_reset\n      @type = 0\n")
	for _, v := range un.Variants {
		if v.F != nil {
			fmt.Fprintf(b, "      @%s = %s\n", v.Name, defaultExprForClass(un.Name, v.F, false))
		}
	}
	b.WriteString("    end\n\n")
	fmt.Fprintf(b, "    def ==(other)\n      other.instance_of?(%s) && SchemaTable.graph_eq(self, other)\n    end\n\n", un.Name)
	b.WriteString("    def inspect\n      SchemaTable.graph_inspect(self)\n    end\n")
	b.WriteString("  end\n")
	emitUnionDescriptor(b, u, un)
	fmt.Fprintf(b, "  SchemaTable.register_union(%s)\n\n", un.Name)
}

func emitAttrAccessor(b *strings.Builder, syms []string, hasObjectID bool) {
	if len(syms) == 0 {
		return
	}
	if hasObjectID {
		b.WriteString("    _s_verbose = $VERBOSE\n")
		b.WriteString("    $VERBOSE = nil\n")
		fmt.Fprintf(b, "    attr_accessor %s\n", strings.Join(syms, ", "))
		b.WriteString("    $VERBOSE = _s_verbose\n\n")
	} else {
		fmt.Fprintf(b, "    attr_accessor %s\n\n", strings.Join(syms, ", "))
	}
}

func emitTable(b *strings.Builder, u *ir.Unit, mod string, st *ir.Struct, blocks *ir.BlockUnit, surfaces Surfaces) {
	name := st.Name
	fmt.Fprintf(b, "  class %s\n", name)
	b.WriteString("    class << self\n")
	if surfaces.Fixed && st.FixedDeclared {
		b.WriteString("      attr_accessor :descriptor, :block, :block_row, :fixed_plan_max\n")
	} else {
		b.WriteString("      attr_accessor :descriptor, :block, :block_row\n")
	}
	b.WriteString("    end\n\n")
	if surfaces.Fixed && st.FixedDeclared {
		b.WriteString("    @fixed_plan_max = 0\n\n")
	}

	syms := make([]string, len(st.Fields))
	hasObjectID := false
	for i, f := range st.Fields {
		syms[i] = ":" + f.Name
		if f.Name == "object_id" {
			hasObjectID = true
		}
	}
	emitAttrAccessor(b, syms, hasObjectID)

	b.WriteString("    def initialize\n      _s_reset\n    end\n\n")
	b.WriteString("    def _s_reset\n")
	for _, f := range st.Fields {
		fmt.Fprintf(b, "      @%s = %s\n", f.Name, defaultExprForClass(name, f, false))
	}
	b.WriteString("    end\n\n")

	b.WriteString("    def _s_copy_from(other)\n")
	for _, f := range st.Fields {
		fmt.Fprintf(b, "      self.%s = other.%s\n", f.Name, f.Name)
	}
	b.WriteString("      self\n    end\n\n")

	fmt.Fprintf(b, "    def ==(other)\n      other.instance_of?(%s) && SchemaTable.graph_eq(self, other)\n    end\n\n", name)
	b.WriteString("    def inspect\n      SchemaTable.graph_inspect(self)\n    end\n\n")

	if surfaces.Fixed && fixedRootAdmitted(u, st) {
		b.WriteString("    def self.fixed_load(data, capacity = 4096, report = nil, plan_capacity = nil, **opts)\n")
		b.WriteString("      opts.each_key do |k|\n")
		b.WriteString("        unless k == :capacity || k == :report || k == :plan_capacity\n")
		b.WriteString("          raise ::ArgumentError, \"unknown keyword: #{k.inspect}\"\n")
		b.WriteString("        end\n")
		b.WriteString("      end\n")
		b.WriteString("      capacity = opts[:capacity] if opts.key?(:capacity)\n")
		b.WriteString("      report = opts[:report] if opts.key?(:report)\n")
		b.WriteString("      plan_capacity = opts[:plan_capacity] if opts.key?(:plan_capacity)\n")
		fmt.Fprintf(b, "      ::%s._s_fixed_load(%s, data, capacity, report, plan_capacity)\n", mod, rbQuote(name))
		b.WriteString("    end\n\n")
		b.WriteString("    def self.fixed_save(values)\n")
		fmt.Fprintf(b, "      ::%s._s_fixed_save(%s, self, values)\n", mod, rbQuote(name))
		b.WriteString("    end\n\n")
		b.WriteString("    def self.fixed_measure(count)\n")
		fmt.Fprintf(b, "      ::%s._s_fixed_measure(%s, count)\n", mod, rbQuote(name))
		b.WriteString("    end\n\n")
	}

	switch {
	case st.FixedDeclared && !surfaces.Fixed:
		b.WriteString("    def self.load(_data, _report = nil)\n      raise Refused.new('fixed_surface_not_selected')\n    end\n\n")
		fmt.Fprintf(b, "    def save\n      raise InvalidValue.new(%s, 'fixed_surface_not_selected')\n    end\n\n", rbQuote(name))
		fmt.Fprintf(b, "    def measure\n      raise InvalidValue.new(%s, 'fixed_surface_not_selected')\n    end\n\n", rbQuote(name))
	case st.FixedDeclared:
		fmt.Fprintf(b, "    def self.load(data, report = nil)\n      ::%s._s_fixed_load(%s, data, 1, report, nil)[0]\n    end\n\n", mod, rbQuote(name))
		fmt.Fprintf(b, "    def save\n      ::%s._s_fixed_save(%s, %s, [self])\n    end\n\n", mod, rbQuote(name), name)
		fmt.Fprintf(b, "    def measure\n      ::%s._s_fixed_measure(%s, 1)\n    end\n\n", mod, rbQuote(name))
	default:
		b.WriteString("    def self.load(data, report = nil)\n")
		if st.Integrity == "crc32c" {
			b.WriteString("      data = SchemaTable.open_crc(data)\n")
		}
		fmt.Fprintf(b, "      SchemaTable.load_table(%s, data, report)\n    end\n\n", name)
		b.WriteString("    def save\n")
		if st.Integrity == "crc32c" {
			b.WriteString("      SchemaTable.seal_crc(SchemaTable.save_table(self))\n")
		} else {
			b.WriteString("      SchemaTable.save_table(self)\n")
		}
		b.WriteString("    end\n\n")
		b.WriteString("    def measure\n")
		if st.Integrity == "crc32c" {
			b.WriteString("      SchemaTable.measure_table(self) + 4\n")
		} else {
			b.WriteString("      SchemaTable.measure_table(self)\n")
		}
		b.WriteString("    end\n\n")
	}

	b.WriteString("    def self.measure(value)\n      value.measure\n    end\n\n")

	if surfaces.Retain {
		if ir.VariableTables(u)[name] {
			b.WriteString("    def self.load_retain(data, retain, report = nil)\n")
			if st.Integrity == "crc32c" {
				b.WriteString("      data = SchemaTable.open_crc(data)\n")
			}
			fmt.Fprintf(b, "      SchemaTable.load_table(%s, data, report, retain)\n", name)
			b.WriteString("    end\n\n")

			b.WriteString("    def measure_retain(retain)\n")
			b.WriteString("      raise InvalidValue.new('retain', 'measure_retain requires a TableRetain') unless retain.is_a?(TableRetain)\n")
			if st.Integrity == "crc32c" {
				b.WriteString("      SchemaTable.encode_retain_table(self, retain, SchemaTable::Writer.new(nil), SchemaTable::KNOWN)[0] + 4\n")
			} else {
				b.WriteString("      SchemaTable.encode_retain_table(self, retain, SchemaTable::Writer.new(nil), SchemaTable::KNOWN)[0]\n")
			}
			b.WriteString("    end\n\n")

			b.WriteString("    def save_retain(retain, report)\n")
			b.WriteString("      raise InvalidValue.new('retain', 'save_retain requires a TableRetain') unless retain.is_a?(TableRetain)\n")
			b.WriteString("      raise InvalidValue.new('report', 'save_retain requires a LoadReport') unless report.is_a?(LoadReport)\n")
			b.WriteString("      sink = String.new(encoding: Encoding::ASCII_8BIT)\n")
			b.WriteString("      _, lost = SchemaTable.encode_retain_table(self, retain, SchemaTable::Writer.new(sink), SchemaTable::KNOWN)\n")
			b.WriteString("      report.retain_lost += lost\n")
			if st.Integrity == "crc32c" {
				b.WriteString("      SchemaTable.seal_crc(sink)\n")
			} else {
				b.WriteString("      sink\n")
			}
			b.WriteString("    end\n\n")
		} else {
			b.WriteString("    def self.load_retain(_data, _retain, _report = nil)\n")
			b.WriteString("      raise Refused.new('unsupported_kind', 'load_retain is unavailable for a fixed-class root')\n")
			b.WriteString("    end\n\n")
			b.WriteString("    def measure_retain(_retain)\n")
			b.WriteString("      raise InvalidValue.new('measure_retain', 'a fixed-class root cannot retain unknown fields')\n")
			b.WriteString("    end\n\n")
			b.WriteString("    def save_retain(_retain, _report)\n")
			b.WriteString("      raise InvalidValue.new('save_retain', 'a fixed-class root cannot retain unknown fields')\n")
			b.WriteString("    end\n\n")
		}
	}

	if surfaceCarries(surfaces, st, "json") {
		b.WriteString("    def self.from_json(text, report = nil)\n")
		fmt.Fprintf(b, "      SchemaTable.from_json(%s, text, report)\n", name)
		b.WriteString("    end\n\n")
		b.WriteString("    def to_json(*_args)\n      SchemaTable.to_json(self)\n    end\n\n")
		if surfaces.TextForm {
			b.WriteString("    def self.from_text_form(text, max_bytes: 0, max_tokens: 0, max_nodes: 0, max_allocation: 0, max_depth: 0)\n")
			fmt.Fprintf(b, "      SchemaTable.read_text_form(%s, text, SchemaTable::TextLimits.new(bytes: max_bytes, tokens: max_tokens, nodes: max_nodes, allocation: max_allocation, depth: max_depth))\n", name)
			b.WriteString("    end\n\n")
			b.WriteString("    def self.read_text(text, max_bytes: 0, max_tokens: 0, max_nodes: 0, max_allocation: 0, max_depth: 0)\n")
			b.WriteString("      from_text_form(text, max_bytes: max_bytes, max_tokens: max_tokens, max_nodes: max_nodes, max_allocation: max_allocation, max_depth: max_depth)\n")
			b.WriteString("    end\n\n")
		}
	}

	b.WriteString("    def self.load_text(text, report = nil)\n      load(SchemaTable.decode_text(text), report)\n    end\n\n")
	b.WriteString("    def save_text\n      SchemaTable.encode_text(save)\n    end\n\n")

	if surfaceCarries(surfaces, st, "block") {
		b.WriteString("    def self.block_open(data, build_version: nil, base_offset: 0)\n")
		fmt.Fprintf(b, "      SchemaTable.block_open(%s, data, build_version: build_version, base_offset: base_offset)\n", name)
		b.WriteString("    end\n\n")
		b.WriteString("    def self.block_begin(counts = nil)\n")
		fmt.Fprintf(b, "      SchemaTable.block_begin(%s, counts)\n", name)
		b.WriteString("    end\n\n")
		b.WriteString("    def self.block_max_bytes\n")
		fmt.Fprintf(b, "      SchemaTable.block_max_bytes(%s)\n", name)
		b.WriteString("    end\n\n")
	}

	if surfaceCarries(surfaces, st, "cook") {
		b.WriteString("    def self.cook_open(data, base_offset: 0)\n")
		fmt.Fprintf(b, "      SchemaTable.open_cook(%s, data, base_offset: base_offset)\n", name)
		b.WriteString("    end\n\n")
		b.WriteString("    def self.cook_check(data, base_offset: 0)\n")
		fmt.Fprintf(b, "      SchemaTable.check_cook(%s, data, base_offset: base_offset)\n", name)
		b.WriteString("    end\n\n")
		b.WriteString("    def cook(order: SchemaTable::COOK_ORDER_LITTLE, capacity: nil)\n")
		b.WriteString("      SchemaTable.cook_table(self, order: order, capacity: capacity)\n")
		b.WriteString("    end\n\n")
		b.WriteString("    def cook_measure\n      SchemaTable.cook_measure(self)\n    end\n\n")
	}

	b.WriteString("  end\n")
	emitTableDescriptor(b, u, st, false, surfaces)
	emitBlockDescriptors(b, u, st, blocks)
	if surfaces.Fixed {
		fmt.Fprintf(b, "  _s_fixed_classes[%s] = %s\n\n", rbQuote(name), name)
	}
}

func emitTableDescriptor(b *strings.Builder, u *ir.Unit, st *ir.Struct, valueClass bool, surfaces Surfaces) {
	name := st.Name
	layout := ir.RecordLayout(u, st)
	variable := false
	if !valueClass {
		variable = ir.VariableTables(u)[name]
	}
	guards := structGuards(st)
	fmt.Fprintf(b, "  %s.descriptor = SchemaTable::TableDesc.new(\n", name)
	fmt.Fprintf(b, "    name: %s,\n", rbQuote(name))
	fmt.Fprintf(b, "    doc: %s, tags: %s,\n", rbQuote(st.Doc), rbTags(st.Tags))
	fmt.Fprintf(b, "    table: %s,\n", rbBool(st.IsTable))
	fmt.Fprintf(b, "    type_id: 0x%016x,\n", ir.TableWireId(st.WireName()))
	fmt.Fprintf(b, "    variable: %s,\n", rbBool(variable))
	fmt.Fprintf(b, "    fixed: %s,\n", rbBool(st.FixedDeclared))
	fmt.Fprintf(b, "    integrity: %s,\n", rbQuote(st.Integrity))
	fmt.Fprintf(b, "    json_array: %s,\n", rbBool(st.JsonArray))
	if !valueClass {
		forms := surfaceForms(surfaces, st)
		fmt.Fprintf(b, "    forms: %s,\n", rbTags(forms))
	}
	fmt.Fprintf(b, "    size: %d,\n", layout.Size)
	fmt.Fprintf(b, "    align: %d,\n", layout.Align)
	fmt.Fprintf(b, "    region_align: %d,\n", ir.TableRegionAlign(u))
	fmt.Fprintf(b, "    %s,\n", cookTableColumns(u, st, layout))
	b.WriteString("    fields: [\n")
	for ordinal, f := range st.Fields {
		emitFieldDesc(b, u, st, layout, ordinal, f, valueClass, guards)
	}
	b.WriteString("    ]\n")
	b.WriteString("  )\n")
	if valueClass {
		fmt.Fprintf(b, "  SchemaTable.register_value(%s)\n", name)
	} else {
		fmt.Fprintf(b, "  SchemaTable.register(%s)\n", name)
	}
}

func emitFieldDesc(b *strings.Builder, u *ir.Unit, st *ir.Struct, layout *ir.MemberLayout, ordinal int, f *ir.Field, valueClass bool, guards map[string][]guardTerm) {
	lo, hi := intRange(f)
	codec := codecOf(f)
	b.WriteString("      SchemaTable::FieldDesc.new(\n")
	fmt.Fprintf(b, "        name: %s,\n", rbQuote(f.Name))
	fmt.Fprintf(b, "        json_text_list: %s,\n", rbBool(f.IsTextList()))
	if f.IsTextList() {
		fmt.Fprintf(b, "        json_text_unbounded: %s,\n", rbBool(ir.TextListValue(f).Type.TextUnbounded()))
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		fmt.Fprintf(b, "        json_variants: %s,\n", rbTags(ref.Variants))
	case *ir.Flags:
		fmt.Fprintf(b, "        json_variants: %s,\n", rbTags(ref.Variants))
		b.WriteString("        json_flags: true,\n")
	}
	if f.KeyEnumRef != nil {
		fmt.Fprintf(b, "        json_keys: %s,\n", rbTags(f.KeyEnumRef.Variants))
	}
	fmt.Fprintf(b, "        doc: %s, tags: %s,\n", rbQuote(f.Doc), rbTags(f.Tags))
	fmt.Fprintf(b, "        json_key: %s, type_name: %s,\n", rbQuote(ir.TableFieldJsonKey(f)), rbQuote(ir.TableTypeSpelling(f)))
	if fl := layout.FieldByName(f.Name); fl != nil {
		fmt.Fprintf(b, "        at: %d, pieces: %s, elem_size: %d, elem_align: %d, %s,\n",
			fl.Offset, reflectionPieces(u, f, fl.Offset), reflectionElemSize(u, f), reflectionElemAlign(u, f), reflectionColumns(u, f, fl.Offset))
	}
	fmt.Fprintf(b, "        field_id: 0x%016x,\n", ir.TableFieldWireId(f))
	fmt.Fprintf(b, "        kind: %d,\n", ir.TableWireFieldKind(f))
	fmt.Fprintf(b, "        codec: %s,\n", rbQuote(codec))
	if f.Type.Optional {
		fmt.Fprintf(b, "        inner: %s,\n", rbQuote(innerCodecOf(f)))
	}
	fmt.Fprintf(b, "        ordinal: %d,\n", ordinal)
	fmt.Fprintf(b, "        lo: %s,\n", lo)
	fmt.Fprintf(b, "        hi: %s,\n", hi)
	fmt.Fprintf(b, "        target: %s,\n", rbQuote(targetName(f)))
	fmt.Fprintf(b, "        map_entry: %s,\n", rbQuote(reflectionMapEntry(f)))
	fmt.Fprintf(b, "        pointer: %s,\n", rbBool(f.Type.Pointer))
	fmt.Fprintf(b, "        optional: %s,\n", rbBool(f.Type.Optional))
	fmt.Fprintf(b, "        array: %s,\n", rbBool(f.Array != ir.ArrayNone))
	fmt.Fprintf(b, "        bound: %d,\n", f.ArrayBound)
	fmt.Fprintf(b, "        key_enum: %s,\n", rbQuote(f.KeyEnum))
	if valueClass && f.KeyEnum != "" {
		b.WriteString("        keyed_list: true,\n")
	}
	fmt.Fprintf(b, "        array_kind: %d,\n", int(f.Array))
	fmt.Fprintf(b, "        array_min: %d,\n", f.ArrayMin)
	fmt.Fprintf(b, "        elem_kind: %d,\n", ir.TableWireElemKind(f))
	elemCodec := ""
	if codec == "array" {
		elemCodec = arrayElemCodec(f)
	}
	fmt.Fprintf(b, "        elem_codec: %s,\n", rbQuote(elemCodec))
	fmt.Fprintf(b, "        size: %d,\n", fieldSize(f))
	if f.IsTextList() {
		fmt.Fprintf(b, "        text_total: %d,\n", f.TextTotal)
	}
	fmt.Fprintf(b, "        variant_ids: %s, %s,\n", viewVariantIds(f, true), reflectionVocabularyColumns(f))
	if dep := deprecatedHash(f); dep != "" {
		fmt.Fprintf(b, "        deprecated: %s,\n", dep)
	}
	emitFieldDefaultOption(b, st.Name, f, codec, valueClass, "        ")
	if mode := arrayMode(f); mode != "" {
		fmt.Fprintf(b, "        array_mode: %s,\n", rbQuote(mode))
	}
	if f.ArrayMin != 0 {
		fmt.Fprintf(b, "        min_count: %d,\n", f.ArrayMin)
	}
	if gterms := guards[f.Name]; len(gterms) > 0 {
		fmt.Fprintf(b, "        guards: %s,\n", formatGuardArray(gterms))
	}
	if isFloat(f) {
		fmt.Fprintf(b, "        f32: %s,\n", rbBool(f.Type.Kind == ir.TFloat32))
		fmt.Fprintf(b, "        fmin: %s,\n", floatBound(f, f.FMin))
		fmt.Fprintf(b, "        fmax: %s,\n", floatBound(f, f.FMax))
		fmt.Fprintf(b, "        float_range: %s,\n", rbBool(compressedFloat(f)))
		if compressedFloat(f) {
			fmt.Fprintf(b, "        resolution: %s,\n", rbFloatLiteral(f.Resolution))
		}
	}
	if innerCodecOf(f) == "array" {
		emitOptionalArrayElem(b, f)
	}
	if codec == "keyed" {
		emitKeyedDesc(b, st.Name, f, valueClass)
	} else if f.KeyEnumRef != nil {
		fmt.Fprintf(b, "        keys: %s,\n", viewKeys(f, true))
	}
	b.WriteString("      ),\n")
}

func emitFieldDefaultOption(b *strings.Builder, ownerName string, f *ir.Field, codec string, packetHosted bool, indent string) {
	switch codec {
	case "table", "union", "map", "array", "pointer_array", "union_array", "keyed":
		fmt.Fprintf(b, "%sdefault_factory: -> { %s },\n", indent, defaultExprForClass(ownerName, f, packetHosted))
	default:
		fmt.Fprintf(b, "%sdefault: %s,\n", indent, defaultFor(f, codec))
	}
}

func emitKeyedDesc(b *strings.Builder, ownerName string, f *ir.Field, packetHosted bool) {
	codec := keyedElemCodec(f)
	lo, hi := intRange(f)
	b.WriteString("        elem: SchemaTable::FieldDesc.new(\n")
	fmt.Fprintf(b, "          name: %s,\n", rbQuote(f.Name))
	b.WriteString("          field_id: 0,\n")
	fmt.Fprintf(b, "          kind: %d,\n", ir.TableWireElemKind(f))
	fmt.Fprintf(b, "          codec: %s,\n", rbQuote(codec))
	fmt.Fprintf(b, "          lo: %s,\n", lo)
	fmt.Fprintf(b, "          hi: %s,\n", hi)
	fmt.Fprintf(b, "          target: %s,\n", rbQuote(targetName(f)))
	fmt.Fprintf(b, "          unbounded: %s,\n", rbBool(f.Type.TextUnbounded()))
	fmt.Fprintf(b, "          json_variants: %s,\n", rbJSONFieldVariants(f))
	fmt.Fprintf(b, "          json_flags: %s,\n", rbBool(rbJSONIsFlags(f)))
	fmt.Fprintf(b, "          %s,\n", reflectionElementColumns(f))
	if dep := deprecatedHash(f); dep != "" {
		fmt.Fprintf(b, "          deprecated: %s,\n", dep)
	}
	elem := *f
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	elem.ArrayMin = 0
	elem.KeyEnum = ""
	elem.KeyEnumRef = nil
	emitFieldDefaultOption(b, ownerName, &elem, codec, packetHosted, "          ")
	if isFloat(f) {
		fmt.Fprintf(b, "          f32: %s,\n", rbBool(f.Type.Kind == ir.TFloat32))
		fmt.Fprintf(b, "          fmin: %s,\n", floatBound(f, f.FMin))
		fmt.Fprintf(b, "          fmax: %s,\n", floatBound(f, f.FMax))
		fmt.Fprintf(b, "          float_range: %s,\n", rbBool(compressedFloat(f)))
	}
	b.WriteString("        ),\n")
	fmt.Fprintf(b, "        keys: %s,\n", viewKeys(f, true))
}

func emitOptionalArrayElem(b *strings.Builder, f *ir.Field) {
	lo, hi := intRange(f)
	codec := optionalElemCodecOf(f)
	def := "nil"
	switch codec {
	case "bool":
		def = "false"
	case "int", "enum":
		def = "0"
	case "float32", "float64":
		def = "0.0"
	case "string":
		def = "''"
	case "bytes":
		def = "''.b.freeze"
	}
	b.WriteString("        elem: SchemaTable::FieldDesc.new(\n")
	fmt.Fprintf(b, "          name: %s,\n", rbQuote(f.Name))
	b.WriteString("          field_id: 0,\n")
	fmt.Fprintf(b, "          kind: %d,\n", ir.TableWireElemKind(f))
	fmt.Fprintf(b, "          codec: %s,\n", rbQuote(codec))
	fmt.Fprintf(b, "          lo: %s,\n", lo)
	fmt.Fprintf(b, "          hi: %s,\n", hi)
	fmt.Fprintf(b, "          target: %s,\n", rbQuote(targetName(f)))
	fmt.Fprintf(b, "          unbounded: %s,\n", rbBool(f.Type.TextUnbounded()))
	fmt.Fprintf(b, "          json_variants: %s,\n", rbJSONFieldVariants(f))
	fmt.Fprintf(b, "          json_flags: %s,\n", rbBool(rbJSONIsFlags(f)))
	fmt.Fprintf(b, "          %s,\n", reflectionElementColumns(f))
	if dep := deprecatedHash(f); dep != "" {
		fmt.Fprintf(b, "          deprecated: %s,\n", dep)
	}
	if codec == "table" || codec == "union" {
		fmt.Fprintf(b, "          default_factory: -> { %s.new },\n", targetName(f))
	} else {
		fmt.Fprintf(b, "          default: %s,\n", def)
	}
	if isFloat(f) {
		fmt.Fprintf(b, "          f32: %s,\n", rbBool(f.Type.Kind == ir.TFloat32))
		fmt.Fprintf(b, "          fmin: %s,\n", floatBound(f, f.FMin))
		fmt.Fprintf(b, "          fmax: %s,\n", floatBound(f, f.FMax))
		fmt.Fprintf(b, "          float_range: %s,\n", rbBool(compressedFloat(f)))
	}
	b.WriteString("        ),\n")
}

func emitUnionDescriptor(b *strings.Builder, u *ir.Unit, un *ir.Union) {
	size, _, tagSize, armOffset := ir.UnionLayout(u, un)
	fmt.Fprintf(b, "  %s.descriptor = SchemaTable::UnionDesc.new(\n", un.Name)
	fmt.Fprintf(b, "    name: %s,\n", rbQuote(un.Name))
	fmt.Fprintf(b, "    size: %d, tag_size: %d, arm_offset: %d, doc: %s, tags: %s,\n",
		size, tagSize, armOffset, rbQuote(un.Doc), rbTags(un.Tags))
	b.WriteString("    arms: [\n")
	for tag, v := range un.Variants {
		wire := v.Name
		if v.WasName != "" {
			wire = v.WasName
		}
		kind := ir.TableKindNoPayload
		target := ""
		pointer := false
		if v.F != nil {
			kind = ir.TableWireFieldKind(v.F)
			target = targetName(v.F)
			pointer = v.F.Type.Pointer
		}
		armSize, _ := ir.ArmLayout(u, v)
		armDep := slices.Contains(v.Tags, "deprecated")
		b.WriteString("      SchemaTable::ArmDesc.new(\n")
		fmt.Fprintf(b, "        name: %s,\n", rbQuote(v.Name))
		fmt.Fprintf(b, "        tag: %d,\n", tag+1)
		fmt.Fprintf(b, "        wire_id: 0x%016x,\n", ir.TableWireId(wire))
		fmt.Fprintf(b, "        kind: %d,\n", kind)
		fmt.Fprintf(b, "        target: %s,\n", rbQuote(target))
		fmt.Fprintf(b, "        pointer: %s,\n", rbBool(pointer))
		fmt.Fprintf(b, "        size: %d, doc: %s, tags: %s, deprecated: %s,\n", armSize, rbQuote(v.Doc), rbTags(v.Tags), rbBool(armDep))
		fmt.Fprintf(b, "        field: %s\n", armFieldDesc(u, un, v, tag+1))
		b.WriteString("      ),\n")
	}
	b.WriteString("    ]\n  )\n")
}

func armFieldDesc(u *ir.Unit, un *ir.Union, v ir.UnionVariant, tag int) string {
	f := v.F
	if f == nil {
		return "nil"
	}
	wire := v.Name
	if v.WasName != "" {
		wire = v.WasName
	}
	codec := codecOf(f)
	if codec == "keyed" {
		codec = "unsupported"
	}
	elemCodec := ""
	if codec == "array" {
		elemCodec = arrayElemCodec(f)
	}
	lo, hi := intRange(f)
	dep := "SchemaTable::EMPTY_HASH"
	if d := deprecatedHash(f); d != "" {
		dep = d
	}
	var floatMeta strings.Builder
	if isFloat(f) {
		fmt.Fprintf(&floatMeta, ", f32: %s, fmin: %s, fmax: %s, float_range: %s",
			rbBool(f.Type.Kind == ir.TFloat32), floatBound(f, f.FMin), floatBound(f, f.FMax), rbBool(compressedFloat(f)))
	}
	var extra strings.Builder
	if f.Type.Optional {
		fmt.Fprintf(&extra, ", inner: %s", rbQuote(innerCodecOf(f)))
	}
	if f.IsTextList() {
		fmt.Fprintf(&extra, ", json_text_list: true, json_text_unbounded: %s, text_total: %d",
			rbBool(ir.TextListValue(f).Type.TextUnbounded()), f.TextTotal)
	}
	if mode := arrayMode(f); mode != "" {
		fmt.Fprintf(&extra, ", array_mode: %s", rbQuote(mode))
	}
	if f.ArrayMin != 0 {
		fmt.Fprintf(&extra, ", min_count: %d", f.ArrayMin)
	}
	var defPart string
	switch codec {
	case "table", "union", "map", "array", "pointer_array", "union_array":
		defPart = fmt.Sprintf("default_factory: -> { %s }", defaultExprForClass(un.Name, f, false))
	default:
		defPart = fmt.Sprintf("default: %s", defaultFor(f, codec))
	}
	return fmt.Sprintf("SchemaTable::FieldDesc.new(name: %s, doc: %s, tags: %s, json_key: %s, type_name: %s, at: 0, pieces: %s, elem_align: %d, elem_size: %d, %s, field_id: 0x%016x, kind: %d, codec: %s, ordinal: %d, lo: %s, hi: %s, target: %s, map_entry: %s, pointer: %s, optional: %s, array: %s, bound: %d, key_enum: %s, array_kind: %d, array_min: %d, elem_kind: %d, elem_codec: %s, size: %d, json_variants: %s, json_flags: %s, variant_ids: %s, %s, deprecated: %s%s%s, %s)",
		rbQuote(v.Name), rbQuote(v.Doc), rbTags(v.Tags), rbQuote(ir.TableFieldJsonKey(f)), rbQuote(ir.FieldTypeSpelling(f)),
		reflectionPieces(u, f, 0), reflectionElemAlign(u, f), reflectionElemSize(u, f), reflectionColumns(u, f, 0),
		ir.TableWireId(wire), ir.TableWireFieldKind(f), rbQuote(codec), tag, lo, hi,
		rbQuote(targetName(f)), rbQuote(reflectionMapEntry(f)), rbBool(f.Type.Pointer), rbBool(f.Type.Optional),
		rbBool(f.Array != ir.ArrayNone), f.ArrayBound, rbQuote(f.KeyEnum), int(f.Array), f.ArrayMin,
		ir.TableWireElemKind(f), rbQuote(elemCodec), fieldSize(f), rbJSONFieldVariants(f), rbBool(rbJSONIsFlags(f)),
		viewVariantIds(f, true), reflectionVocabularyColumns(f), dep, floatMeta.String(), extra.String(), defPart)
}

func emitBlockDescriptors(b *strings.Builder, u *ir.Unit, st *ir.Struct, blocks *ir.BlockUnit) {
	name := st.Name
	row := ir.RecordLayout(u, st)
	fmt.Fprintf(b, "  %s.block_row = SchemaTable::BlockRecord.new(\n", name)
	emitBlockRecordBody(b, u, row, nil, false, "    ")
	b.WriteString("  )\n")
	if blocks == nil || !st.CarriesForm("block") {
		fmt.Fprintf(b, "  %s.block = nil\n\n", name)
		return
	}
	bl := blocks.Block(st.Name)
	if bl == nil {
		fmt.Fprintf(b, "  %s.block = nil\n\n", name)
		return
	}
	fmt.Fprintf(b, "  %s.block = SchemaTable::BlockTable.new(\n", name)
	fmt.Fprintf(b, "    name: %s,\n", rbQuote(name))
	b.WriteString("    projection: SchemaTable::BlockRecord.new(\n")
	emitBlockRecordBody(b, u, &bl.Projection, bl, true, "      ")
	b.WriteString("    ),\n")
	if len(bl.Arrays) == 0 {
		b.WriteString("    arrays: SchemaTable::EMPTY_ARR,\n")
	} else {
		b.WriteString("    arrays: [\n")
		for _, a := range bl.Arrays {
			fmt.Fprintf(b, "      SchemaTable::BlockArray.new(name: %s, offset: %d, maximum: %d, stride: %d, elem_align: %d, element: %s),\n",
				rbQuote(a.Field.Name), a.TripleOffset, a.Max, a.Stride, a.ElemAlign(), rbQuote(a.ElemName))
		}
		b.WriteString("    ],\n")
	}
	fmt.Fprintf(b, "    max_bytes: %d\n  )\n\n", bl.MaxBytes)
}

func emitValueBlockRow(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	row := ir.RecordLayout(u, st)
	fmt.Fprintf(b, "  %s.block_row = SchemaTable::BlockRecord.new(\n", st.Name)
	emitBlockRecordBody(b, u, row, nil, false, "    ")
	b.WriteString("  )\n\n")
}

func emitBlockRecordBody(b *strings.Builder, u *ir.Unit, ml *ir.MemberLayout, bl *ir.BlockLayout, projection bool, indent string) {
	fmt.Fprintf(b, "%sname: %s,\n", indent, rbQuote(ml.Name))
	fmt.Fprintf(b, "%ssize: %d,\n", indent, ml.Size)
	fmt.Fprintf(b, "%salign: %d,\n", indent, ml.Align)
	if len(ml.Fields) == 0 {
		fmt.Fprintf(b, "%sfields: SchemaTable::EMPTY_ARR\n", indent)
		return
	}
	fmt.Fprintf(b, "%sfields: [\n", indent)
	for _, fl := range ml.Fields {
		f := fl.Field
		facts := ir.BlockFieldOf(u, f, fl.Offset, projection)
		kind := ir.TableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = ir.TableKindU8
		}
		outOfLine := projection && ir.BlockOutOfLine(f)
		target := ""
		if f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				target = ref.Name
			}
		}
		offsetOfOffset, strideOffset, stride := int64(-1), int64(-1), int64(0)
		if outOfLine && bl != nil {
			if a := bl.ArrayByName(f.Name); a != nil {
				offsetOfOffset, strideOffset, stride = a.OffsetOfOffset, a.StrideOffset, a.Stride
			}
		}
		fmt.Fprintf(b, "%s  SchemaTable::BlockField.new(name: %s, kind: %d, offset: %d, size: %d, out_of_line: %s, counted: %s, optional: %s, array: %s, bound: %d, elem_size: %d, count_offset: %d, present_offset: %d, target: %s, offset_of_offset: %d, stride_offset: %d, stride: %d),\n",
			indent, rbQuote(f.Name), kind, fl.Offset, fl.Size, rbBool(outOfLine), rbBool(facts.Counted),
			rbBool(facts.Optional), rbBool(facts.IsArray), facts.ArrayBound, facts.ElemSize,
			facts.CountOffset, facts.PresentOffset, rbQuote(target), offsetOfOffset, strideOffset, stride)
	}
	fmt.Fprintf(b, "%s]\n", indent)
}

func defaultExprForClass(ownerName string, f *ir.Field, packetHosted bool) string {
	if f.Type.Optional {
		return "nil"
	}
	if f.IsMap() {
		return "SchemaTable.map_h"
	}
	if f.Array != ir.ArrayNone {
		if f.KeyEnum != "" && !packetHosted {
			return keyedClassName(ownerName, f) + ".new"
		}
		elem := *f
		elem.Array = ir.ArrayNone
		elem.ArrayBound = 0
		elem.ArrayMin = 0
		elem.KeyEnum = ""
		elem.KeyEnumRef = nil
		elemExpr := scalarOrNamedDefault(&elem)
		switch f.Array {
		case ir.ArrayList:
			return "SchemaTable.arr"
		case ir.ArrayFixed:
			return fmt.Sprintf("Array.new(%d) { %s }", f.ArrayBound, elemExpr)
		case ir.ArrayCounted:
			if f.ArrayMin == 0 {
				return "SchemaTable.arr"
			}
			return fmt.Sprintf("Array.new(%d) { %s }", f.ArrayMin, elemExpr)
		}
	}
	if f.Type.Pointer {
		return "nil"
	}
	return scalarOrNamedDefault(f)
}

func scalarOrNamedDefault(f *ir.Field) string {
	if f.Type.Pointer {
		return "nil"
	}
	if f.IsTextList() {
		if ir.TextListOptional(f) {
			return "nil"
		}
		return "''"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return rbBool(f.HasDefault && f.DefBool)
	case ir.TInt, ir.TFixed, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case ir.TFloat32, ir.TFloat64:
		return floatDefault(f)
	case ir.TString:
		return rbQuote(string(f.DefBytes))
	case ir.TBytes:
		return rbBytesLiteral(f.DefBytes) + ".b.freeze"
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return ref.Name + ".new"
		case *ir.Union:
			return ref.Name + ".new"
		case *ir.Enum:
			return strconv.FormatInt(enumDefaultValue(f), 10)
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String()
			}
			return "0"
		}
	}
	return "nil"
}

type guardTerm struct {
	cond string
	want bool
}

func structGuards(st *ir.Struct) map[string][]guardTerm {
	out := map[string][]guardTerm{}
	var walk func(items []ir.Item, cond []guardTerm)
	walk = func(items []ir.Item, cond []guardTerm) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					terms := make([]guardTerm, len(cond))
					copy(terms, cond)
					out[item.F.Name] = terms
				}
			case *ir.Branch:
				pos, neg := true, false
				if item.Neg {
					pos, neg = false, true
				}
				walk(item.Then, append(cond[:len(cond):len(cond)], guardTerm{item.Cond, pos}))
				walk(item.Else, append(cond[:len(cond):len(cond)], guardTerm{item.Cond, neg}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

func formatGuardArray(terms []guardTerm) string {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, fmt.Sprintf("[%s, %s].freeze", rbQuote(term.cond), rbBool(term.want)))
	}
	return "[" + strings.Join(parts, ", ") + "].freeze"
}

func arrayMode(f *ir.Field) string {
	switch f.Array {
	case ir.ArrayFixed:
		return "fixed"
	case ir.ArrayCounted:
		return "counted"
	case ir.ArrayList:
		return "list"
	}
	return ""
}

func keyedElemCodec(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TFixed, ir.TBits:
		return "int"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			return "table"
		case *ir.Enum:
			return "enum"
		case *ir.Flags:
			return "int"
		}
	}
	return ""
}

func codecOf(f *ir.Field) string {
	if f.KeyEnum != "" && f.KeyEnumRef != nil && !f.Type.Pointer && !f.Type.Optional && !f.IsMap() && keyedElemCodec(f) != "" {
		return "keyed"
	}
	if f.Array != ir.ArrayNone && !f.IsMap() && f.KeyEnum == "" && !f.Type.Optional {
		if _, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed && !f.Type.Pointer {
			return "union_array"
		}
		if f.Type.Pointer {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
				return "pointer_array"
			}
			return "unsupported"
		}
		element := arrayElemCodec(f)
		if element != "unsupported" {
			return "array"
		}
		return "unsupported"
	}
	if f.Type.Pointer {
		if f.Type.Blob() {
			if f.Type.Kind == ir.TString {
				return "string_blob"
			}
			return "bytes_blob"
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
			return "pointer"
		}
		return "unsupported"
	}
	if f.IsMap() {
		if f.Type.Optional {
			return "optional"
		}
		return "map"
	}
	if f.KeyEnum != "" {
		return "unsupported"
	}
	if f.Type.Optional {
		return "optional"
	}
	if f.Array != ir.ArrayNone {
		return "unsupported"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TFixed, ir.TBits:
		return "int"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			return "table"
		case *ir.Union:
			return "union"
		case *ir.Enum:
			return "enum"
		case *ir.Flags:
			return "int"
		}
	}
	return "unsupported"
}

func elementCodecOf(f *ir.Field) string {
	if f.Type.Pointer {
		return "unsupported"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TFixed, ir.TBits:
		return "int"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			return "table"
		case *ir.Enum:
			return "enum"
		case *ir.Flags:
			return "int"
		}
	}
	return "unsupported"
}

func arrayElemCodec(f *ir.Field) string {
	if f.IsTextList() {
		if ir.TextListOptional(f) {
			return "text_optional"
		}
		return "string"
	}
	return elementCodecOf(f)
}

func innerCodecOf(f *ir.Field) string {
	if !f.Type.Optional {
		return ""
	}
	if f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted {
		switch optionalElemCodecOf(f) {
		case "bool", "int", "table", "enum", "float32", "float64", "string", "bytes":
			return "array"
		}
		return "unsupported"
	}
	single := *f
	single.Type.Optional = false
	return codecOf(&single)
}

func optionalElemCodecOf(f *ir.Field) string {
	elem := *f
	elem.Type.Optional = false
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	elem.ArrayMin = 0
	return codecOf(&elem)
}

func targetName(f *ir.Field) string {
	if f.MapEntry != nil {
		return f.MapEntry.Name
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return ref.Name
	case *ir.Union:
		return ref.Name
	}
	return ""
}

func isFloat(f *ir.Field) bool {
	return f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64
}

func compressedFloat(f *ir.Field) bool {
	return f.Type.Kind == ir.TFloat32 && f.HasFloatRange
}

func floatBound(f *ir.Field, v float64) string {
	if !compressedFloat(f) {
		return "0.0"
	}
	return rbFloatLiteral(v)
}

func rbFloatLiteral(v float64) string {
	if v == 0 && math.Signbit(v) {
		return "-0.0"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func enumDefaultValue(f *ir.Field) int64 {
	ref, ok := f.Type.Ref.(*ir.Enum)
	if !ok || !f.HasDefault || f.DefVariant == "" || f.DefVariant == "None" {
		return 0
	}
	for i, name := range ref.Variants {
		if name == f.DefVariant {
			return int64(i + 1)
		}
	}
	return 0
}

func variantIds(f *ir.Field) string {
	ref, ok := f.Type.Ref.(*ir.Enum)
	if !ok || len(ref.Variants) == 0 {
		return "SchemaTable::EMPTY_ARR"
	}
	ids := make([]string, 0, len(ref.Variants))
	for i := range ref.Variants {
		ids = append(ids, fmt.Sprintf("0x%016x", ir.TableWireId(ref.VariantWireName(i))))
	}
	return "[" + strings.Join(ids, ", ") + "].freeze"
}

func deprecatedHash(f *ir.Field) string {
	ref, ok := f.Type.Ref.(*ir.Enum)
	if !ok {
		return ""
	}
	var pairs []string
	for i := range ref.Variants {
		if i < len(ref.VariantTags) && slices.Contains(ref.VariantTags[i], "deprecated") {
			pairs = append(pairs, fmt.Sprintf("%d => true", i+1))
		}
	}
	if len(pairs) == 0 {
		return ""
	}
	return "{ " + strings.Join(pairs, ", ") + " }.freeze"
}

func fieldSize(f *ir.Field) int64 {
	if f.IsTextList() {
		return ir.TextListValue(f).Type.Size
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		return f.Type.Size
	}
	return 0
}

func defaultFor(f *ir.Field, codec string) string {
	switch codec {
	case "bool":
		return rbBool(f.HasDefault && f.DefBool)
	case "int":
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case "string":
		return rbQuote(string(f.DefBytes))
	case "bytes":
		return rbBytesLiteral(f.DefBytes) + ".b.freeze"
	case "enum":
		return strconv.FormatInt(enumDefaultValue(f), 10)
	case "float32", "float64":
		return floatDefault(f)
	}
	return "nil"
}

func floatDefault(f *ir.Field) string {
	if !f.HasDefault {
		return "0.0"
	}
	if f.Type.Kind == ir.TFloat32 {
		return rbFloatLiteral(float64(float32(f.DefFloat)))
	}
	return rbFloatLiteral(f.DefFloat)
}

func intRange(f *ir.Field) (lo, hi string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if l, h, ok := ir.TableRawRange(f); ok {
			return l.String(), h.String()
		}
	case ir.TInt:
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			return f.IntMin.String(), f.IntMax.String()
		}
	case ir.TBits:
		return "0", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)).String()
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return "0", strconv.FormatInt(ref.Max, 10)
		case *ir.Flags:
			return "0", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1)).String()
		}
	}
	return widthRange(f.Type.Width, f.Type.Signed)
}

func widthRange(width int, signed bool) (string, string) {
	bits := width
	if bits <= 0 {
		bits = 64
	}
	one := big.NewInt(1)
	if signed {
		lo := new(big.Int).Neg(new(big.Int).Lsh(one, uint(bits-1)))
		hi := new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits-1)), one)
		return lo.String(), hi.String()
	}
	hi := new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
	return "0", hi.String()
}

func rbBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func rbQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '"', '#':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func rbBytesLiteral(data []byte) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range data {
		fmt.Fprintf(&b, `\x%02x`, c)
	}
	b.WriteByte('"')
	return b.String()
}

func rbTags(tags []string) string {
	if len(tags) == 0 {
		return "SchemaTable::EMPTY_ARR"
	}
	quoted := make([]string, len(tags))
	for i, t := range tags {
		quoted[i] = rbQuote(t)
	}
	return "[" + strings.Join(quoted, ", ") + "].freeze"
}

func rbJSONFieldVariants(f *ir.Field) string {
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		return rbTags(ref.Variants)
	case *ir.Flags:
		return rbTags(ref.Variants)
	default:
		return "SchemaTable::EMPTY_ARR"
	}
}

func rbJSONIsFlags(f *ir.Field) bool {
	_, ok := f.Type.Ref.(*ir.Flags)
	return ok
}
