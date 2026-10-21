package parser

import (
	"math"
	"math/big"
	"sync"
)

// LuaJIT folds "^" on number constants with pow() of the C library. The
// result is a constant of the function, so its value decides whether it
// is shared with another constant. math.Pow is not rounded correctly and
// often differs from glibc in the last bit, like on 10^33 in the Lua 5.1
// tests, see TestPow. The parser rounds the power correctly instead.
//
// Measured once against LuaJIT with glibc 2.43, on 26727 powers of the
// integers from -12 to 120 and 18 fractions to the integers from -80 to 80
// and 16 fractions: math.Pow differed in 10841 of them (41%; 37% of the
// integer powers of integers), this pow in 51, all within 0.0013 ulp of a
// tie, where glibc rounds either way. LuaJIT with another C library may
// differ in more cases.

// powPrec is the precision of the computation of a power in bits.
const powPrec = 320

// pow returns x**y rounded correctly.
func pow(x, y float64) float64 {
	if y == 0 || x == 1 || x == 0 || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) ||
		math.IsInf(y, 0) {

		return math.Pow(x, y) // The special cases of C99, the same in Go.
	}

	neg := false

	if x < 0 {
		if y != math.Trunc(y) {
			return math.NaN()
		}

		x = -x
		neg = math.Mod(y, 2) != 0
	}

	var r float64

	if y == math.Trunc(y) && math.Abs(y) <= 64 {
		r = intPow(x, int(y))
	} else {
		r = expLog(x, y)
	}

	if neg {
		return -r
	}

	return r
}

// intPow returns x**n for x > 0 and |n| <= 64. A positive power is exact
// before the rounding, so a tie rounds to even.
func intPow(x float64, n int) float64 {
	abs := max(n, -n)
	prec := uint(53*abs + 64)
	r := new(big.Float).SetPrec(prec).SetInt64(1)
	b := new(big.Float).SetPrec(prec).SetFloat64(x)

	for range abs {
		r.Mul(r, b)
	}

	if n < 0 {
		r = new(big.Float).SetPrec(powPrec).Quo(newFloat(1), r)
	}

	f, _ := r.Float64()

	return f
}

// expLog returns x**y = exp(y*ln(x)) for x > 0.
func expLog(x, y float64) float64 {
	t := newFloat(0).Mul(bigLog(x), newFloat(y))

	switch tf, _ := t.Float64(); {
	case tf > 800: // Above the largest double, e^709.8.
		return math.Inf(1)
	case tf < -800: // Below half of the smallest double, e^-745.1.
		return 0
	}

	f, _ := bigExp(t).Float64()

	return f
}

func newFloat(x float64) *big.Float { return new(big.Float).SetPrec(powPrec).SetFloat64(x) }

// ln2 is ln(2) = 2*atanh(1/3).
var ln2 = sync.OnceValue(func() *big.Float {
	return atanh2(newFloat(0).Quo(newFloat(1), newFloat(3)))
})

// atanh2 returns 2*atanh(z) = ln((1+z)/(1-z)) for |z| <= 1/3.
func atanh2(z *big.Float) *big.Float {
	z2 := newFloat(0).Mul(z, z)
	term := newFloat(0).Set(z)
	sum := newFloat(0).Set(z)

	for k := 3.0; ; k += 2 {
		term.Mul(term, z2)
		t := newFloat(0).Quo(term, newFloat(k))

		if t.Sign() == 0 || t.MantExp(nil) < sum.MantExp(nil)-powPrec-8 {
			break
		}

		sum.Add(sum, t)
	}

	return sum.Mul(sum, newFloat(2))
}

// bigLog returns ln(x) for x > 0: with x = m*2^e and m in [0.5, 1),
// ln(x) = 2*atanh((m-1)/(m+1)) + e*ln(2).
func bigLog(x float64) *big.Float {
	m := newFloat(x)
	e := m.MantExp(m)
	z := newFloat(0).Quo(newFloat(0).Sub(m, newFloat(1)), newFloat(0).Add(m, newFloat(1)))
	r := atanh2(z)

	return r.Add(r, newFloat(0).Mul(ln2(), newFloat(float64(e))))
}

// bigExp returns e^t for |t| <= 800: with t = k*ln(2) + r,
// e^t = 2^k * (e^(r/2^16))^(2^16), and the Taylor series for e^(r/2^16).
func bigExp(t *big.Float) *big.Float {
	const halvings = 16

	q, _ := newFloat(0).Quo(t, ln2()).Float64()
	k := math.Round(q)
	r := newFloat(0).Sub(t, newFloat(0).Mul(ln2(), newFloat(k)))
	r.SetMantExp(r, -halvings)

	sum := newFloat(1)
	term := newFloat(1)

	for n := 1.0; ; n++ {
		term.Mul(term, r)
		term.Quo(term, newFloat(n))

		if term.Sign() == 0 || term.MantExp(nil) < -powPrec-8 {
			break
		}

		sum.Add(sum, term)
	}

	for range halvings {
		sum.Mul(sum, sum)
	}

	return sum.SetMantExp(sum, int(k))
}
