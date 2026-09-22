#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    ValueOutOfRange,
    UnexpectedEof,
    InvalidEncoding,
    TrailingData,
}

pub trait ReadStream {
    fn read_u8(&mut self) -> Result<u8, Error>;
    fn read_u16(&mut self) -> Result<u16, Error>;
    fn read_u32(&mut self) -> Result<u32, Error>;
    fn read_u64(&mut self) -> Result<u64, Error>;
    fn read_i8(&mut self) -> Result<i8, Error>;
    fn read_i16(&mut self) -> Result<i16, Error>;
    fn read_i32(&mut self) -> Result<i32, Error>;
    fn read_i64(&mut self) -> Result<i64, Error>;
    fn read_f32(&mut self) -> Result<f32, Error>;
    fn read_f64(&mut self) -> Result<f64, Error>;
    fn read_bytes(&mut self, buf: &mut [u8]) -> Result<(), Error>;
    fn read_bool(&mut self) -> Result<bool, Error>;
    fn position(&self) -> u64;
    fn seek(&mut self, pos: u64) -> Result<(), Error>;
}

pub trait WriteStream {
    fn write_u8(&mut self, v: u8) -> Result<(), Error>;
    fn write_u16(&mut self, v: u16) -> Result<(), Error>;
    fn write_u32(&mut self, v: u32) -> Result<(), Error>;
    fn write_u64(&mut self, v: u64) -> Result<(), Error>;
    fn write_i8(&mut self, v: i8) -> Result<(), Error>;
    fn write_i16(&mut self, v: i16) -> Result<(), Error>;
    fn write_i32(&mut self, v: i32) -> Result<(), Error>;
    fn write_i64(&mut self, v: i64) -> Result<(), Error>;
    fn write_f32(&mut self, v: f32) -> Result<(), Error>;
    fn write_f64(&mut self, v: f64) -> Result<(), Error>;
    fn write_bytes(&mut self, buf: &[u8]) -> Result<(), Error>;
    fn write_bool(&mut self, v: bool) -> Result<(), Error>;
    fn position(&self) -> u64;
}

pub trait Stream: ReadStream + WriteStream {}
