-- | Exact float arithmetic: correctly rounded decimal-to-binary conversion at
-- a field's own width, and C's `%.*g` spelled out over the exact binary value
-- (docs/SPEC-TABLES.md §16.14 W12, §16.2 Numbers).
module FloatBits
  ( ratToF32
  , ratToF64
  , f32ToRat
  , f64ToRat
  , f32IsFinite
  , f64IsFinite
  , f32IsNegZero
  , f64IsNegZero
  , fmtFloat32
  , fmtFloat64
  , float32FromRational
  , float64FromRational
  , f32Bits
  , f64Bits
  , ilog2
  ) where

import Data.Bits (shiftL, shiftR, testBit, (.&.), (.|.))
import Data.Ratio (denominator, numerator, (%))
import Data.Word (Word32, Word64)

-- | floor(log2 n) for n > 0
ilog2 :: Integer -> Int
ilog2 n = go 0 n
  where
    go acc m
      | m >= 2 ^ (64 :: Int) = go (acc + 64) (m `shiftR` 64)
      | m > 1 = go (acc + 1) (m `shiftR` 1)
      | otherwise = acc

-- | floor(log2 r) for r > 0
floorLog2 :: Rational -> Int
floorLog2 r =
  let l = ilog2 (numerator r) - ilog2 (denominator r)
   in if r < pow2 l then l - 1 else l

pow2 :: Int -> Rational
pow2 e = if e >= 0 then fromInteger (2 ^ e) else 1 % (2 ^ negate e)

roundHalfEven :: Rational -> Integer
roundHalfEven r =
  let n = floor r
      f = r - fromInteger n
   in case compare f (1 % 2) of
        LT -> n
        GT -> n + 1
        EQ -> if even n then n else n + 1

-- | Round a positive rational to a binary format; Nothing on overflow.
-- Returns the magnitude's bit pattern (exponent and fraction fields).
roundBin :: Int -> Int -> Int -> Rational -> Maybe Integer
roundBin p emin emax r
  | r == 0 = Just 0
  | otherwise =
      let e0 = floorLog2 r - (p - 1)
          e1 = max e0 (emin - (p - 1))
          m0 = roundHalfEven (r / pow2 e1)
          (m, e) = if m0 == 2 ^ p then (2 ^ (p - 1), e1 + 1) else (m0, e1)
          bias = emax
       in if e + (p - 1) > emax
            then Nothing
            else
              if m < 2 ^ (p - 1)
                then Just m
                else Just ((fromIntegral (e + (p - 1) + bias) `shiftL` (p - 1)) .|. (m - 2 ^ (p - 1)))

ratToF32 :: Rational -> Maybe Word32
ratToF32 r
  | r < 0 = (.|. 0x80000000) . fromInteger <$> roundBin 24 (-126) 127 (negate r)
  | otherwise = fromInteger <$> roundBin 24 (-126) 127 r

ratToF64 :: Rational -> Maybe Word64
ratToF64 r
  | r < 0 = (.|. 0x8000000000000000) . fromInteger <$> roundBin 53 (-1022) 1023 (negate r)
  | otherwise = fromInteger <$> roundBin 53 (-1022) 1023 r

-- | Defaults in a schema are finite by construction; an overflow is a checker
-- error the corpus does not reach, so it saturates here.
float32FromRational :: Rational -> Word32
float32FromRational r = maybe 0x7F7FFFFF id (ratToF32 r)

float64FromRational :: Rational -> Word64
float64FromRational r = maybe 0x7FEFFFFFFFFFFFFF id (ratToF64 r)

f32Bits :: Word32 -> Word32
f32Bits = id

f64Bits :: Word64 -> Word64
f64Bits = id

binToRat :: Int -> Int -> Integer -> Bool -> Rational
binToRat p bias bits neg =
  let ef = bits `shiftR` (p - 1)
      fr = bits .&. (2 ^ (p - 1) - 1)
      mag =
        if ef == 0
          then fromInteger fr * pow2 (1 - bias - (p - 1))
          else fromInteger (fr + 2 ^ (p - 1)) * pow2 (fromInteger ef - bias - (p - 1))
   in if neg then negate mag else mag

