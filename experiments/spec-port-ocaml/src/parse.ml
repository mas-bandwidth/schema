(* Scanner and recursive-descent parser for .schema text (SPEC.md §4.1, §4.2).
   The scanner emits every newline as a token; the parser skips the ones §4.1
   suppresses.  SPEC-GAP 1: the spec does not define a serialized IR a port can
   receive, so a port parses the schema text itself. *)

open Ast

exception Error of string * int

type tok =
  | Ident of string
  | IntLit of Big.t
  | FloatLit of string
  | StrLit of string
  | Punct of string
  | NL
  | EOF

type lexer = { src : string; mutable pos : int; mutable line : int }

let error lx msg = raise (Error (msg, lx.line))

let is_ident_start c = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c = '_'
let is_ident c = is_ident_start c || (c >= '0' && c <= '9')
let is_digit c = c >= '0' && c <= '9'

let peekc lx k = if lx.pos + k < String.length lx.src then lx.src.[lx.pos + k] else '\000'

let rec next_tok lx =
  let n = String.length lx.src in
  if lx.pos >= n then EOF
  else begin
    let c = lx.src.[lx.pos] in
    if c = ' ' || c = '\t' || c = '\r' then (lx.pos <- lx.pos + 1; next_tok lx)
    else if c = '\n' then (lx.pos <- lx.pos + 1; lx.line <- lx.line + 1; NL)
    else if c = '/' && peekc lx 1 = '/' then begin
      while lx.pos < n && lx.src.[lx.pos] <> '\n' do lx.pos <- lx.pos + 1 done;
      next_tok lx
    end else if c = '/' && peekc lx 1 = '*' then begin
      lx.pos <- lx.pos + 2;
      let saw_nl = ref false in
      while lx.pos < n && not (lx.src.[lx.pos] = '*' && peekc lx 1 = '/') do
        if lx.src.[lx.pos] = '\n' then (lx.line <- lx.line + 1; saw_nl := true);
        lx.pos <- lx.pos + 1
      done;
      if lx.pos >= n then error lx "unclosed block comment";
      lx.pos <- lx.pos + 2;
      if !saw_nl then NL else next_tok lx
    end else if is_ident_start c then begin
      let s = lx.pos in
      while lx.pos < n && is_ident lx.src.[lx.pos] do lx.pos <- lx.pos + 1 done;
      Ident (String.sub lx.src s (lx.pos - s))
    end else if is_digit c then lex_number lx
    else if c = '"' then begin
      let s = lx.pos + 1 in
      lx.pos <- s;
      while lx.pos < n && lx.src.[lx.pos] <> '"' && lx.src.[lx.pos] <> '\n' do lx.pos <- lx.pos + 1 done;
      if lx.pos >= n || lx.src.[lx.pos] <> '"' then error lx "unterminated string";
      let v = String.sub lx.src s (lx.pos - s) in
      lx.pos <- lx.pos + 1;
      StrLit v
    end else begin
      let two = if lx.pos + 1 < n then String.sub lx.src lx.pos 2 else "" in
      if two = ".." || two = "::" then (lx.pos <- lx.pos + 2; Punct two)
      else (lx.pos <- lx.pos + 1; Punct (String.make 1 c))
    end
  end

and lex_number lx =
  let n = String.length lx.src in
  let s = lx.pos in
  if lx.src.[lx.pos] = '0' && (peekc lx 1 = 'x' || peekc lx 1 = 'X' || peekc lx 1 = 'b' || peekc lx 1 = 'B') then begin
    let b = if peekc lx 1 = 'x' || peekc lx 1 = 'X' then 16 else 2 in
    lx.pos <- lx.pos + 2;
    let ds = lx.pos in
    while lx.pos < n && (is_ident lx.src.[lx.pos]) do lx.pos <- lx.pos + 1 done;
    let digits = String.concat "" (String.split_on_char '_' (String.sub lx.src ds (lx.pos - ds))) in
    (try IntLit (Big.of_string_base digits b) with _ -> error lx "bad integer literal")
  end else begin
    while lx.pos < n && is_digit lx.src.[lx.pos] do lx.pos <- lx.pos + 1 done;
    let isfloat = ref false in
    if lx.pos < n && lx.src.[lx.pos] = '.' && is_digit (peekc lx 1) then begin
      isfloat := true;
      lx.pos <- lx.pos + 1;
      while lx.pos < n && is_digit lx.src.[lx.pos] do lx.pos <- lx.pos + 1 done
    end;
    if lx.pos < n && (lx.src.[lx.pos] = 'e' || lx.src.[lx.pos] = 'E')
       && (is_digit (peekc lx 1) || ((peekc lx 1 = '+' || peekc lx 1 = '-') && is_digit (peekc lx 2))) then begin
      isfloat := true;
      lx.pos <- lx.pos + 2;
      while lx.pos < n && is_digit lx.src.[lx.pos] do lx.pos <- lx.pos + 1 done
    end;
    let txt = String.sub lx.src s (lx.pos - s) in
    if String.length txt > 1 && txt.[0] = '0' && is_digit txt.[1] then
      error lx "a decimal number never starts with 0 followed by another digit";
    if !isfloat then FloatLit txt else IntLit (Big.of_string txt)
  end

