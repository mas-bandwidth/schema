// Lexer for the schema language (docs/SPEC.md §4.1).
//
// The scanner emits NL tokens as they are; the parser decides where a
// newline is significant (SPEC.md §4.1 lists the suppression points).
const std = @import("std");

pub const Tag = enum {
    ident,
    int,
    float,
    string,
    lbrace,
    rbrace,
    lparen,
    rparen,
    lbrack,
    rbrack,
    comma,
    colon,
    coloncolon,
    eq,
    bang,
    question,
    dot,
    dotdot,
    pipe,
    plus,
    minus,
    star,
    slash,
    percent,
    nl,
    eof,
};

pub const Token = struct {
    tag: Tag,
    text: []const u8,
    line: u32,
};

pub const Error = error{ LexError, OutOfMemory };

pub fn lex(gpa: std.mem.Allocator, src: []const u8, out: *std.ArrayList(Token)) Error!void {
    var i: usize = 0;
    var line: u32 = 1;
    // SPEC.md §4.1: a UTF-8 BOM is rejected at parse.
    if (src.len >= 3 and src[0] == 0xEF and src[1] == 0xBB and src[2] == 0xBF) return error.LexError;
    while (i < src.len) {
        const c = src[i];
        if (c == ' ' or c == '\t' or c == '\r') {
            i += 1;
            continue;
        }
        if (c == '\n') {
            try out.append(gpa, .{ .tag = .nl, .text = src[i .. i + 1], .line = line });
            line += 1;
            i += 1;
            continue;
        }
        if (c == '/' and i + 1 < src.len and src[i + 1] == '/') {
            while (i < src.len and src[i] != '\n') i += 1;
            continue;
        }
        if (c == '/' and i + 1 < src.len and src[i + 1] == '*') {
            i += 2;
            while (i + 1 < src.len and !(src[i] == '*' and src[i + 1] == '/')) {
                if (src[i] == '\n') line += 1;
                i += 1;
            }
            if (i + 1 >= src.len) return error.LexError;
            i += 2;
            continue;
        }
        if (std.ascii.isAlphabetic(c) or c == '_') {
            const s = i;
            while (i < src.len and (std.ascii.isAlphanumeric(src[i]) or src[i] == '_')) i += 1;
            try out.append(gpa, .{ .tag = .ident, .text = src[s..i], .line = line });
            continue;
        }
        if (std.ascii.isDigit(c)) {
            const s = i;
            var is_float = false;
            if (c == '0' and i + 1 < src.len and (src[i + 1] == 'x' or src[i + 1] == 'X' or src[i + 1] == 'b' or src[i + 1] == 'B')) {
                i += 2;
                while (i < src.len and (std.ascii.isHex(src[i]) or src[i] == '_')) i += 1;
            } else {
                while (i < src.len and std.ascii.isDigit(src[i])) i += 1;
                // a fraction: '.' followed by a digit ('..' is the range operator)
                if (i + 1 < src.len and src[i] == '.' and std.ascii.isDigit(src[i + 1])) {
                    is_float = true;
                    i += 1;
                    while (i < src.len and std.ascii.isDigit(src[i])) i += 1;
                } else if (i + 1 < src.len and src[i] == '.' and src[i + 1] != '.') {
                    // "1." with no digits after
                    is_float = true;
                    i += 1;
                }
                if (i < src.len and (src[i] == 'e' or src[i] == 'E')) {
                    is_float = true;
                    i += 1;
                    if (i < src.len and (src[i] == '+' or src[i] == '-')) i += 1;
                    while (i < src.len and std.ascii.isDigit(src[i])) i += 1;
                }
            }
            try out.append(gpa, .{ .tag = if (is_float) .float else .int, .text = src[s..i], .line = line });
            continue;
        }
        if (c == '"') {
            const s = i + 1;
            i += 1;
            while (i < src.len and src[i] != '"' and src[i] != '\n') i += 1;
            if (i >= src.len or src[i] != '"') return error.LexError;
            try out.append(gpa, .{ .tag = .string, .text = src[s..i], .line = line });
            i += 1;
            continue;
        }
        const two: ?Tag = if (i + 1 < src.len) blk: {
            if (c == '.' and src[i + 1] == '.') break :blk .dotdot;
            if (c == ':' and src[i + 1] == ':') break :blk .coloncolon;
            break :blk null;
        } else null;
        if (two) |t| {
            try out.append(gpa, .{ .tag = t, .text = src[i .. i + 2], .line = line });
            i += 2;
            continue;
        }
        const t: Tag = switch (c) {
            '{' => .lbrace,
            '}' => .rbrace,
            '(' => .lparen,
            ')' => .rparen,
            '[' => .lbrack,
            ']' => .rbrack,
            ',' => .comma,
            ':' => .colon,
            '=' => .eq,
            '!' => .bang,
            '?' => .question,
            '.' => .dot,
            '|' => .pipe,
            '+' => .plus,
            '-' => .minus,
            '*' => .star,
            '/' => .slash,
            '%' => .percent,
            else => return error.LexError,
        };
        try out.append(gpa, .{ .tag = t, .text = src[i .. i + 1], .line = line });
        i += 1;
    }
    try out.append(gpa, .{ .tag = .nl, .text = "", .line = line });
    try out.append(gpa, .{ .tag = .eof, .text = "", .line = line });
}
