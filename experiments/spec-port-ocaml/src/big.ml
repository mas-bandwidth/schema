(* Arbitrary-precision integers, enough for SPEC.md §4.2's constant folding
   (arbitrary precision, fit-checked at each use) and for the 128-bit family.
   OCaml's native int is 63 bits, so nothing wider than that goes through it.
   Representation: sign (-1, 0, 1) and a little-endian magnitude in base 2^24. *)

let base_bits = 24
let base = 1 lsl base_bits
let mask = base - 1

type t = { sign : int; mag : int array } (* mag has no trailing zero limbs *)

let zero = { sign = 0; mag = [||] }

let norm_mag m =
  let n = ref (Array.length m) in
  while !n > 0 && m.(!n - 1) = 0 do decr n done;
  if !n = Array.length m then m else Array.sub m 0 !n

let make sign mag =
  let mag = norm_mag mag in
  if Array.length mag = 0 then zero else { sign; mag }

let of_int i =
  if i = 0 then zero
  else begin
    (* careful with min_int: work with negative accumulation *)
    let neg = i < 0 in
    let limbs = ref [] in
    let x = ref i in
    while !x <> 0 do
      let r = if neg then - (!x mod base) else !x mod base in
      limbs := r :: !limbs;
      x := !x / base
    done;
    make (if neg then -1 else 1) (Array.of_list (List.rev !limbs))
  end

let cmp_mag a b =
  let la = Array.length a and lb = Array.length b in
  if la <> lb then compare la lb
  else begin
    let r = ref 0 and i = ref (la - 1) in
    while !r = 0 && !i >= 0 do
      r := compare a.(!i) b.(!i);
      decr i
    done;
    !r
  end

let compare a b =
  if a.sign <> b.sign then compare a.sign b.sign
  else if a.sign >= 0 then cmp_mag a.mag b.mag
  else cmp_mag b.mag a.mag

let equal a b = compare a b = 0
let is_zero a = a.sign = 0
let neg a = { a with sign = - a.sign }
let abs a = if a.sign < 0 then neg a else a

let add_mag a b =
  let la = Array.length a and lb = Array.length b in
  let n = max la lb + 1 in
  let r = Array.make n 0 in
  let carry = ref 0 in
  for i = 0 to n - 1 do
    let s = (if i < la then a.(i) else 0) + (if i < lb then b.(i) else 0) + !carry in
    r.(i) <- s land mask;
    carry := s lsr base_bits
  done;
  r

(* requires a >= b *)
let sub_mag a b =
  let la = Array.length a and lb = Array.length b in
  let r = Array.make la 0 in
  let borrow = ref 0 in
  for i = 0 to la - 1 do
    let s = a.(i) - (if i < lb then b.(i) else 0) - !borrow in
    if s < 0 then (r.(i) <- s + base; borrow := 1) else (r.(i) <- s; borrow := 0)
  done;
  r

let add a b =
  if a.sign = 0 then b
  else if b.sign = 0 then a
  else if a.sign = b.sign then make a.sign (add_mag a.mag b.mag)
  else begin
    let c = cmp_mag a.mag b.mag in
    if c = 0 then zero
    else if c > 0 then make a.sign (sub_mag a.mag b.mag)
    else make b.sign (sub_mag b.mag a.mag)
  end

let sub a b = add a (neg b)

let mul a b =
  if a.sign = 0 || b.sign = 0 then zero
  else begin
    let la = Array.length a.mag and lb = Array.length b.mag in
    let r = Array.make (la + lb + 1) 0 in
    for i = 0 to la - 1 do
      let carry = ref 0 in
      for j = 0 to lb - 1 do
        let s = r.(i + j) + a.mag.(i) * b.mag.(j) + !carry in
        r.(i + j) <- s land mask;
        carry := s lsr base_bits
      done;
      let k = ref (i + lb) in
      while !carry <> 0 do
        let s = r.(!k) + !carry in
        r.(!k) <- s land mask;
        carry := s lsr base_bits;
        incr k
      done
    done;
    make (a.sign * b.sign) r
  end

let num_bits_mag m =
  let n = Array.length m in
  if n = 0 then 0
  else begin
    let top = m.(n - 1) in
    let b = ref 0 in
    while top lsr !b <> 0 do incr b done;
    (n - 1) * base_bits + !b
  end

let num_bits a = num_bits_mag a.mag

let test_bit_mag m i =
  let l = i / base_bits in
  if l >= Array.length m then false else (m.(l) lsr (i mod base_bits)) land 1 = 1

let shift_left a k =
  if a.sign = 0 || k = 0 then a
  else begin
    let limbs = k / base_bits and bits = k mod base_bits in
    let la = Array.length a.mag in
    let r = Array.make (la + limbs + 1) 0 in
    for i = 0 to la - 1 do
      let v = a.mag.(i) lsl bits in
      r.(i + limbs) <- r.(i + limbs) lor (v land mask);
      r.(i + limbs + 1) <- r.(i + limbs + 1) lor (v lsr base_bits)
    done;
    make a.sign r
  end

(* magnitude shift right (truncates magnitude toward zero) *)
let shift_right_mag a k =
  if a.sign = 0 || k = 0 then a
  else begin
    let limbs = k / base_bits and bits = k mod base_bits in
    let la = Array.length a.mag in
    if limbs >= la then zero
    else begin
      let n = la - limbs in
      let r = Array.make n 0 in
      for i = 0 to n - 1 do
        let lo = a.mag.(i + limbs) lsr bits in
        let hi = if i + limbs + 1 < la then (a.mag.(i + limbs + 1) lsl (base_bits - bits)) land mask else 0 in
        r.(i) <- if bits = 0 then a.mag.(i + limbs) else lo lor hi
      done;
      make a.sign r
    end
  end

