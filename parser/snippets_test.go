package parser

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// snippets cover the lexer and parser rules one by one. The expected
// results, including the global variables, come from LuaJIT.
var snippets = []testCase{
	// Numbers.
	{"number/int", []byte("x = 1 + 0 + 00 + 123")},
	{"number/float", []byte("x = 1.5 + .5 + 5. + 1e10 + 1E-3 + 1e+3 + 0.000")},
	{"number/hex", []byte("x = 0x10 + 0XfF + 0x.8 + 0x1p4 + 0x1P-2 + 0x1.8p+1")},
	{"number/hex-e-minus", []byte("x = 0xe-1")},
	{"number/binary", []byte("x = 0b101 + 0B11")},
	{"number/binary-digit", []byte("x = 0b102")},
	{"number/binary-point", []byte("x = 0b1.0")},
	{"number/binary-exp", []byte("x = 0b1e1")},
	{"number/binary-65", []byte("x = 0b" + strings.Repeat("1", 65))},
	{"number/binary-64", []byte("x = 0b" + strings.Repeat("1", 64) + "ULL")},
	{"number/ll", []byte("x = 1LL + 1ll + 1lL + 0x10LL + 0b1LL")},
	{"number/ull", []byte("x = 1ULL + 1ull + 1LLU + 1llu")},
	{"number/u", []byte("x = 1U")},
	{"number/l", []byte("x = 1L")},
	{"number/ullu", []byte("x = 1ULLU")},
	{"number/float-ll", []byte("x = 1.0LL")},
	{"number/exp-ll", []byte("x = 1e2LL")},
	{"number/imag", []byte("x = 1i + 1.5i + 1e2I + 0x10i + 0b1i")},
	{"number/imag-ll", []byte("x = 1LLi")},
	{"number/ll-max", []byte("x = 18446744073709551615ULL")},
	{"number/ll-overflow", []byte("x = 18446744073709551616ULL")},
	{"number/ll-zeros", []byte("x = 000000000000000000000000001LL")},
	{"number/hex-ll-16", []byte("x = 0xffffffffffffffffLL")},
	{"number/hex-ll-17", []byte("x = 0x1ffffffffffffffffLL")},
	{"number/hex-ll-zeros", []byte("x = 0x0000ffffffffffffffffLL")},
	{"number/big-int", []byte("x = 123456789012345678901234567890")},
	{"number/exp-max", []byte("x = 1e1048575")},
	{"number/exp-over", []byte("x = 1e1048576")},
	{"number/double-dot", []byte("x = 3..2")},
	{"number/trailing-letter", []byte("x = 3x")},
	{"number/x-only", []byte("x = 0x")},
	{"number/exp-only", []byte("x = 1e")},
	{"number/exp-sign-only", []byte("x = 1e+")},
	{"number/dot-hex", []byte("x = .0x1")},
	{"number/utf8-suffix", []byte("x = 1\xc3\xa9")},
	{"number/concat", []byte("x = 1 .. 2")},
	{"number/method", []byte("x = 1:foo()")},

	// Short strings.
	{"string/simple", []byte(`x = "a" .. 'b' .. "it's" .. 'say "hi"'`)},
	{"string/escapes", []byte(`x = "\a\b\f\n\r\t\v\\\"\'"`)},
	{"string/decimal", []byte(`x = "\0\65\065\255\1234"`)},
	{"string/decimal-256", []byte(`x = "\256"`)},
	{"string/hex", []byte(`x = "\x41\x7a\xFf"`)},
	{"string/hex-short", []byte(`x = "\x4"`)},
	{"string/hex-bad", []byte(`x = "\xg0"`)},
	{"string/unicode", []byte(`x = "\u{48}\u{418}\u{20AC}\u{1F600}\u{10FFFF}\u{000041}"`)},
	{"string/unicode-big", []byte(`x = "\u{110000}"`)},
	{"string/unicode-surrogate", []byte(`x = "\u{D800}"`)},
	{"string/unicode-empty", []byte(`x = "\u{}"`)},
	{"string/unicode-no-brace", []byte(`x = "\u48"`)},
	{"string/unicode-unclosed", []byte(`x = "\u{48"`)},
	{"string/z", []byte("x = \"a\\z   \n\n   b\"\ny = z")},
	{"string/backslash-newline", []byte("x = \"a\\\nb\"\ny = z")},
	{"string/backslash-crlf", []byte("x = \"a\\\r\nb\"\ny = z")},
	{"string/invalid-escape", []byte(`x = "abc\q"`)},
	{"string/unfinished", []byte(`x = "abc`)},
	{"string/newline", []byte("x = \"abc\ndef\"")},
	{"string/backslash-eof", []byte(`x = "abc\`)},
	{"string/nul", []byte("x = \"a\x00b\"")},
	{"string/high-bytes", []byte("x = \"\xff\xfe\"")},
	{"string/error-after-nul", []byte(`x = "a\u{00}b\u{100}\q"`)},
	{"string/unfinished-after-nul", []byte("x = 'a\x00b\n")},

	// Long strings and comments.
	{"long/simple", []byte("x = [[abc]]")},
	{"long/level", []byte("x = [==[a]]b]=]c]==]")},
	{"long/first-newline", []byte("x = [[\nabc]]\ny = z")},
	{"long/crlf", []byte("x = [[\r\na\r\nb]]\r\ny = z")},
	{"long/nested", []byte("x = [[ [[ ]]")},
	{"long/unfinished", []byte("x = [[abc")},
	{"long/unfinished-level", []byte("x = [==[abc]=]")},
	{"long/bad-delimiter", []byte("x = [=abc")},
	{"long/bracket-eq", []byte("x = a[=1]")},
	{"long/index", []byte("x = a[ [[k]] ]")},
	{"long/call", []byte("f[[abc]]")},
	{"comment/short", []byte("-- comment\nx = 1 -- trailing")},
	{"comment/long", []byte("--[[ multi\nline ]] x = y")},
	{"comment/long-level", []byte("--[==[ ]] ]==] x = y")},
	{"comment/unfinished", []byte("--[[ abc")},
	{"comment/not-long", []byte("--[= abc\nx = y")},
	{"comment/bracket-only", []byte("--[\nx = y")},
	{"comment/eof", []byte("x = y --")},

	// Chunk header.
	{"header/shebang", []byte("#!/usr/bin/tarantool\nx = y")},
	{"header/shebang-only", []byte("#!/usr/bin/tarantool")},
	{"header/bom", []byte("\xef\xbb\xbfx = y")},
	{"header/bom-shebang", []byte("\xef\xbb\xbf#!lua\nx = y")},
	{"header/hash-later", []byte("x = 1\n#!lua")},
	{"header/bytecode-after-shebang", []byte("#!lua\n\x1bLJ")},

	// Line breaks.
	{"lines/cr", []byte("x = 1\ry = 2\rz(")},
	{"lines/crlf", []byte("x = 1\r\ny = 2\r\nz(")},
	{"lines/lfcr", []byte("x = 1\n\ry = 2\n\rz(")},
	{"lines/lflf", []byte("x = 1\n\ny = 2\n\nz(")},
	{"lines/crcr", []byte("x = 1\r\ry = 2\r\rz(")},

	// Names and keywords.
	{"name/utf8", []byte("привет = мир")},
	{"name/goto-local", []byte("local goto = 1 return goto")},
	{"name/goto-global", []byte("goto = 1 print(goto)")},
	{"name/goto-field", []byte("local t = {goto = 1} t.goto = t.goto")},
	{"name/goto-param", []byte("local function f(goto) return goto end")},
	{"name/goto-method", []byte("x:goto()")},
	{"name/goto-for", []byte("for goto = 1, 2 do end")},
	{"name/goto-goto", []byte("goto goto")},
	{"name/goto-call", []byte("goto(1)")},
	{"name/goto-label", []byte("goto l ::l::")},
	{"name/label-goto", []byte("::goto::")},
	{"name/keyword-field", []byte("x = t.end")},
	{"name/keyword-local", []byte("local end = 1")},

	// Unknown and control characters.
	{"char/nul", []byte("x = \x00")},
	{"char/control", []byte("x = \x01")},
	{"char/del", []byte("x = \x7f")},
	{"char/bang", []byte("x = !y")},
	{"char/at", []byte("x = @")},
	{"char/dollar", []byte("$")},
	{"char/backtick", []byte("x = `y`")},
	{"char/tilde", []byte("x = ~y")},
	{"char/floor-div", []byte("x = a // b")},
	{"char/bitwise", []byte("x = a & b | c << 1")},
	{"char/attrib", []byte("local x <const> = 1")},

	// Expressions.
	{"expr/precedence", []byte("x = a + b * c ^ d ^ e .. f .. g == h and i or not j")},
	{"expr/unary", []byte("x = - - a + not not b + # # c")},
	{"expr/compare", []byte("x = a < b, a <= b, a > b, a >= b, a == b, a ~= b")},
	{"expr/paren", []byte("x = (a)")},
	{"expr/paren-unclosed", []byte("x = (a")},
	{"expr/paren-unclosed-line", []byte("x = (a\n+ b\ny = 1")},
	{"expr/dangling-op", []byte("x = a +")},
	{"expr/double-op", []byte("x = a + * b")},
	{"expr/vararg-main", []byte("x = ...")},
	{"expr/vararg-func", []byte("function f() return ... end")},
	{"expr/vararg-param", []byte("function f(...) return ... end")},
	{"expr/vararg-nested", []byte("function f(...) return function() return ... end end")},
	{"expr/function", []byte("x = function(a, b, ...) return a + b, y end")},
	{"expr/string-call", []byte("x = f'a' .. g\"b\" .. h[[c]] .. i{}")},
	{"expr/string-method", []byte("x = ('a'):upper()")},
	{"expr/string-index", []byte("x = 'a'.len")},
	{"expr/number-call", []byte("x = 1()")},

	// Tables.
	{"table/forms", []byte("x = {1, 2; a = 1, [k] = v, ['s'] = 3, f(), ...}")},
	{"table/trailing", []byte("x = {1, 2,}")},
	{"table/trailing-semicolon", []byte("x = {1; 2;}")},
	{"table/empty-sep", []byte("x = {,}")},
	{"table/double-sep", []byte("x = {1,,2}")},
	{"table/name-eq", []byte("x = {a == b}")},
	{"table/unclosed", []byte("x = {1, 2")},
	{"table/unclosed-line", []byte("x = {1,\n2\ny = 1")},
	{"table/key-no-value", []byte("x = {a = }")},
	{"table/bracket-key", []byte("x = {[1]}")},

	// Calls and assignments.
	{"call/ambiguous", []byte("f\n(g)")},
	{"call/ambiguous-paren", []byte("local a = f\n(g).x = 1")},
	{"call/same-line", []byte("f (g)")},
	{"call/after-string", []byte("f [[\n]](g)")},
	{"call/method-no-args", []byte("a:b")},
	{"call/method-string", []byte("a:b'c'")},
	{"call/method-table", []byte("a:b{}")},
	{"call/args-unclosed", []byte("f(a, b")},
	{"call/args-trailing-comma", []byte("f(a, )")},
	{"assign/multi", []byte("a, b.c, d[e] = 1, 2, 3")},
	{"assign/paren", []byte("(a) = 1")},
	{"assign/paren-field", []byte("(a).b = 1")},
	{"assign/call", []byte("f() = 1")},
	{"assign/call-in-list", []byte("a, f() = 1, 2")},
	{"assign/method", []byte("a:b = 1")},
	{"assign/missing-eq", []byte("a")},
	{"assign/missing-eq-field", []byte("a.b")},
	{"assign/paren-call", []byte("(f())")},
	{"assign/op", []byte("a, b + 1 = 1")},
	{"assign/vararg", []byte("... = 1")},
	{"assign/literal", []byte("1 = 2")},
	{"assign/string", []byte(`"a" = 1`)},
	{"assign/no-rhs", []byte("a =")},

	// Statements.
	{"stmt/semicolons", []byte("x = 1; y = 2; do end;")},
	{"stmt/empty", []byte(";")},
	{"stmt/double-semicolon", []byte("x = 1;;")},
	{"stmt/return-semicolon", []byte("return;")},
	{"stmt/return-after", []byte("return 1 x = 2")},
	{"stmt/return-values", []byte("return 1, f(), ...")},
	{"stmt/return-in-block", []byte("do return end x = 1")},
	{"stmt/break-not-last", []byte("while true do break; x = 1 end")},
	{"stmt/break-semicolon", []byte("while true do break; end")},
	{"stmt/break-outside", []byte("break")},
	{"stmt/break-in-if", []byte("if x then break end")},
	{"stmt/break-in-function", []byte("while true do local f = function() break end end")},
	{"stmt/break-in-repeat", []byte("repeat break until x")},
	{"stmt/break-nested", []byte("for i = 1, 2 do do do break end end end")},
	{"stmt/if", []byte("if a then b() elseif c then d() elseif e then else f() end")},
	{"stmt/if-no-then", []byte("if a b() end")},
	{"stmt/if-unclosed", []byte("if a then\nb()\n")},
	{"stmt/while", []byte("while a do b() end")},
	{"stmt/while-unclosed", []byte("while a do\nb()\n")},
	{"stmt/repeat", []byte("repeat local x = 1 until x == y")},
	{"stmt/repeat-unclosed", []byte("repeat\nb()\n")},
	{"stmt/for-num", []byte("for i = a, b, c do x = i end")},
	{"stmt/for-num-no-limit", []byte("for i = a do end")},
	{"stmt/for-in", []byte("for k, v in pairs(t) do x = k end")},
	{"stmt/for-bad", []byte("for i do end")},
	{"stmt/for-unclosed", []byte("for i = 1, 2 do\nx()\n")},
	{"stmt/do-unclosed", []byte("do\nx()\n")},
	{"stmt/function-names", []byte("function a.b.c:d() return self end")},
	{"stmt/function-local-name", []byte("local a = {} function a.b() end")},
	{"stmt/function-unclosed", []byte("function f()\nx()\n")},
	{"stmt/function-bad-name", []byte("function a:b.c() end")},
	{"stmt/function-no-name", []byte("function () end")},
	{"stmt/local-function-field", []byte("local function a.b() end")},
	{"stmt/params-bad", []byte("function f(a, 1) end")},
	{"stmt/params-after-dots", []byte("function f(..., a) end")},
	{"stmt/params-trailing-comma", []byte("function f(a,) end")},
	{"stmt/eof-expected", []byte("x = 1 end")},
	{"stmt/else-outside", []byte("else")},
	{"stmt/until-outside", []byte("until x")},
	{"stmt/expression", []byte("1 + 2")},
	{"stmt/unclosed-same-line", []byte("do x = 1")},

	// Goto and labels.
	{"goto/forward", []byte("goto l x = 1 ::l::")},
	{"goto/backward", []byte("::l:: x = 1 goto l")},
	{"goto/continue", []byte("for i = 1, 3 do if i then goto c end local y = i ::c:: end")},
	{"goto/continue-repeat", []byte("repeat goto c local y ::c:: until y")},
	{"goto/into-scope", []byte("goto l local x ::l:: print(x)")},
	{"goto/into-scope-nested", []byte("do goto l end local z ::l:: print(z)")},
	{"goto/trailing-label", []byte("do goto l local x ::l:: end")},
	{"goto/trailing-labels", []byte("do goto l local x ::l:: ::m:: end")},
	{"goto/label-then-semicolon", []byte("do goto l local x ::l:: ; end")},
	{"goto/undefined", []byte("goto nowhere")},
	{"goto/undefined-line", []byte("x = 1\n\ngoto\nnowhere\n")},
	{"goto/into-block", []byte("goto l do ::l:: end")},
	{"goto/out-of-block", []byte("do goto l end ::l::")},
	{"goto/into-function", []byte("goto l local f = function() ::l:: end")},
	{"goto/out-of-function", []byte("::l:: local f = function() goto l end")},
	{"goto/duplicate", []byte("::a:: ::a::")},
	{"goto/duplicate-nested", []byte("::a:: do ::a:: end")},
	{"goto/duplicate-after-block", []byte("do ::a:: end ::a::")},
	{"goto/same-name-functions", []byte("::a:: local f = function() ::a:: end")},
	{"goto/label-bad", []byte(":: 1 ::")},
	{"goto/label-unclosed", []byte("::a")},
	{"goto/backward-from-nested", []byte("::top:: do goto top end")},
	{"goto/multiple-forward", []byte("goto l goto l do goto l end ::l::")},
	{"goto/first-error-wins", []byte("goto a goto b local x ::b:: ::a::")},
	{"goto/loop-break-mix", []byte("while x do if y then break end goto c ::c:: end")},

	// A goto after a block closing upvalues gets the line of the block end.
	{"uclo/goto", []byte("do\nlocal x\nf = function() return x end\nend\n\ngoto nowhere")},
	{"uclo/nested-do", []byte("do do\nlocal x\nf = function() return x end\nend" +
		" end\n\ngoto nowhere")},
	{"uclo/after-empty-do", []byte("do local x f = function() return x end\nend" +
		" do end\n\ngoto nowhere")},
	{"uclo/after-while", []byte("while a do local x f = function() return" +
		" x end\nend\n\ngoto nowhere")},
	{"uclo/after-statement", []byte("do local x f = function() return x end\nend" +
		" y = 1\n\ngoto nowhere")},
	{"uclo/after-label", []byte("do local x f = function() return x end\nend" +
		" ::l::\n\ngoto nowhere")},
	{"uclo/break", []byte("do local x f = function() return x end\nend\n\nbreak")},
	{"uclo/scope", []byte("do local x f = function() return x end\nend\ngoto" +
		" l local y ::l:: print(y)")},
	{"uclo/second-goto", []byte("do local x f = function() return x end\nend\ngoto" +
		" a\ngoto b ::a::")},
	{"uclo/no-capture", []byte("do local x f = function() return y end\nend\n\ngoto nowhere")},
	{"uclo/function-body", []byte("do local x f = function() return x end\nend" +
		" local function g()\ngoto nowhere end")},

	// Scoping, checked against GGET/GSET of LuaJIT.
	{"scope/local-self-ref", []byte("local x = x")},
	{"scope/local-function-recursive", []byte("local function f() return f end")},
	{"scope/local-var-function", []byte("local f = function() return f end")},
	{"scope/local-multi", []byte("local a, b = b, a")},
	{"scope/shadow", []byte("local print = print print(x)")},
	{"scope/block", []byte("do local x = 1 end return x")},
	{"scope/repeat-until", []byte("repeat local done = f() until done return done")},
	{"scope/for-var", []byte("for i = i, i do x = i end return i")},
	{"scope/for-in-vars", []byte("for k, v in k, v do y = k + v end return k, v")},
	{"scope/upvalue", []byte("local a = 1 local function f() return function()" +
		" return a + b end end")},
	{"scope/params", []byte("function f(a, b) return a, b, c end")},
	{"scope/method-self", []byte("function t:m() return self, x end")},
	{"scope/function-self", []byte("function t.m() return self end")},
	{"scope/global-function", []byte("function f() end f()")},
	{"scope/local-after-use", []byte("print(x) local x = 1 print(x)")},
	{"scope/if-branches", []byte("if a then local x = 1 else x = 2 end")},
	{"scope/while-cond", []byte("local x while x do local y end return y")},
	{"scope/nested-function-params", []byte("local function f(x) return function(y) return" +
		" x, y, z end end")},
	{"scope/assign-order", []byte("a, b, c = c, b, a")},
	{"scope/field-write", []byte("a.b.c = d.e.f")},
	{"scope/index-write", []byte("a[b][c] = d[e]")},
	{"scope/method-call", []byte("a.b:c(d)")},
	{"scope/require", []byte("local log = require('log') log.info(x)")},
	{"scope/goto-name", []byte("local goto = 1 goto = goto + 1")},

	// Positions of multi-line constructs.
	{"lines/field-newline", []byte("x = a\n.b\n.c")},
	{"lines/binop-newline", []byte("x = a\n+\nb")},
	{"lines/call-args", []byte("f(\na,\nb\n)")},
	{"lines/long-string-arg", []byte("f([[\n\n]], a)")},
	{"lines/table", []byte("x = {\na,\nb = c,\n[d] = e,\n}")},
}

// limitSnippets build sources around the limits of LuaJIT.
func limitSnippets() []testCase {
	locals := func(n int) []byte {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "local v%d\n", i)
		}

		return []byte(b.String())
	}

	upvals := func(n int) []byte {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "local u%d\n", i)
		}

		b.WriteString("local function f()\nreturn ")

		for i := range n {
			fmt.Fprintf(&b, "u%d, ", i)
		}

		b.WriteString("x\nend")

		return []byte(b.String())
	}

	nested := func(open, mid, closing string, n int) []byte {
		return []byte(strings.Repeat(open, n) + mid + strings.Repeat(closing, n))
	}

	targets := func(n int) []byte {
		return []byte(strings.Repeat("a, ", n-1) + "a = 1")
	}

	return []testCase{
		{"limit/locals-199", locals(199)},
		{"limit/locals-200", locals(200)},
		{"limit/locals-201", locals(201)},
		{"limit/locals-for", []byte(string(locals(196)) + "for i = 1, 2 do end")},
		{"limit/locals-for-over", []byte(string(locals(197)) + "for i = 1, 2 do end")},
		{"limit/locals-function", []byte("function f()\n" + string(locals(200)) + "end")},
		{"limit/upvalues-60", upvals(60)},
		{"limit/upvalues-61", upvals(61)},
		{"limit/parens-197", nested("(", "x", ")", 197)},
		{"limit/parens-198", nested("(", "x", ")", 198)},
		{"limit/parens-199", nested("(", "x", ")", 199)},
		{"limit/blocks-198", nested("do ", "", " end", 198)},
		{"limit/blocks-199", nested("do ", "", " end", 199)},
		{"limit/unary-197", []byte("x = " + strings.Repeat("-", 197) + "y")},
		{"limit/unary-198", []byte("x = " + strings.Repeat("-", 198) + "y")},
		{"limit/tables-199", []byte("x = " + string(nested("{", "", "}", 199)))},
		{"limit/tables-200", []byte("x = " + string(nested("{", "", "}", 200)))},
		{"limit/functions", nested("function f() ", "", " end ", 100)},
		{"limit/labels", []byte(strings.Repeat("::l", 1) + "0:: " + labels(250))},
		{"limit/targets-199", targets(199)},
		{"limit/targets-200", targets(200)},
		{"limit/targets-nested", []byte(string(nested("do ", "", "", 5)) + string(targets(195)) +
			strings.Repeat(" end", 5))},
	}
}

func labels(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "::l%d:: ", i)
	}

	return b.String()
}

func TestSnippets(t *testing.T) {
	checkSuite(t, "snippets", slices.Concat(snippets, limitSnippets()), false)
}
