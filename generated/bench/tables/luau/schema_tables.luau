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

-- lookupTypeId is the optional cook-check's type lookup. It includes every
-- registered record, so an attributed node is checked even when no edge from
-- the requested root's declaration can reach that type. This scans build
-- metadata only; it allocates no state for nodes in the untrusted image.
function S.lookupTypeId(id: integer): any
	for _, d in registry do
		if d.fields ~= nil and d.id == id then return d end
	end
	return nil
end

-- THE KEYED ACCESSOR (docs/PORTING.md M5; docs/SPEC-TABLES.md §2.4). An
-- enum-keyed array is a plain Luau array with key k at index k, so a bare
-- `array[key]` answers nil at both ends; the refusal lives here, in one place.
-- keyed(table, field) answers { max, get, set, each, key }: get and set take
-- an integer check and ONE range check of key against 1..E.Max (a Luau number
-- is a double, so there is no unsigned wrap to lean on), so None, a negative,
-- non-integer, NaN or infinite key and any key past Max are refused by name in
-- every build; each yields the key beside the element; key(name) is the key
-- of a variant name.
function S.keyed(tableName: string, fieldName: string): any
	local d = S.lookup(tableName)
	local fd: any = nil
	for _, f in d.fields do
		if f.name == fieldName then
			fd = f
			break
		end
	end
	if fd == nil or fd.array ~= "keyed" then
		error("schema: keyed_refused: " .. tableName .. "." .. fieldName .. " is not an enum-keyed array", 2)
	end
	local n: number = fd.bound
	local names: { [string]: number } = {}
	for i, kv in fd.keyvs do
		names[kv[1]] = i
	end
	local function refuse(key: any)
		error("schema: keyed_refused: key " .. tostring(key) .. " of " .. tableName .. "." .. fieldName .. " is not in 1.." .. n, 3)
	end
	local function check(key: any)
		if type(key) ~= "number" or key // 1 ~= key or not (key >= 1 and key <= n) then
			refuse(key)
		end
	end
	local acc = {}
	acc.max = n
	function acc.get(array: { any }, key: number): any
		check(key)
		return array[key]
	end
	function acc.set(array: { any }, key: number, value: any)
		check(key)
		array[key] = value
	end
	function acc.each(array: { any }): () -> (number?, any)
		local key = 0
		return function()
			key += 1
			if key > n then
				return nil, nil
			end
			return key, array[key]
		end
	end
	function acc.key(name: string): number
		local key = names[name]
		if key == nil then
			refuse(name)
		end
		return key :: number
	end
	return table.freeze(acc)
end

return table.freeze(S)