(* truncating division toward zero, Go's rule; remainder takes the dividend's sign *)
let quo_rem a b =
  if b.sign = 0 then raise Division_by_zero;
  if cmp_mag a.mag b.mag < 0 then (zero, a)
  else begin
    let am = { sign = 1; mag = a.mag } and bm = { sign = 1; mag = b.mag } in
    let nb = num_bits am in
    let q = ref zero and r = ref zero in
    for i = nb - 1 downto 0 do
      r := shift_left !r 1;
      if test_bit_mag am.mag i then r := add !r (of_int 1);
      if compare !r bm >= 0 then begin
        r := sub !r bm;
        q := add !q (shift_left (of_int 1) i)
      end
    done;
    let q = if a.sign * b.sign < 0 then neg !q else !q in
    let r = if a.sign < 0 then neg !r else !r in
    (q, r)
  end

let div a b = fst (quo_rem a b)
let rem a b = snd (quo_rem a b)

let pow2 k = shift_left (of_int 1) k

let rec pow10 k = if k = 0 then of_int 1 else mul (of_int 10) (pow10 (k - 1))

let to_string a =
  if a.sign = 0 then "0"
  else begin
    let digits = Buffer.create 64 in
    let cur = ref (abs a) in
    let parts = ref [] in
    let ten4 = of_int 10000 in
    while not (is_zero !cur) do
      let q, r = quo_rem !cur ten4 in
      let rv = if is_zero r then 0 else r.mag.(0) in
      parts := rv :: !parts;
      cur := q
    done;
    if a.sign < 0 then Buffer.add_char digits '-';
    (match !parts with
     | [] -> Buffer.add_char digits '0'
     | first :: rest ->
       Buffer.add_string digits (string_of_int first);
       List.iter (fun p -> Buffer.add_string digits (Printf.sprintf "%04d" p)) rest);
    Buffer.contents digits
  end

(* decimal digits only, optional leading '-' *)
let of_string s =
  let n = String.length s in
  let neg, start = if n > 0 && s.[0] = '-' then (true, 1) else (false, 0) in
  let acc = ref zero in
  let ten = of_int 10 in
  for i = start to n - 1 do
    let c = s.[i] in
    if c < '0' || c > '9' then invalid_arg "Big.of_string";
    acc := add (mul !acc ten) (of_int (Char.code c - 48))
  done;
  if neg then { !acc with sign = - (!acc).sign } else !acc

let of_string_base s b =
  let acc = ref zero in
  let bb = of_int b in
  String.iter (fun c ->
      let d = match c with
        | '0' .. '9' -> Char.code c - 48
        | 'a' .. 'f' -> Char.code c - 87
        | 'A' .. 'F' -> Char.code c - 55
        | _ -> invalid_arg "Big.of_string_base" in
      if d >= b then invalid_arg "Big.of_string_base";
      acc := add (mul !acc bb) (of_int d)) s;
  !acc

let fits_int a = num_bits a <= 61

let to_int a =
  if not (fits_int a) then failwith "Big.to_int";
  let r = ref 0 in
  for i = Array.length a.mag - 1 downto 0 do r := (!r lsl base_bits) lor a.mag.(i) done;
  if a.sign < 0 then - !r else !r

(* the low [bytes] bytes of the two's complement image, little-endian *)
let to_le_bytes a nbytes =
  let b = Bytes.make nbytes '\000' in
  let v = if a.sign >= 0 then a else add (pow2 (nbytes * 8)) a in
  for i = 0 to nbytes - 1 do
    let bitpos = i * 8 in
    let limb = bitpos / base_bits and off = bitpos mod base_bits in
    let lo = if limb < Array.length v.mag then v.mag.(limb) lsr off else 0 in
    let hi = if off + 8 > base_bits && limb + 1 < Array.length v.mag then v.mag.(limb + 1) lsl (base_bits - off) else 0 in
    Bytes.set b i (Char.chr ((lo lor hi) land 0xff))
  done;
  Bytes.to_string b

let of_le_bytes ?(signed = false) s off nbytes =
  let acc = ref zero in
  for i = nbytes - 1 downto 0 do
    acc := add (shift_left !acc 8) (of_int (Char.code s.[off + i]))
  done;
  if signed && nbytes > 0 && Char.code s.[off + nbytes - 1] land 0x80 <> 0 then
    sub !acc (pow2 (nbytes * 8))
  else !acc

let of_int64 (x : int64) = of_le_bytes ~signed:true (let b = Bytes.create 8 in Bytes.set_int64_le b 0 x; Bytes.to_string b) 0 8
let of_uint64 (x : int64) = of_le_bytes ~signed:false (let b = Bytes.create 8 in Bytes.set_int64_le b 0 x; Bytes.to_string b) 0 8
let to_int64_bits a = Bytes.get_int64_le (Bytes.of_string (to_le_bytes a 8)) 0

(* minimal big-endian magnitude bytes *)
let mag_be_bytes a =
  let nb = (num_bits a + 7) / 8 in
  let le = to_le_bytes (abs a) nb in
  String.init nb (fun i -> le.[nb - 1 - i])

let one = of_int 1
let succ a = add a one
let pred a = sub a one
let min a b = if compare a b <= 0 then a else b
let max a b = if compare a b >= 0 then a else b
