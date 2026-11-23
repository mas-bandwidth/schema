(* The parse tree of a .schema file, SPEC.md §4.2's grammar. *)

type expr =
  | EInt of Big.t
  | EFloat of string            (* the literal's spelling; converted where used *)
  | EName of string
  | EDot of string * string     (* E.Max, F.Count *)
  | EBin of char * expr * expr
  | ENeg of expr

type attr_value =
  | AExpr of expr
  | AStr of string
  | AQual of string list        (* cpp_native = a::b *)

type attr = { akey : string; aval : attr_value option }

type bound =
  | BExact of expr              (* [N]T, or [E]T when N names an enum *)
  | BUpto of expr               (* [..N]T *)
  | BRange of expr * expr       (* [A..B]T *)
  | BOpen                       (* []T, SPEC-TABLES §2.9 *)

type base =
  | TInt of string              (* int8 .. uint64 *)
  | TInt128 | TUInt128
  | TBits of expr
  | TBool | TF32 | TF64
  | TString of expr option      (* None = unbounded string, §2.13.1 *)
  | TBytes of expr
  | TTimestamp
  | TFixed of expr * expr
  | TUFixed of expr * expr
  | TPtr of string              (* *T, *bytes, *string *)
  | TNamed of string
  | TMap of base * ftype

and ftype = { topt : bool; tarr : bound option; telemopt : bool; tbase : base }

type default =
  | DExpr of expr               (* numbers, and bare words: true/false/variant *)
  | DStr of string
  | DList of string list

type item =
  | IField of field
  | IIf of bool * string * item list * item list  (* negated, guard, then, else *)
  | IConst of expr * expr
  | IReserved of expr
  | IAlign

and field = {
  fname : string;
  ftype : ftype;
  fdefault : default option;
  fattrs : attr list;
  fline : int;
}

type variant = { vname : string; vattrs : attr list }

type arm = { arm_name : string; arm_type : ftype option; arm_attrs : attr list }

type decl =
  | DPackage of string
  | DConst of string * string option * expr * attr list
  | DEnum of string * attr list * variant list
  | DFlags of string * attr list * variant list
  | DType of string * attr list * item list
  | DTable of string * bool * attr list * item list   (* name, fixed, attrs, body *)
  | DUnion of string * attr list * arm list
