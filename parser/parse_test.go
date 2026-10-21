package parser_test

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bigbes/go-luascope/ast"
	"github.com/bigbes/go-luascope/parser"
)

// errReader fails after the bytes it returns.
type errReader struct {
	data []byte
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}

	n := copy(p, r.data)
	r.data = r.data[n:]

	return n, nil
}

func TestParseReader(t *testing.T) {
	t.Parallel()

	const src = "local n = 1\nprint(n, x)\n"

	result, err := parser.Parse(strings.NewReader(src), "reader.lua")
	require.NoError(t, err)

	fromBytes, err := parser.ParseBytes([]byte(src), "reader.lua")
	require.NoError(t, err)

	var names, nameFromBytes []string

	ast.Inspect(result.Block, func(node ast.Node) bool {
		if name, ok := node.(*ast.Name); ok {
			names = append(names, name.Name)
		}

		return true
	})

	ast.Inspect(fromBytes.Block, func(node ast.Node) bool {
		if name, ok := node.(*ast.Name); ok {
			nameFromBytes = append(nameFromBytes, name.Name)
		}

		return true
	})

	assert.Equal(t, []string{"print", "n", "x"}, names)
	assert.Equal(t, nameFromBytes, names)
	assert.Equal(t, fromBytes.Comments, result.Comments)
}

func TestParseReaderSyntaxError(t *testing.T) {
	t.Parallel()

	_, err := parser.Parse(strings.NewReader("x = 1 +"), "@reader.lua")

	var se *parser.SyntaxError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "@reader.lua", se.Chunk)
	assert.Equal(t, "reader.lua:1: unexpected symbol near '<eof>'", err.Error())
}

func TestParseReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("connection reset")

	_, err := parser.Parse(&errReader{data: []byte("x = 1"), err: readErr}, "@reader.lua")
	require.Error(t, err)

	var re *parser.ReadError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "@reader.lua", re.Chunk)
	assert.Equal(t, readErr, re.Err)
	assert.ErrorIs(t, err, readErr)
	assert.Equal(t, "reader.lua: connection reset", err.Error())

	// A read error is not a syntax error of the chunk.
	var se *parser.SyntaxError
	assert.False(t, errors.As(err, &se))
}

// TestParseReadErrorNotTruncated checks that a reader which fails after
// returning valid Lua does not yield a parsed chunk.
func TestParseReadErrorNotTruncated(t *testing.T) {
	t.Parallel()

	readErr := io.ErrUnexpectedEOF
	src := []byte("x = 1\n")

	chunk, err := parser.Parse(&errReader{data: src, err: readErr}, "reader.lua")
	require.ErrorIs(t, err, readErr)
	require.Nil(t, chunk)
}

func TestParseBytesReaderAgreement(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"",
		"local a = 1\n",
		"\xef\xbb\xbfx = 1\n",
		"#!/usr/bin/env tarantool\nx = 1\n",
		"x = 'unfinished\n",
	} {
		fromReader, errReader := parser.Parse(strings.NewReader(src), "c.lua")
		fromBytes, errBytes := parser.ParseBytes([]byte(src), "c.lua")

		require.Equal(t, errBytes == nil, errReader == nil, "src %q", src)

		if errBytes != nil {
			assert.Equal(t, errBytes.Error(), errReader.Error(), "src %q", src)
			assert.Nil(t, fromReader)
			assert.Nil(t, fromBytes)

			continue
		}

		assert.Equal(t, fromBytes.Block, fromReader.Block, "src %q", src)
		assert.Equal(t, len(fromBytes.Comments), len(fromReader.Comments), "src %q", src)
	}
}

