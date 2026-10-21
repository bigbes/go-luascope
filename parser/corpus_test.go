package parser

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// corpusSuites are the archives in testdata/corpus, see SOURCES.md there.
var corpusSuites = []string{"lua5.1-tests", "luajit-tests", "tarantool", "vshard", "crud"}

// loadCorpus reads the Lua sources of an archive.
func loadCorpus(t *testing.T, suite string) []testCase {
	t.Helper()

	f, err := os.Open(filepath.Join("testdata", "corpus", suite+".tar.gz"))
	require.NoError(t, err)

	defer f.Close()

	zr, err := gzip.NewReader(f)
	require.NoError(t, err)

	var cases []testCase

	tr := tar.NewReader(zr)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		if hdr.Typeflag != tar.TypeReg ||
			!strings.HasSuffix(hdr.Name, ".lua") && !strings.HasSuffix(hdr.Name, ".t") {

			continue
		}

		src, err := io.ReadAll(tr)
		require.NoError(t, err)

		cases = append(cases, testCase{name: suite + "/" + hdr.Name, src: src})
	}

	require.NotEmpty(t, cases)

	return cases
}

func TestCorpus(t *testing.T) {
	for _, suite := range corpusSuites {
		t.Run(suite, func(t *testing.T) {
			checkSuite(t, suite, loadCorpus(t, suite), false)
		})
	}
}

// insertions are fragments inserted by mutations. They target the
// lexer and parser rules that ordinary code rarely exercises.
var insertions = []string{
	"(", ")", "[", "]", "{", "}", "=", "==", "~=", ".", "..", "...", ",", ";",
	":", "::", "\"", "'", "\\", "-", "--", "--[[", "[[", "]]", "[=[", "]=]",
	"\n", "\r", " ", "#", "~", "0", "0x", "1LL", "0b1", "1e", "1i", "L", "u",
	"\x00", "\x1b", "\x80", "\\u{", "\\z", "\\x", "\\9", "end", "local", "function",
	"return", "break", "goto x", "::x::", "goto", "if", "then", "else", "do",
	"for", "in", "until", "repeat", "while", "nil", "not", "and", "or",
	"self", "x", "_G", "local x", "local function x() end",
}

// mutate applies one random edit to a source.
func mutate(r *rand.Rand, src []byte) []byte {
	pos := r.IntN(len(src) + 1)
	out := make([]byte, 0, len(src)+32)

	switch r.IntN(4) {
	case 0: // Truncate.
		return append(out, src[:pos]...)
	case 1: // Delete a few bytes.
		end := min(len(src), pos+1+r.IntN(8))
		return append(append(out, src[:pos]...), src[end:]...)
	case 2: // Insert a fragment.
		frag := insertions[r.IntN(len(insertions))]
		return append(append(append(out, src[:pos]...), frag...), src[pos:]...)
	default: // Replace a byte with a fragment.
		end := min(len(src), pos+1)
		frag := insertions[r.IntN(len(insertions))]

		return append(append(append(out, src[:pos]...), frag...), src[end:]...)
	}
}

// mutationsPerFile is the number of mutated copies of each corpus file.
const mutationsPerFile = 4

// TestMutations compares error handling with LuaJIT on randomly edited
// corpus files. The edits are deterministic, so the golden file is stable.
func TestMutations(t *testing.T) {
	checkSuite(t, "mutations", mutationCases(t), true)
}

// mutationCases returns the randomly edited corpus files.
func mutationCases(t *testing.T) []testCase {
	t.Helper()

	var cases []testCase

	for _, suite := range corpusSuites {
		for _, c := range loadCorpus(t, suite) {
			seed := uint64(len(cases))
			r := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15)) //nolint:gosec // Not for security.

			for i := range mutationsPerFile {
				cases = append(cases, testCase{
					name: fmt.Sprintf("%s#%d", c.name, i),
					src:  mutate(r, c.src),
				})
			}
		}
	}

	return cases
}