(* ---- the parser over a token list ---- *)

type p = { toks : (tok * int) array; mutable i : int }

let peek p = fst p.toks.(p.i)
let peek2 p = if p.i + 1 < Array.length p.toks then fst p.toks.(p.i + 1) else EOF
let line p = snd p.toks.(p.i)
let adv p = if p.i < Array.length p.toks - 1 then p.i <- p.i + 1
let perr p msg = raise (Error (msg, line p))

let skip_nl p = while peek p = NL do adv p done

let expect p t =
  if peek p = t then adv p
  else perr p (match t with Punct s -> "expected " ^ s | _ -> "unexpected token")

let ident p = match peek p with Ident s -> adv p; s | _ -> perr p "expected identifier"

let is_punct p s = peek p = Punct s

(* expressions: + - lowest, * / % next, unary -, primary *)
let rec expr p = additive p
and additive p =
  let l = ref (multiplicative p) in
  let continue = ref true in
  while !continue do
    match peek p with
    | Punct "+" -> adv p; skip_nl p; l := EBin ('+', !l, multiplicative p)
    | Punct "-" -> adv p; skip_nl p; l := EBin ('-', !l, multiplicative p)
    | _ -> continue := false
  done; !l
and multiplicative p =
  let l = ref (unary p) in
  let continue = ref true in
  while !continue do
    match peek p with
    | Punct ("*" | "/" | "%" as o) -> adv p; skip_nl p; l := EBin (o.[0], !l, unary p)
    | _ -> continue := false
  done; !l
and unary p =
  match peek p with
  | Punct "-" -> adv p; ENeg (unary p)
  | Punct "+" -> adv p; unary p
  | _ -> primary p
and primary p =
  match peek p with
  | IntLit v -> adv p; EInt v
  | FloatLit s -> adv p; EFloat s
  | Punct "(" -> adv p; skip_nl p; let e = expr p in skip_nl p; expect p (Punct ")"); e
  | Ident s ->
    adv p;
    if is_punct p "." then begin adv p; let m = ident p in EDot (s, m) end else EName s
  | _ -> perr p "expected expression"

(* the qualification section: runs to the end of the line *)
let attrs p =
  if not (is_punct p "|") then []
  else begin
    adv p;
    let out = ref [] in
    let rec one () =
      let k = ident p in
      let v =
        if is_punct p "=" then begin
          adv p;
          match peek p with
          | StrLit s -> adv p; Some (AStr s)
          | Ident s when peek2 p = Punct "::" ->
            adv p;
            let parts = ref [ s ] in
            while is_punct p "::" do adv p; parts := ident p :: !parts done;
            Some (AQual (List.rev !parts))
          | _ -> Some (AExpr (expr p))
        end else None in
      out := { akey = k; aval = v } :: !out;
      if is_punct p "," then (adv p; one ()) in
    one ();
    List.rev !out
  end

let int_types = [ "int8"; "int16"; "int32"; "int64"; "uint8"; "uint16"; "uint32"; "uint64" ]

let rec scalar p =
  match peek p with
  | Punct "*" -> adv p; TPtr (ident p)
  | Ident "map" when peek2 p = Punct "[" ->
    adv p; expect p (Punct "[");
    let k = scalar p in
    expect p (Punct "]");
    let v = ftype p in
    TMap (k, v)
  | Ident s ->
    adv p;
    let paren1 () = expect p (Punct "("); let e = expr p in expect p (Punct ")"); e in
    let paren2 () =
      expect p (Punct "("); let a = expr p in expect p (Punct ","); skip_nl p;
      let b = expr p in expect p (Punct ")"); (a, b) in
    (match s with
     | _ when List.mem s int_types -> TInt s
     | "int128" -> TInt128
     | "uint128" -> TUInt128
     | "bits" -> TBits (paren1 ())
     | "bool" -> TBool
     | "float32" -> TF32
     | "float64" -> TF64
     | "string" -> if is_punct p "(" then TString (Some (paren1 ())) else TString None
     | "bytes" -> TBytes (paren1 ())
     | "timestamp" -> TTimestamp
     | "fixed" -> let a, b = paren2 () in TFixed (a, b)
     | "ufixed" -> let a, b = paren2 () in TUFixed (a, b)
     | _ -> TNamed s)
  | _ -> perr p "expected a type"

and ftype p =
  let opt = if is_punct p "?" then (adv p; true) else false in
  let arr =
    if is_punct p "[" then begin
      adv p;
      let b =
        if is_punct p "]" then BOpen
        else if is_punct p ".." then (adv p; BUpto (expr p))
        else begin
          let a = expr p in
          if is_punct p ".." then (adv p; BRange (a, expr p)) else BExact a
        end in
      expect p (Punct "]");
      Some b
    end else None in
  (* `?` may also follow the bound: [..N]?string(M), §2.13 *)
  let opt2 = if arr <> None && is_punct p "?" then (adv p; true) else false in
  let base = scalar p in
  { topt = opt; tarr = arr; telemopt = opt2; tbase = base }

