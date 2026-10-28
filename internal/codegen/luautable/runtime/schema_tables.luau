--!strict
-- schema_tables.luau — the Luau table surface's runtime (docs/PORT-LUAU.md,
-- THE RUNTIME LAYOUT). Emitted beside the generated <Base>Table.luau modules
-- and required as require("./schema_tables"). It holds the DESCRIPTOR types
-- every table module's data has, the constructors that freeze them, and the
-- registry a walk resolves a descriptor's name through: the table codecs
-- (schema_load.luau, schema_save.luau, schema_fixed.luau, schema_message.luau, schema_json.luau, schema_text.luau) are
-- later runtime files beside this one, each walking these descriptors as data
-- (docs/SPEC-TABLES.md §8.1; docs/PORTING.md M13).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

-- A FieldDescriptor is one field, one union arm or one array element, as data
-- (docs/SPEC-TABLES.md §3, §8.1). Every field carries its wire id (the FNV-1a
-- 64 hash of its name or of its was alias, §5), its name, its Luau slot, its
-- text-form key (§16.3), its ordinal o and its header kind k; s is its SHAPE
-- and the rest is what that shape needs (the list is in luautable.go's
-- fieldDesc). An element descriptor carries the shape facts alone.
export type FieldDescriptor = {
	id: integer?,
	name: string?,
	slot: string?,
	json: string?,
	o: number?,
	k: number?,
	s: string,
	tag: number?,
	present: string?,
	required: boolean?,
	deprecated: boolean?,
	t: string?,
	ek: number?,
	e: FieldDescriptor?,
	array: string?,
	bound: number?,
	-- unbounded is the ONE TELL of a container with no declared bound
	-- (docs/SPEC-TABLES.md §8.1): true on a map and a []T, with bound = 0
	-- beside it; false or absent on every other field. It is not the §2.13.1
	-- unbounded TEXT fact.
	unbounded: boolean?,
	min: number?,
	wide: boolean?,
	f32: boolean?,
	res: number?,
	vs: { { any } }?,
	kw: number?,
	signed: boolean?,
	dom: string?,
	bits: number?,
	ibits: number?,
	fbits: number?,
	timestamp: boolean?,
	-- flags is a flags set's variant names in bit order (docs/SPEC-TABLES.md
	-- §16.2): the value is the integer mask, the text form its names
	flags: { string }?,
	lo: any,
	hi: any,
	def: any,
}

-- A TableDescriptor is one table, or one packet type a table reaches by value:
-- its node type id (§3.1, the hash of its wire name), its declared class
-- (§2.2), its integrity trailer when declared (§2.14), what a pointer walk
-- from it may place (§3.1), its fields in declared order, and the constructor
-- of a fresh value.
export type TableDescriptor = {
	name: string,
	id: integer,
	table: boolean,
	fixed: boolean,
	variable: boolean,
	jsonArray: boolean?, -- docs/SPEC-TABLES.md §16.10
	integrity: string?,
	place: { tables: { string }, str: boolean, bytes: boolean },
	fields: { FieldDescriptor },
	new: () -> any,
}

-- A UnionDescriptor is one union: its arms in tag order, each a field line
-- under the arm's id (§2.6), and the constructor of the empty union.
export type UnionDescriptor = {
	name: string,
	arms: { FieldDescriptor },
	new: () -> any,
}

local S = {}

local registry: { [string]: any } = {}

local function freezeField(f: FieldDescriptor)
	if f.e then
		freezeField(f.e :: FieldDescriptor)
	end
	if f.vs then
		for _, v in f.vs :: { { any } } do
			table.freeze(v :: any)
		end
		table.freeze(f.vs :: any)
	end
	table.freeze(f :: any)
end

local function register(name: string, d: any)
	if registry[name] ~= nil then
		error("schema: two descriptors are named " .. name)
	end
	registry[name] = d
end

-- table freezes a table descriptor (a descriptor is a constant) and registers
-- it under its name.
function S.table(d: TableDescriptor): TableDescriptor
	for _, f in d.fields do
		freezeField(f)
	end
	table.freeze(d.fields :: any)
	table.freeze(d.place.tables :: any)
	table.freeze(d.place :: any)
	table.freeze(d :: any)
	register(d.name, d)
	return d
end

-- union freezes a union descriptor and registers it under its name.
function S.union(d: UnionDescriptor): UnionDescriptor
	for _, a in d.arms do
		freezeField(a)
	end
	table.freeze(d.arms :: any)
	table.freeze(d :: any)
	register(d.name, d)
	return d
end

-- wireIds registers a unit's wire ids, the ids this build names
-- (ir.TableWireIds; docs/SPEC-TABLES.md §6.6): every table module of the unit
-- registers the same list, so the set is the union of identical lists.
local known: { [integer]: boolean } = {}
function S.wireIds(ids: { integer })
	for _, id in ids do
		known[id] = true
	end
end

-- knownId says whether this build names the id: the retaining save numbers
-- such an id through the ordinary id table, and only an id it cannot name
-- takes an entry of the caller's retained-id list (§6.6).
function S.knownId(id: integer): boolean
	return known[id] == true
end

-- lookup is the descriptor a field's t names: every module a descriptor
-- names requires the module that registers it, so a loaded root's closure is
-- registered.
function S.lookup(name: string): any
	local d = registry[name]
	if d == nil then
		error("schema: no descriptor is named " .. name)
	end
	return d
end

return table.freeze(S)