f32ToRat :: Word32 -> Rational
f32ToRat w = binToRat 24 127 (toInteger (w .&. 0x7FFFFFFF)) (testBit w 31)

f64ToRat :: Word64 -> Rational
f64ToRat w = binToRat 53 1023 (toInteger (w .&. 0x7FFFFFFFFFFFFFFF)) (testBit w 63)

f32IsFinite :: Word32 -> Bool
f32IsFinite w = (w `shiftR` 23) .&. 0xFF /= 0xFF

f64IsFinite :: Word64 -> Bool
f64IsFinite w = (w `shiftR` 52) .&. 0x7FF /= 0x7FF

f32IsNegZero :: Word32 -> Bool
f32IsNegZero = (== 0x80000000)

f64IsNegZero :: Word64 -> Bool
f64IsNegZero = (== 0x8000000000000000)

-- | floor(log10 a) for a > 0
floorLog10 :: Rational -> Int
floorLog10 a =
  let est = floor (fromIntegral (ilog2 (numerator a) - ilog2 (denominator a)) * (0.30102999566398 :: Double)) :: Int
      pw k = if k >= 0 then fromInteger (10 ^ k) else 1 % (10 ^ negate k) :: Rational
      fixup k
        | pw k > a = fixup (k - 1)
        | pw (k + 1) <= a = fixup (k + 1)
        | otherwise = k
   in fixup est

-- | C's `%.*g` at precision p over an exact value.
fmtG :: Int -> Bool -> Rational -> String
fmtG p neg a = sign ++ body
  where
    sign = if neg then "-" else ""
    pw k = if k >= 0 then fromInteger (10 ^ k) else 1 % (10 ^ negate k) :: Rational
    (digits, x)
      | a == 0 = (replicate p '0', 0)
      | otherwise =
          let x0 = floorLog10 a
              n0 = roundHalfEven (a / pw (x0 - p + 1))
           in if n0 >= 10 ^ p
                then (show (roundHalfEven (a / pw (x0 + 1 - p + 1))), x0 + 1)
                else (show n0, x0)
    ds = let t = reverse (dropWhile (== '0') (reverse digits)) in if null t then "0" else t
    body
      | x < -4 || x >= p =
          let (d1, rest) = splitAt 1 ds
              e = abs x
              es = (if x < 0 then "-" else "+") ++ (if e < 10 then '0' : show e else show e)
           in d1 ++ (if null rest then "" else '.' : rest) ++ "e" ++ es
      | x >= 0 =
          let ip = take (x + 1) (ds ++ replicate (x + 1) '0')
              fp = drop (x + 1) ds
           in if null fp then ip else ip ++ "." ++ fp
      | otherwise = "0." ++ replicate (negate x - 1) '0' ++ ds

-- | W12 for a 32-bit field. The caller has refused a non-finite value.
fmtFloat32 :: Word32 -> String
fmtFloat32 w = go 6
  where
    v = f32ToRat w
    neg = testBit w 31
    go prec =
      let t = fmtG prec neg (abs v)
       in if prec == 9 || roundTrips t then t else go (prec + 1)
    roundTrips t = fmap (.&. 0x7FFFFFFF) (ratToF32 (decRat t)) == Just (w .&. 0x7FFFFFFF)

fmtFloat64 :: Word64 -> String
fmtFloat64 w = go 15
  where
    v = f64ToRat w
    neg = testBit w 63
    go prec =
      let t = fmtG prec neg (abs v)
       in if prec == 17 || roundTrips t then t else go (prec + 1)
    roundTrips t = fmap (.&. 0x7FFFFFFFFFFFFFFF) (ratToF64 (decRat t)) == Just (w .&. 0x7FFFFFFFFFFFFFFF)

-- | The magnitude of a `%g` spelling.
decRat :: String -> Rational
decRat s0 =
  let s = dropWhile (== '-') s0
      (mant, ex) = break (== 'e') s
      (ip, fp) = break (== '.') mant
      fd = drop 1 fp
      digits = read (ip ++ fd) :: Integer
      e = (case ex of
             ('e' : '+' : r) -> read r
             ('e' : r) -> read r
             _ -> 0) - length fd
   in if e >= 0 then fromInteger (digits * 10 ^ e) else digits % (10 ^ negate e)
