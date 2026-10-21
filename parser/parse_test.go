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

	_, err := parser.Parse(strings.NewReader("x = 1 +"), "reader.lua")

	var se *parser.SyntaxError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "reader.lua", se.Chunk)
	assert.Equal(t, "reader.lua:1: unexpected symbol near '<eof>'", err.Error())
}

func TestParseReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("connection reset")

	_, err := parser.Parse(&errReader{data: []byte("x = 1"), err: readErr}, "reader.lua")
	require.Error(t, err)

	var re *parser.ReadError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "reader.lua", re.Chunk)
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

func ExampleParse() {
	chunk, err := parser.Parse(strings.NewReader("x = 1"), "example")
	if err != nil {
		panic(err)
	}

	ast.Inspect(chunk.Block, func(node ast.Node) bool {
		if assign, ok := node.(*ast.Assign); ok {
			fmt.Println("an assignment at", assign.Pos())
		}

		return true
	})

	_, err = parser.Parse(strings.NewReader("x = 1 +"), "example")
	fmt.Println(err)

	// Output:
	// an assignment at 1:1
	// example:1: unexpected symbol near '<eof>'
}
