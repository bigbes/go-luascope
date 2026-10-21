package parser

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The bytecode the parser emits is compared with the bytecode of LuaJIT
// for every test source: the instructions, the constants and the frame
// sizes of all the prototypes. The parser drops the bytecode, but needs
// every instruction of it to fail where LuaJIT fails on code it cannot
// encode, see codegen.go.

// bytecodeOracle compares the bytecode, see testdata/oracle/bytecode.lua.
var bytecodeOracle = &oracle{
	script: "bytecode.lua",
	prefix: "bytecode-",
	result: bytecodeResult,
	render: renderBytecode,
	same:   func(want, got string) bool { return want == got },
}

// renderBytecode renders "ok" and a line per prototype, or a hash of them.
func renderBytecode(_ *testing.T, lines []string, hashed bool) string {
	body := strings.Join(append([]string{"ok"}, lines...), "\n")
	if hashed {
		sum := sha256.Sum256([]byte(body))
		return fmt.Sprintf("ok %x", sum[:8])
	}

	return body
}

// bytecodeResult renders the bytecode of the parser for a source.
func bytecodeResult(src []byte, hashed bool) string {
	p := newParser(src)
	p.keep = true

	if _, err := p.run("=m"); err != nil {
		var se *SyntaxError
		if errors.As(err, &se) {
			return "error " + escape(se.Detail())
		}

		return "error " + escape(err.Error())
	}

	var lines []string

	protoLines(p.main, &lines)

	return renderBytecode(nil, lines, hashed)
}

// protoLines renders a prototype and the prototypes of its constants like
// testdata/oracle/bytecode.lua.
func protoLines(pt *proto, out *[]string) {
	var (
		b        strings.Builder
		children []*proto
	)

	vararg := "f"
	if pt.vararg {
		vararg = "v"
	}

	fmt.Fprintf(&b, "%d %d %s %d |", pt.params, pt.framesize, vararg, pt.upvals)

	for _, ins := range pt.bc {
		fmt.Fprintf(&b, " %08x", uint32(ins))
	}

	b.WriteString(" |")

	for _, n := range pt.knum {
		b.WriteString(" " + formatNumber(n))
	}

	b.WriteString(" |")

	for _, k := range pt.kgc {
		switch k.kind {
		case "s":
			b.WriteString(" s:" + escapeConst(k.str))
		case "proto":
			b.WriteString(" proto")

			children = append(children, k.proto)
		default:
			b.WriteString(" " + k.kind)
		}
	}

	*out = append(*out, b.String())

	for _, c := range children {
		protoLines(c, out)
	}
}

// formatNumber formats a number like "%.17g" of C.
func formatNumber(n float64) string {
	switch {
	case math.IsInf(n, 1):
		return "inf"
	case math.IsInf(n, -1):
		return "-inf"
	default:
		return strconv.FormatFloat(n, 'g', 17, 64)
	}
}

// escapeConst escapes a string constant like the oracle: control
// characters, spaces, backslashes and bytes 128-255 as \ddd.
func escapeConst(s string) string {
	var b strings.Builder

	for i := range len(s) {
		c := s[i]
		if c <= ' ' || c == 127 || c == '\\' || c >= 128 {
			fmt.Fprintf(&b, "\\%03d", c)
		} else {
			b.WriteByte(c)
		}
	}

	return b.String()
}

// skipRace skips a comparison of the bytecode under the race detector.
// The bytecode is the same in every run, and the other suites parse the
// same sources, so the race detector checks the same code there; with it,
// the comparisons would take most of the time of "make testrace".
func skipRace(t *testing.T) {
	t.Helper()

	if raceEnabled {
		t.Skip("the bytecode is compared without the race detector")
	}
}

func TestBytecodeSnippets(t *testing.T) {
	skipRace(t)
	checkOracle(t, bytecodeOracle, "snippets",
		slices.Concat(snippets, limitSnippets(), codegenSnippets()), true)
}

// TestBytecodeInstructionsLimit checks the limit of LuaJIT on the bytecode
// of the functions being compiled, 2^26 instructions. A source that
// reaches it would take minutes to parse, so the test puts the main
// function near the end of the bytecode stack.
func TestBytecodeInstructionsLimit(t *testing.T) {
	for _, tt := range []struct {
		room int
		err  string
	}{
		{room: 3},
		{room: 2, err: "1: main function has more than 67108864 bytecode instructions"},
	} {
		p := newParser([]byte("f()"))
		fs := p.openFunc(0)
		fs.base = maxBCIns - uint32(tt.room) - 1 // The room left after the header.

		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = r.(*SyntaxError) //nolint:forcetypeassert // Fails the test otherwise.
				}
			}()

			fs.emit(insAD(bcFUNCV, 0, 0))
			p.next()
			p.chunk() // GGET and CALL, then the final RET0.
			p.closeFunc()

			return nil
		}()

		if tt.err == "" {
			require.NoError(t, err)
		} else {
			var se *SyntaxError
			require.ErrorAs(t, err, &se)
			require.Equal(t, tt.err, se.Detail())
		}
	}
}

func TestBytecodeGenerated(t *testing.T) {
	skipRace(t)
	checkOracle(t, bytecodeOracle, "generated", generatedCases(), true)
}

func TestBytecodeCorpus(t *testing.T) {
	skipRace(t)
	for _, suite := range corpusSuites {
		t.Run(suite, func(t *testing.T) {
			checkOracle(t, bytecodeOracle, suite, loadCorpus(t, suite), true)
		})
	}
}

func TestBytecodeMutations(t *testing.T) {
	skipRace(t)
	checkOracle(t, bytecodeOracle, "mutations", mutationCases(t), true)
}