let end_item p =
  match peek p with
  | NL -> skip_nl p
  | Punct "}" | EOF -> ()
  | _ -> perr p "expected end of line"

let default p =
  match peek p with
  | StrLit s -> adv p; DStr s
  | Punct "{" ->
    adv p; skip_nl p;
    let names = ref [] in
    while not (is_punct p "}") do
      names := ident p :: !names;
      skip_nl p;
      if is_punct p "," then (adv p; skip_nl p)
    done;
    adv p;
    DList (List.rev !names)
  | _ -> DExpr (expr p)

let rec block p =
  skip_nl p;
  expect p (Punct "{");
  skip_nl p;
  let items = ref [] in
  while not (is_punct p "}") do
    items := item p :: !items;
    skip_nl p
  done;
  adv p;
  List.rev !items

and item p =
  let ln = line p in
  match peek p with
  | Ident "const" when peek2 p = Punct "(" ->
    adv p; adv p; let a = expr p in expect p (Punct ","); let b = expr p in expect p (Punct ")");
    end_item p; IConst (a, b)
  | Ident "reserved" when peek2 p = Punct "(" ->
    adv p; adv p; let a = expr p in expect p (Punct ")"); end_item p; IReserved a
  | Ident "align" when (match peek2 p with NL | Punct "}" -> true | _ -> false) -> adv p; end_item p; IAlign
  | Ident "if" ->
    adv p;
    let neg = if is_punct p "!" then (adv p; true) else false in
    let g = ident p in
    let th = block p in
    let save = p.i in
    skip_nl p;
    let el = if peek p = Ident "else" then (adv p; block p) else (p.i <- save; []) in
    end_item p;
    IIf (neg, g, th, el)
  | Ident _ ->
    let name = ident p in
    let t = ftype p in
    let d = if is_punct p "=" then (adv p; Some (default p)) else None in
    let a = attrs p in
    end_item p;
    IField { fname = name; ftype = t; fdefault = d; fattrs = a; fline = ln }
  | _ -> perr p "expected a field"

let variant_list p =
  skip_nl p;
  expect p (Punct "{");
  skip_nl p;
  let vs = ref [] in
  while not (is_punct p "}") do
    let name = match peek p with Ident s -> adv p; s | _ -> perr p "expected variant" in
    let a = attrs p in
    vs := { vname = name; vattrs = a } :: !vs;
    skip_nl p;
    if is_punct p "," then (adv p; skip_nl p)
  done;
  adv p;
  List.rev !vs

let union_block p =
  skip_nl p;
  expect p (Punct "{");
  skip_nl p;
  let arms = ref [] in
  while not (is_punct p "}") do
    let name = ident p in
    let t =
      match peek p with
      | NL | Punct "|" | Punct "}" -> None
      | _ -> Some (ftype p) in
    let a = attrs p in
    end_item p;
    arms := { arm_name = name; arm_type = t; arm_attrs = a } :: !arms;
    skip_nl p
  done;
  adv p;
  List.rev !arms

let decl p =
  match peek p with
  | Ident "package" -> adv p; let n = ident p in end_item p; DPackage n
  | Ident "const" ->
    adv p;
    let n = ident p in
    let ty = match peek p with Ident s -> adv p; Some s | _ -> None in
    expect p (Punct "=");
    let e = expr p in
    let a = attrs p in
    end_item p;
    DConst (n, ty, e, a)
  | Ident "enum" -> adv p; let n = ident p in let a = attrs p in let vs = variant_list p in end_item p; DEnum (n, a, vs)
  | Ident "flags" -> adv p; let n = ident p in let a = attrs p in let vs = variant_list p in end_item p; DFlags (n, a, vs)
  | Ident "type" -> adv p; let n = ident p in let a = attrs p in let b = block p in end_item p; DType (n, a, b)
  | Ident "table" -> adv p; let n = ident p in let a = attrs p in let b = block p in end_item p; DTable (n, false, a, b)
  | Ident "fixed" when peek2 p = Ident "table" ->
    (* SPEC-GAP 2: §4.2's grammar has no `fixed table`; SPEC-TABLES §2.2 declares it *)
    adv p; adv p; let n = ident p in let a = attrs p in let b = block p in end_item p; DTable (n, true, a, b)
  | Ident "union" -> adv p; let n = ident p in let a = attrs p in let arms = union_block p in end_item p; DUnion (n, a, arms)
  | _ -> perr p "expected a declaration"

let parse_file src =
  let lx = { src; pos = 0; line = 1 } in
  let toks = ref [] in
  let fin = ref false in
  while not !fin do
    let ln = lx.line in
    let t = next_tok lx in
    toks := (t, ln) :: !toks;
    if t = EOF then fin := true
  done;
  let p = { toks = Array.of_list (List.rev !toks); i = 0 } in
  let decls = ref [] in
  skip_nl p;
  while peek p <> EOF do
    decls := decl p :: !decls;
    skip_nl p
  done;
  List.rev !decls
