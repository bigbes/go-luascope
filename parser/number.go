package parser

// Number formats of lj_strscan.c, in the same order.
type numFormat int

const (
	fmtNum numFormat = iota
	fmtImag
	fmtInt
	fmtU32
	fmtI64
	fmtU64
)

// maxExp is STRSCAN_MAXEXP.
const maxExp = 1 << 20

// numScanner holds the state of lj_strscan_scan for a number lexeme.
type numScanner struct {
	s      []byte
	p      int
	base   int
	fmt    numFormat
	dig    int  // Significant digits.
	hasDig bool // Leading zeros were seen.
	dp     int  // Offset of the decimal point or -1.
	sp     int  // Offset of the first significant digit.
}

// at returns the character at offset i or 0 past the end, like the
// NUL-terminated buffer LuaJIT scans.
func (n *numScanner) at(i int) int {
	if i < len(n.s) {
		return int(n.s[i])
	}

	return 0
}

func (n *numScanner) cur() int { return n.at(n.p) }

// validNumber reports whether a number lexeme is accepted by LuaJIT with FFI
// enabled: decimal, hexadecimal and binary literals, hexadecimal exponents
// and the "i", "LL", "ULL" and "LLU" suffixes.
func validNumber(s []byte) bool {
	n := &numScanner{s: s, base: 10, fmt: fmtInt, dp: -1}

	if !n.prefix() || !n.digits() || !n.point() || !n.exponent() || !n.suffix() {
		return false
	}

	return n.p == len(s) && n.convert()
}

// prefix detects the base and skips leading zeros.
func (n *numScanner) prefix() bool {
	if n.cur() > '0' {
		return true
	}

	if n.cur() == '0' {
		switch n.at(1) | 0x20 {
		case 'x':
			n.base, n.p = 16, 2
		case 'b':
			n.base, n.p = 2, 2
		}
	}

	for ; ; n.p++ {
		switch n.cur() {
		case '0':
			n.hasDig = true
		case '.':
			if n.dp >= 0 {
				return false
			}

			n.dp = n.p
		default:
			return true
		}
	}
}

func (n *numScanner) isDigit(c int) bool {
	if n.base == 16 {
		return isXDigit(c)
	}

	return isDigit(c)
}

// digits scans significant digits and the decimal point.
func (n *numScanner) digits() bool {
	for n.sp = n.p; ; n.p++ {
		switch c := n.cur(); {
		case n.isDigit(c):
			n.dig++
		case c == '.':
			if n.dp >= 0 {
				return false
			}

			n.dp = n.p
		default:
			return n.hasDig || n.dig > 0
		}
	}
}

// point checks the fractional part.
func (n *numScanner) point() bool {
	if n.dp < 0 {
		return true
	}

	if n.base == 2 {
		return false
	}

	n.fmt = fmtNum
	if n.dig == 0 {
		return true
	}

	// Count fractional digits without trailing zeros.
	ex := n.dp - (n.p - 1)
	for i := n.p - 1; ex < 0 && n.s[i] == '0'; i-- {
		ex++
	}

	return ex > -maxExp
}

// exponent scans the "e" or "p" exponent.
func (n *numScanner) exponent() bool {
	mark := 'e'
	if n.base == 16 {
		mark = 'p'
	}

	if n.base < 10 || n.cur()|0x20 != int(mark) {
		return true
	}

	n.fmt = fmtNum
	n.p++

	if c := n.cur(); c == '+' || c == '-' {
		n.p++
	}

	if !isDigit(n.cur()) {
		return false
	}

	for x := 0; isDigit(n.cur()); n.p++ {
		x = x*10 + n.cur() - '0'
		if x >= maxExp {
			return false
		}
	}

	return true
}

// suffix scans the "i", "LL", "ULL" and "LLU" suffixes.
func (n *numScanner) suffix() bool {
	if n.p == len(n.s) {
		return true
	}

	if n.cur()|0x20 == 'i' {
		n.p++
		n.fmt = fmtImag

		return true
	}

	if n.fmt != fmtInt {
		return false
	}

	if n.cur()|0x20 == 'u' {
		n.p++
		n.fmt = fmtU32
	}

	if n.cur()|0x20 == 'l' {
		n.p++
		if n.cur()|0x20 != 'l' {
			return false
		}

		n.p++
		n.fmt += fmtI64 - fmtInt
	}

	if n.cur()|0x20 == 'u' && (n.fmt == fmtInt || n.fmt == fmtI64) {
		n.p++
		n.fmt += fmtU32 - fmtInt
	}

	return n.fmt != fmtU32
}

// convert applies the range checks of the base-specific converters.
func (n *numScanner) convert() bool {
	is64 := n.fmt == fmtI64 || n.fmt == fmtU64

	switch n.base {
	case 16:
		return !is64 || n.dig <= 16
	case 2:
		return n.dig <= 64 && n.binaryDigits()
	default:
		return !is64 || n.fitsUint64()
	}
}

func (n *numScanner) binaryDigits() bool {
	for i := n.sp; i < n.sp+n.dig; i++ {
		if n.s[i] != '0' && n.s[i] != '1' {
			return false
		}
	}

	return true
}

// fitsUint64 reports whether the decimal integer is below 2^64.
func (n *numScanner) fitsUint64() bool {
	const maxDiv10, maxMod10 = (1<<64 - 1) / 10, (1<<64 - 1) % 10

	var x uint64

	for i := n.sp; i < n.sp+n.dig; i++ {
		d := uint64(n.s[i] - '0')
		if x > maxDiv10 || x == maxDiv10 && d > maxMod10 {
			return false
		}

		x = x*10 + d
	}

	return true
}
