package parser

import (
	"bytes"
	"errors"
	"strconv"
)

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
	ep     int  // Offset of the exponent mark or -1.
	exp    int  // Value of the exponent.
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

// scanNumber checks a number lexeme the way LuaJIT with FFI does: decimal,
// hexadecimal and binary literals, hexadecimal exponents and the "i", "LL",
// "ULL" and "LLU" suffixes. It returns the value of a number, or reports
// that the literal is cdata: a 64-bit integer or an imaginary number.
func scanNumber(s []byte) (value float64, cdata, ok bool) {
	n := &numScanner{s: s, base: 10, fmt: fmtInt, dp: -1, ep: -1}

	if !n.prefix() || !n.digits() || !n.point() || !n.exponent() || !n.suffix() {
		return 0, false, false
	}

	if n.p != len(s) || !n.convert() {
		return 0, false, false
	}

	switch n.fmt {
	case fmtImag, fmtI64, fmtU64:
		return 0, true, true
	default:
		return n.value(), false, true
	}
}

// maxDigits is STRSCAN_MAXDIG: LuaJIT converts only the first 800
// significant digits of a decimal number.
const maxDigits = 800

// value converts a number lexeme without a suffix to a double. LuaJIT
// rounds correctly, as strconv does, but drops the decimal digits after
// the first 800 significant ones.
func (n *numScanner) value() float64 {
	var (
		v   float64
		err error
	)

	switch n.base {
	case 2:
		var x uint64
		for _, c := range n.s[n.sp : n.sp+n.dig] {
			x = x<<1 | uint64(c-'0')
		}

		return float64(x)
	case 16:
		s := string(n.s)
		if !bytes.ContainsAny(n.s, "pP") {
			s += "p0"
		}

		v, err = strconv.ParseFloat(s, 64)
	default:
		v, err = strconv.ParseFloat(n.decimal(), 64)
	}

	if err != nil && !errors.Is(err, strconv.ErrRange) {
		panic("parser: number " + strconv.Quote(string(n.s)) + ": " + err.Error())
	}

	return v
}

// decimal returns a decimal number as "0.<digits>e<exponent>" with at most
// maxDigits significant digits.
func (n *numScanner) decimal() string {
	mant := n.s
	if n.ep >= 0 {
		mant = n.s[:n.ep]
	}

	point := bytes.IndexByte(mant, '.')
	if point < 0 {
		point = len(mant)
	}

	digits := make([]byte, 0, len(mant))
	for _, c := range mant {
		if c == '.' {
			continue
		}

		if len(digits) == 0 && c == '0' {
			point--
			continue
		}

		digits = append(digits, c)
	}

	if len(digits) == 0 {
		return "0"
	}

	return "0." + string(digits[:min(len(digits), maxDigits)]) + "e" + strconv.Itoa(point+n.exp)
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
	n.ep = n.p
	n.p++

	sign := 1

	if c := n.cur(); c == '+' || c == '-' {
		if c == '-' {
			sign = -1
		}

		n.p++
	}

	if !isDigit(n.cur()) {
		return false
	}

	for ; isDigit(n.cur()); n.p++ {
		n.exp = n.exp*10 + n.cur() - '0'
		if n.exp >= maxExp {
			return false
		}
	}

	n.exp *= sign

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
