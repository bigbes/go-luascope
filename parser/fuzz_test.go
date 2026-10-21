package parser

import (
	"errors"
	"testing"

	"github.com/bigbes/go-luascope/ast"
)

// FuzzParse checks that the parser never panics, reports positioned
// errors and produces a tree that can be walked.
func FuzzParse(f *testing.F) {
	for _, c := range snippets {
		f.Add(c.src)
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		chunk, err := Parse(src, "fuzz")
		if err != nil {
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("unexpected error type %T: %v", err, err)
			}

			if se.Line < 0 || se.Msg == "" {
				t.Fatalf("bad error %#v", se)
			}

			return
		}

		ast.Inspect(chunk.Block, func(n ast.Node) bool {
			if n.Pos().Line < 1 {
				t.Fatalf("bad position of %T: %v", n, n.Pos())
			}

			return true
		})
	})
}
