(* Number conversions the text form needs exactly (SPEC-TABLES §16.2 Numbers,
   §16.14 W10-W12).  OCaml's float is binary64 only, so the float32 side is
   spelled out: a decimal is rounded to float32 ONCE, exactly, over integers. *)

(* a decimal token -> (negative, digits as Big, decimal exponent) with value
   (-1)^neg * digits * 10^exp.  The token is RFC 8259's grammar, already checked. *)
let decompose (tok : string) : bool * Big.t * int =
  let n = String.length tok in
  let i = ref 0 in
  let neg = n > 0 && tok.[0] = '-' in
  if neg then incr i;
  let digits = Buffer.create 32 in
  let frac = ref 0 in
  while !i < n && tok.[!i] >= '0' && tok.[!i] <= '9' do Buffer.add_char digits tok.[!i]; incr i done;
  if !i < n && tok.[!i] = '.' then begin
    incr i;
    while !i < n && tok.[!i] >= '0' && tok.[!i] <= '9' do Buffer.add_char digits tok.[!i]; incr frac; incr i done
  end;
  let exp = ref 0 in
  if !i < n && (tok.[!i] = 'e' || tok.[!i] = 'E') then begin
    incr i;
    let eneg = !i < n && tok.[!i] = '-' in
    if !i < n && (tok.[!i] = '-' || tok.[!i] = '+') then incr i;
    (* an exponent may be arbitrarily long; saturate, the value is then 0 or inf *)
    let e = ref 0 in
    while !i < n && tok.[!i] >= '0' && tok.[!i] <= '9' do
      if !e < 100_000_000 then e := !e * 10 + (Char.code tok.[!i] - 48);
      incr i
    done;
    exp := if eneg then - !e else !e
  end;
  (* strip leading zeros of the digit string *)
  let ds = Buffer.contents digits in
  let k = ref 0 in
  while !k < String.length ds - 1 && ds.[!k] = '0' do incr k done;
  let ds = String.sub ds !k (String.length ds - !k) in
  (* strip trailing zeros into the exponent (keeps the integers small) *)
  let m = ref (String.length ds) in
  let e = ref (!exp - !frac) in
  while !m > 1 && ds.[!m - 1] = '0' do decr m; incr e done;
  let ds = String.sub ds 0 !m in
  (neg, Big.of_string ds, !e)

(* round num/den (both positive) to a binary float with [mbits] mantissa bits,
   minimum exponent [emin] (of the unit in the last place), max finite exponent;
   returns None on overflow to infinity, else the value as an OCaml float *)
let round_ratio ~mbits ~emin ~emax_bits num den : float option =
  if Big.is_zero num then Some 0.0
  else begin
    let k = Big.num_bits num - Big.num_bits den in
    let e = ref (max (k - mbits) emin) in
    let scaled e =
      if e >= 0 then (num, Big.shift_left den e) else (Big.shift_left num (- e), den) in
    let qr e = let a, b = scaled e in let q, r = Big.quo_rem a b in (q, r, b) in
    let q, r, d = ref (Big.zero), ref Big.zero, ref Big.one in
    let set e = let a, b, c = qr e in q := a; r := b; d := c in
    set !e;
    let top = Big.pow2 mbits and half = Big.pow2 (mbits - 1) in
    while Big.compare !q top >= 0 do incr e; set !e done;
    while Big.compare !q half < 0 && !e > emin do decr e; set !e done;
    (* round half to even *)
    let twice = Big.shift_left !r 1 in
    let c = Big.compare twice !d in
    let qv = ref !q in
    let odd = (not (Big.is_zero !qv)) && (Big.to_int (Big.rem !qv (Big.of_int 2)) = 1) in
    if c > 0 || (c = 0 && odd) then qv := Big.succ !qv;
    if Big.compare !qv top >= 0 then (qv := Big.shift_right_mag !qv 1; incr e);
    (* qv < 2^mbits and value = qv * 2^e *)
    if !e + mbits > emax_bits then None
    else Some (Float.ldexp (float_of_string (Big.to_string !qv)) !e)
  end

let ndig d = String.length (Big.to_string d)

let ratio_of_decimal (digits : Big.t) (exp : int) =
  if exp >= 0 then (Big.mul digits (Big.pow10 exp), Big.one)
  else (digits, Big.pow10 (- exp))

(* the float32 nearest a decimal token, ties to even; None = rounds to infinity *)
let f32_of_token (tok : string) : float option =
  let neg, d, e = decompose tok in
  let r =
    if Big.is_zero d then Some 0.0
    else if e + ndig d > 40 then None
    else if e + ndig d < -47 then Some 0.0
    else begin
      let num, den = ratio_of_decimal d e in
      (* float32: 24-bit significand, subnormal ulp 2^-149, max < 2^128 *)
      round_ratio ~mbits:24 ~emin:(-149) ~emax_bits:128 num den
    end in
  match r with
  | None -> None
  | Some v -> Some (if neg then -. v else v)

let f64_of_token (tok : string) : float option =
  let neg, d, e = decompose tok in
  let r =
    if Big.is_zero d then Some 0.0
    else if e + ndig d > 310 then None
    else if e + ndig d < -326 then Some 0.0
    else begin
      let num, den = ratio_of_decimal d e in
      round_ratio ~mbits:53 ~emin:(-1074) ~emax_bits:1024 num den
    end in
  match r with
  | None -> None
  | Some v -> Some (if neg then -. v else v)

(* W12: the float spelling *)
let g p v = Printf.sprintf "%.*g" p v

let float_text ~is32 (v : float) : string =
  let lo, hi = if is32 then (6, 9) else (15, 17) in
  let rec loop p =
    let t = g p v in
    if p = hi then t
    else begin
      let back = if is32 then f32_of_token t else f64_of_token t in
      match back with
      | Some b when Int64.bits_of_float b = Int64.bits_of_float v -> t
      | _ -> loop (p + 1)
    end in
  let t = loop lo in
  (* %g prints "inf"/"nan" never here: the caller refused those *)
  t

(* W11: a fixed raw value r / 2^F, written exactly *)
let fixed_text (r : Big.t) (f : int) : string =
  let neg = Big.compare r Big.zero < 0 in
  let a = Big.abs r in
  let whole = Big.shift_right_mag a f in
  let frac = Big.sub a (Big.shift_left whole f) in
  let buf = Buffer.create 32 in
  if neg then Buffer.add_char buf '-';
  Buffer.add_string buf (Big.to_string whole);
  Buffer.add_char buf '.';
  if Big.is_zero frac then Buffer.add_char buf '0'
  else begin
    let cur = ref frac in
    let den = Big.pow2 f in
    while not (Big.is_zero !cur) do
      let t = Big.mul !cur (Big.of_int 10) in
      let q, r = Big.quo_rem t den in
      Buffer.add_string buf (Big.to_string q);
      cur := r
    done
  end;
  Buffer.contents buf

(* a fixed field's token in whole units -> raw, only if exact *)
let fixed_raw_of_token (tok : string) (f : int) : Big.t option =
  let neg, d, e = decompose tok in
  if Big.is_zero d then Some Big.zero
  else if e + ndig d > 45 then None            (* past any 128-bit storage *)
  else if e < 0 && - e > 2 * ndig d + 2 then None   (* 5^-e cannot divide d: not exact *)
  else
  let num, den = ratio_of_decimal d e in
  let num = Big.shift_left num f in
  let q, r = Big.quo_rem num den in
  if not (Big.is_zero r) then None
  else Some (if neg then Big.neg q else q)

(* RFC 8259 number grammar *)
let is_number_token (s : string) : bool =
  let n = String.length s in
  let i = ref 0 in
  let digit () = !i < n && s.[!i] >= '0' && s.[!i] <= '9' in
  if !i < n && s.[!i] = '-' then incr i;
  if not (digit ()) then false
  else begin
    if s.[!i] = '0' then incr i else while digit () do incr i done;
    let ok = ref true in
    if !i < n && s.[!i] = '.' then begin
      incr i;
      if not (digit ()) then ok := false;
      while digit () do incr i done
    end;
    if !ok && !i < n && (s.[!i] = 'e' || s.[!i] = 'E') then begin
      incr i;
      if !i < n && (s.[!i] = '+' || s.[!i] = '-') then incr i;
      if not (digit ()) then ok := false;
      while digit () do incr i done
    end;
    !ok && !i = n
  end

(* integer token: digits, optional minus, no fraction/exponent, not -0 *)
let is_int_token (s : string) : bool =
  let n = String.length s in
  if n = 0 then false
  else begin
    let st = if s.[0] = '-' then 1 else 0 in
    if st >= n then false
    else if s = "-0" then false
    else if s.[st] = '0' && n - st > 1 then false
    else begin
      let ok = ref true in
      for k = st to n - 1 do if s.[k] < '0' || s.[k] > '9' then ok := false done;
      !ok
    end
  end

let is_float_neg_zero (v : float) = Int64.bits_of_float v = Int64.min_int

(* ---- base64, §16.8: standard alphabet, padded, canonical ---- *)

let b64chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

let b64_encode (s : string) : string =
  let n = String.length s in
  let buf = Buffer.create ((n + 2) / 3 * 4) in
  let i = ref 0 in
  while !i < n do
    let b0 = Char.code s.[!i] in
    let b1 = if !i + 1 < n then Char.code s.[!i + 1] else 0 in
    let b2 = if !i + 2 < n then Char.code s.[!i + 2] else 0 in
    Buffer.add_char buf b64chars.[b0 lsr 2];
    Buffer.add_char buf b64chars.[((b0 land 3) lsl 4) lor (b1 lsr 4)];
    Buffer.add_char buf (if !i + 1 < n then b64chars.[((b1 land 15) lsl 2) lor (b2 lsr 6)] else '=');
    Buffer.add_char buf (if !i + 2 < n then b64chars.[b2 land 63] else '=');
    i := !i + 3
  done;
  Buffer.contents buf

(* `Ok bytes | `Noncanonical | `Damaged *)
let b64_decode (s : string) =
  let idx c = String.index_opt b64chars c in
  let n = String.length s in
  let all_ok = ref true in
  String.iter (fun c -> if c <> '=' && idx c = None then all_ok := false) s;
  if not !all_ok then `Damaged
  else if n mod 4 <> 0 then
    (* unpadded or truncated: decodes leniently? treat as not canonical if it would decode *)
    (if n mod 4 = 1 || String.contains s '=' then `Damaged else `Noncanonical)
  else begin
    let out = Buffer.create (n / 4 * 3) in
    let bad = ref false and noncanon = ref false in
    let groups = n / 4 in
    for g = 0 to groups - 1 do
      let c k = s.[g * 4 + k] in
      let last = g = groups - 1 in
      let pads = (if c 3 = '=' then 1 else 0) + (if c 2 = '=' then 1 else 0) in
      if (c 0 = '=' || c 1 = '=') || (c 2 = '=' && c 3 <> '=') then bad := true
      else if pads > 0 && not last then bad := true
      else begin
        let v k = match idx (c k) with Some x -> x | None -> 0 in
        let x = (v 0 lsl 18) lor (v 1 lsl 12) lor (v 2 lsl 6) lor v 3 in
        Buffer.add_char out (Char.chr ((x lsr 16) land 255));
        if pads < 2 then Buffer.add_char out (Char.chr ((x lsr 8) land 255));
        if pads < 1 then Buffer.add_char out (Char.chr (x land 255));
        if pads = 2 && (v 1) land 15 <> 0 then noncanon := true;
        if pads = 1 && (v 2) land 3 <> 0 then noncanon := true
      end
    done;
    if !bad then `Damaged else if !noncanon then `Noncanonical else `Ok (Buffer.contents out)
  end

(* ---- UTF-8 ---- *)

let utf8_valid (s : string) : bool =
  let n = String.length s in
  let i = ref 0 and ok = ref true in
  while !ok && !i < n do
    let c = Char.code s.[!i] in
    let cont k = !i + k < n && Char.code s.[!i + k] land 0xC0 = 0x80 in
    let b k = Char.code s.[!i + k] in
    if c < 0x80 then incr i
    else if c >= 0xC2 && c <= 0xDF then (if cont 1 then i := !i + 2 else ok := false)
    else if c >= 0xE0 && c <= 0xEF then begin
      if cont 1 && cont 2 then begin
        let b1 = b 1 in
        if (c = 0xE0 && b1 < 0xA0) || (c = 0xED && b1 > 0x9F) then ok := false else i := !i + 3
      end else ok := false
    end else if c >= 0xF0 && c <= 0xF4 then begin
      if cont 1 && cont 2 && cont 3 then begin
        let b1 = b 1 in
        if (c = 0xF0 && b1 < 0x90) || (c = 0xF4 && b1 > 0x8F) then ok := false else i := !i + 4
      end else ok := false
    end else ok := false
  done;
  !ok

let is_text (s : string) = utf8_valid s && not (String.contains s '\000')
