-- | Text as the tables layer means it: well-formed UTF-8 holding no zero
-- byte (docs/SPEC-TABLES.md §2.13, §3, §16.3).
module Utf8 (validText, wellFormed, encodeChar) where

import Data.Bits (shiftR, (.&.), (.|.))
import qualified Data.ByteString as B
import Data.Word (Word8)

validText :: B.ByteString -> Bool
validText bs = B.notElem 0 bs && wellFormed bs

-- | RFC 3629 well-formedness: no overlong form, no surrogate, nothing past
-- U+10FFFF, no truncated sequence.
wellFormed :: B.ByteString -> Bool
wellFormed = go . B.unpack
  where
    go [] = True
    go (b : r)
      | b < 0x80 = go r
      | b >= 0xC2 && b <= 0xDF = cont 1 r
      | b == 0xE0 = lead 0xA0 0xBF 1 r
      | b >= 0xE1 && b <= 0xEC = cont 2 r
      | b == 0xED = lead 0x80 0x9F 1 r
      | b >= 0xEE && b <= 0xEF = cont 2 r
      | b == 0xF0 = lead 0x90 0xBF 2 r
      | b >= 0xF1 && b <= 0xF3 = cont 3 r
      | b == 0xF4 = lead 0x80 0x8F 2 r
      | otherwise = False
    lead lo hi n (c : r) | c >= lo && c <= hi = cont n r
    lead _ _ _ _ = False
    cont :: Int -> [Word8] -> Bool
    cont 0 r = go r
    cont n (c : r) | c >= 0x80 && c <= 0xBF = cont (n - 1) r
    cont _ _ = False

encodeChar :: Int -> [Word8]
encodeChar o
  | o < 0x80 = [fromIntegral o]
  | o < 0x800 = map fromIntegral [0xC0 .|. (o `shiftR` 6), 0x80 .|. (o .&. 0x3F)]
  | o < 0x10000 = map fromIntegral [0xE0 .|. (o `shiftR` 12), 0x80 .|. ((o `shiftR` 6) .&. 0x3F), 0x80 .|. (o .&. 0x3F)]
  | otherwise = map fromIntegral [0xF0 .|. (o `shiftR` 18), 0x80 .|. ((o `shiftR` 12) .&. 0x3F), 0x80 .|. ((o `shiftR` 6) .&. 0x3F), 0x80 .|. (o .&. 0x3F)]
