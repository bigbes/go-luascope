package parser

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPow checks the folding of "^" against the results of LuaJIT with
// glibc, given as the bits of the doubles.
func TestPow(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		x, y float64
		want uint64
	}{
		{10, 33, 5100510072459938204}, // math.Pow is an ulp above.
		{2, 0.5, 4609047870845172685},
		{3, -7, 4557070348872234591},
		{1.1, 100, 4668801586081217429},
		{7, 1.5, 4625906207202777549},
		{2, 1023, 9214364837600034816},
		{2, -1074, 1},
		{2, -1075, 0},
		{10, 308, 9214871658872686752},
		{10, 309, 9218868437227405312}, // +Inf.
		{-2, 3, math.Float64bits(-8)},
		{5, 23, 4847330063900822262}, // A tie, glibc rounds it to even too.
	} {
		assert.Equal(t, tt.want, math.Float64bits(pow(tt.x, tt.y)), "%v^%v", tt.x, tt.y)
	}

	// 3^34 is a tie as well: pow rounds it to even, glibc rounds it up to
	// 16677181699666570.
	assert.InDelta(t, 16677181699666568.0, pow(3, 34), 0)
	assert.True(t, math.IsNaN(pow(-8, 1.0/3)))
	assert.Equal(t, math.Inf(1), pow(0, -1))
}