// TestChunkName checks the chunk name in error messages against LuaJIT:
// the expected names are the ones LuaJIT prints for a syntax error, by
// lj_debug_shortname, and for an error of its bytecode loader, by
// bcread_error.
func TestChunkName(t *testing.T) {
	t.Parallel()

	var (
		path59 = strings.Repeat("p", 50) + "/file.lua"
		path60 = strings.Repeat("p", 51) + "/file.lua"
		path70 = strings.Repeat("d", 61) + "/file.lua"
		cut    = func(s string) string { return "..." + s[len(s)-56:] }
		n59    = strings.Repeat("n", 59)
		s45    = strings.Repeat("s", 45)
		s48    = strings.Repeat("s", 48)
		q60    = strings.Repeat("q", 60)
		high   = "\xd0\xbc\xd0\xb8\xd1\x80" // Bytes 128-255 are not control characters.
	)

	tests := []struct {
		chunk    string
		syntax   string // The name in "name:1: unexpected symbol near '<eof>'".
		bytecode string // The name in "name: cannot load bytecode".
	}{
		// A file name.
		{"@file.lua", "file.lua", "file.lua"},
		{"@", "", ""},
		{"@" + path59, path59, path59},
		{"@" + path60, cut(path60), path60},
		{"@" + path70, cut(path70), path70},
		// A name to print as is.
		{"=shown", "shown", "shown"},
		{"=", "", ""},
		{"=" + n59, n59, n59},
		{"=" + n59 + "n", n59, n59 + "n"},
		// A name taken for the source of the chunk.
		{"plain", `[string "plain"]`, "plain"},
		{"", `[string ""]`, ""},
		{"x = 1 +", `[string "x = 1 +"]`, "x = 1 +"},
		{s48, `[string "` + s48 + `"]`, s48},
		{s48 + "s", `[string "` + s45 + `..."]`, s48 + "s"},
		{"first\nsecond", `[string "first..."]`, "first\nsecond"},
		{s48[1:] + "\n", `[string "` + s45 + `..."]`, s48[1:] + "\n"},
		{high, "[string \"" + high + "\"]", high},
		{"\x1bLJ", `[string "..."]`, "(binary)"},
		// LuaJIT takes the name as a C string.
		{"=a\x00b", "a", "a"},
		{"@a\x00b", "a", "a"},
		{"a\x00b", `[string "a"]`, "a"},
		{"@" + q60 + "\x00tail", cut(q60), q60},
	}

	readErr := errors.New("connection reset")

	for _, tt := range tests {
		_, err := parser.ParseBytes([]byte("x = 1 +"), tt.chunk)
		require.EqualError(t, err, tt.syntax+":1: unexpected symbol near '<eof>'",
			"chunk %q", tt.chunk)

		_, err = parser.ParseBytes([]byte("\x1bLJ"), tt.chunk)
		require.EqualError(t, err, tt.bytecode+": cannot load bytecode", "chunk %q", tt.chunk)

		// LuaJIT does not name the chunk when bytecode follows a header.
		_, err = parser.ParseBytes([]byte("#!lua\n\x1bLJ"), tt.chunk)
		require.EqualError(t, err, "cannot load malformed bytecode", "chunk %q", tt.chunk)

		_, err = parser.Parse(&errReader{err: readErr}, tt.chunk)
		require.EqualError(t, err, tt.syntax+": connection reset", "chunk %q", tt.chunk)
	}
}

// TestParseBytesCopiesSource checks that the tree does not refer to the
// slice given to ParseBytes: the caller may reuse it.
func TestParseBytesCopiesSource(t *testing.T) {
	t.Parallel()

	src := []byte("local name = 12 -- comment\nreturn name")

	chunk, err := parser.ParseBytes(src, "=test")
	require.NoError(t, err)

	for i := range src {
		src[i] = 'x'
	}

	local, ok := chunk.Block.Stmts[0].(*ast.LocalStmt)
	require.True(t, ok)

	number, ok := local.Values[0].(*ast.Number)
	require.True(t, ok)

	assert.Equal(t, "name", local.Names[0].Name)
	assert.Equal(t, "12", number.Raw)
	assert.Equal(t, " comment", chunk.Comments[0].Text)
}

func ExampleParse() {
	chunk, err := parser.Parse(strings.NewReader("x = 1"), "=example")
	if err != nil {
		panic(err)
	}

	ast.Inspect(chunk.Block, func(node ast.Node) bool {
		if assign, ok := node.(*ast.Assign); ok {
			fmt.Println("an assignment at", assign.Pos())
		}

		return true
	})

	_, err = parser.Parse(strings.NewReader("x = 1 +"), "=example")
	fmt.Println(err)

	// Output:
	// an assignment at 1:1
	// example:1: unexpected symbol near '<eof>'
}
