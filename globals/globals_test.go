package globals_test

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bigbes/go-luascope/globals"
	"github.com/bigbes/go-luascope/parser"
)

// The corpus and the LuaJIT results are shared with the parser package.
var luajitTestdata = filepath.Join("..", "parser", "testdata")

func readArchive(t *testing.T, suite string) map[string][]byte {
	t.Helper()

	f, err := os.Open(filepath.Join(luajitTestdata, "corpus", suite+".tar.gz"))
	require.NoError(t, err)

	defer f.Close()

	zr, err := gzip.NewReader(f)
	require.NoError(t, err)

	files := map[string][]byte{}
	tr := tar.NewReader(zr)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files
		}

		require.NoError(t, err)

		if hdr.Typeflag == tar.TypeReg {
			files[suite+"/"+hdr.Name], err = io.ReadAll(tr)
			require.NoError(t, err)
		}
	}
}

// readCounts reads the number of GGET and GSET instructions per name from
// a golden file: "GGET name line..." and "GSET name count" lines.
func readCounts(t *testing.T, suite string) map[string]map[string]int {
	t.Helper()

	f, err := os.Open(filepath.Join(luajitTestdata, "golden", suite+".golden.gz"))
	require.NoError(t, err)

	defer f.Close()

	zr, err := gzip.NewReader(f)
	require.NoError(t, err)

	counts := map[string]map[string]int{}

	var file string

	sc := bufio.NewScanner(zr)
	sc.Buffer(nil, 1<<20)

	for sc.Scan() {
		line := sc.Text()

		switch fields := strings.Fields(line); {
		case strings.HasPrefix(line, "== "):
			file = line[3:]
		case line == "ok":
			counts[file] = map[string]int{}
		case fields[0] == "GGET":
			counts[file]["GGET "+fields[1]] += len(fields) - 2
		case fields[0] == "GSET":
			var n int

			_, err := fmt.Sscan(fields[2], &n)
			require.NoError(t, err)

			counts[file]["GSET "+fields[1]] += n
		}
	}

	require.NoError(t, sc.Err())

	return counts
}

// escapeName escapes a name like the golden files do.
func escapeName(s string) string {
	var b strings.Builder

	for i := range len(s) {
		if c := s[i]; c < 32 || c == 127 || c == '\\' || c >= 128 {
			fmt.Fprintf(&b, "\\%03d", c)
		} else {
			b.WriteByte(c)
		}
	}

	return b.String()
}

// TestAnalyzeCorpus checks that the accesses match the GGET and GSET
// instructions LuaJIT emits for the corpus: a write of a global name is a
// GSET, any other access is a GGET of the root name.
func TestAnalyzeCorpus(t *testing.T) {
	for _, suite := range []string{"lua5.1-tests", "luajit-tests", "tarantool", "vshard", "crud"} {
		t.Run(suite, func(t *testing.T) {
			files, golden := readArchive(t, suite), readCounts(t, suite)
			require.NotEmpty(t, golden)

			names := make([]string, 0, len(golden))
			for name := range golden {
				names = append(names, name)
			}

			slices.Sort(names)

			for _, name := range names {
				chunk, err := parser.Parse(files[name], "m")
				require.NoError(t, err, name)

				got := map[string]int{}

				for _, a := range globals.Analyze(chunk).Accesses {
					op := "GGET "
					if a.Write && len(a.Path) == 1 {
						op = "GSET "
					}

					got[op+escapeName(a.Path[0].Name)]++
				}

				require.Equal(t, golden[name], got, name)
			}
		})
	}
}

func TestAnalyzeRequires(t *testing.T) {
	tests := []struct {
		src      string
		requires []string // Module names, "?" for a non-constant one.
		values   int      // Uses of require as a value.
	}{
		{"require('log')", []string{"log"}, 0},
		{"require 'log' require [[json]]", []string{"log", "json"}, 0},
		{"require(('cr') .. 'ud')", []string{"crud"}, 0},
		{"require(name)", []string{"?"}, 0},
		{"require()", []string{"?"}, 0},
		{"pcall(require, 'log')", []string{"log"}, 0},
		{"pcall((require), 'log', 'ignored')", []string{"log"}, 0},
		{"pcall(require, name)", []string{"?"}, 0},
		{"pcall(require)", []string{"?"}, 0},
		{"xpcall(require, handler, 'log')", []string{"log"}, 0},
		{"xpcall(require, 'log')", []string{"?"}, 0},
		{"local pcall = pcall pcall(require, 'log')", nil, 1},
		{"local require = require require('log')", nil, 1},
		{"local r = require", nil, 1},
		{"f(require, 'log')", nil, 1},
		{"pcall(f, require, 'log')", nil, 1},
		{"require.x = 1", nil, 0},
	}

	for _, tt := range tests {
		chunk, err := parser.Parse([]byte(tt.src), "test")
		require.NoError(t, err)

		usage := globals.Analyze(chunk)

		var got []string

		for _, r := range usage.Requires {
			if r.Dynamic {
				got = append(got, "?")
			} else {
				got = append(got, r.Module)
			}
		}

		require.Equal(t, tt.requires, got, tt.src)
		require.Len(t, usage.RequireValues, tt.values, tt.src)
	}
}
