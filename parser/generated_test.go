package parser

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// generator writes random programs that mix scopes: locals, parameters,
// nested functions, loops, repeat-until, gotos and labels. Names come from
// a small pool, so the same name is often local in one place and global
// in another. Many programs are invalid, which tests the errors as well.
type generator struct {
	r     *rand.Rand
	b     strings.Builder
	depth int
}

var genNames = []string{"a", "b", "x", "self", "goto", "f"}

func (g *generator) pick(list []string) string { return list[g.r.IntN(len(list))] }

func (g *generator) name() string { return g.pick(genNames) }

func (g *generator) write(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

func (g *generator) params() string {
	params := make([]string, 0, 3)
	for range g.r.IntN(3) {
		params = append(params, g.name())
	}

	if g.r.IntN(4) == 0 {
		params = append(params, "...")
	}

	return strings.Join(params, ", ")
}

func (g *generator) expr() {
	if g.depth > 4 {
		g.write("%s", g.name())
		return
	}

	g.depth++
	defer func() { g.depth-- }()

	switch g.r.IntN(9) {
	case 0:
		g.write("%s.%s", g.name(), g.name())
	case 1:
		g.write("%s(", g.name())
		g.expr()
		g.write(")")
	case 2:
		g.write("function(%s) ", g.params())
		g.block()
		g.write(" end")
	case 3:
		g.write("...")
	case 4:
		g.write("{%s, %s = ", g.name(), g.name())
		g.expr()
		g.write("}")
	case 5:
		g.expr()
		g.write(" %s ", g.pick([]string{"+", "..", "and", "=="}))
		g.expr()
	case 6:
		g.write("%s:%s()", g.name(), g.name())
	default:
		g.write("%s", g.name())
	}
}

func (g *generator) block() {
	for range 1 + g.r.IntN(3) {
		g.stmt()
		g.write("\n")
	}
}

func (g *generator) loop(kind int) {
	switch kind {
	case 0:
		g.write("while ")
		g.expr()
		g.write(" do ")
		g.block()
		g.write(" end")
	case 1:
		g.write("repeat ")
		g.block()
		g.write(" until ")
		g.expr()
	case 2:
		g.write("for %s = 1, ", g.name())
		g.expr()
		g.write(" do ")
		g.block()
		g.write(" end")
	default:
		g.write("for %s, %s in ", g.name(), g.name())
		g.expr()
		g.write(" do ")
		g.block()
		g.write(" end")
	}
}

func (g *generator) stmt() {
	if g.depth > 4 {
		g.write("%s = %s", g.name(), g.name())
		return
	}

	g.depth++
	defer func() { g.depth-- }()

	switch k := g.r.IntN(14); k {
	case 0, 1:
		g.write("local %s = ", g.name())
		g.expr()
	case 2:
		g.write("local function %s(%s) ", g.name(), g.params())
		g.block()
		g.write(" end")
	case 3:
		g.write("function %s%s(%s) ", g.name(), g.pick([]string{"", ".m", ":m"}), g.params())
		g.block()
		g.write(" end")
	case 4, 5:
		g.write("%s%s = ", g.name(), g.pick([]string{"", ".k", "[x]"}))
		g.expr()
	case 6:
		g.write("do ")
		g.block()
		g.write(" end")
	case 7, 8, 9:
		g.loop(k - 6)
	case 10:
		g.write("if ")
		g.expr()
		g.write(" then ")
		g.block()
		g.write(" else ")
		g.block()
		g.write(" end")
	case 11:
		g.write("goto %s", g.pick([]string{"l1", "l2"}))
	case 12:
		g.write("::%s::", g.pick([]string{"l1", "l2"}))
	default:
		g.write("%s(", g.name())
		g.expr()
		g.write(")")
	}
}

// generatedPrograms is the number of generated programs.
const generatedPrograms = 2000

func TestGenerated(t *testing.T) {
	cases := make([]testCase, 0, generatedPrograms)

	for i := range generatedPrograms {
		g := &generator{r: rand.New(rand.NewPCG(uint64(i), 42))} //nolint:gosec // Not for security.
		g.block()

		name := fmt.Sprintf("program-%d", i)
		cases = append(cases, testCase{name: name, src: []byte(g.b.String())})
	}

	checkSuite(t, "generated", cases, true)
}
