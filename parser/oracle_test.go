package parser

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bigbes/go-luascope/ast"
)

// The golden files hold the results of Tarantool's LuaJIT for every test
// source. To regenerate them, build the LuaJIT fork used by Tarantool
// (https://github.com/tarantool/luajit) and run:
//
//	go test ./parser -update -luajit=/path/to/build/src/luajit
var (
	update = flag.Bool("update", false, "regenerate golden files with -luajit")
	luajit = flag.String("luajit", "", "path to Tarantool's LuaJIT binary")
)

// testCase is a named test source.
type testCase struct {
	name string
	src  []byte
}

// escape prints control characters, backslashes and bytes 128-255 as
// \ddd, like testdata/oracle/oracle.lua does.
func escape(s string) string {
	var b strings.Builder

	for i := range len(s) {
		c := s[i]
		if c < 32 || c == 127 || c == '\\' || c >= 128 {
			fmt.Fprintf(&b, "\\%03d", c)
		} else {
			b.WriteByte(c)
		}
	}

	return b.String()
}

// globalOp is a GGET or GSET instruction.
type globalOp struct {
	op   string
	name string
	line int
}

// render returns the canonical result: "error <message>" or "ok" with one
// "GGET name line..." line per read name and one "GSET name count" line
// per written name. Hashed results keep only a hash of the names and the
// counts, to keep the golden file of the mutations small.
func render(err string, ops []globalOp, hashed bool) string {
	if err != "" {
		return "error " + err
	}

	gets, sets := map[string][]int{}, map[string]int{}

	for _, op := range ops {
		if op.op == "GGET" {
			gets[op.name] = append(gets[op.name], op.line)
		} else {
			sets[op.name]++
		}
	}

	lines := make([]string, 0, len(gets)+len(sets))

	for name, ls := range gets {
		slices.Sort(ls)

		s := make([]string, len(ls))
		for i, l := range ls {
			s[i] = strconv.Itoa(l)
		}

		if hashed {
			lines = append(lines, fmt.Sprintf("GGET %s %d", name, len(ls)))
		} else {
			lines = append(lines, "GGET "+name+" "+strings.Join(s, " "))
		}
	}

	for name, n := range sets {
		lines = append(lines, fmt.Sprintf("GSET %s %d", name, n))
	}

	slices.Sort(lines)

	body := strings.Join(append([]string{"ok"}, lines...), "\n")
	if hashed {
		sum := sha256.Sum256([]byte(body))
		return fmt.Sprintf("ok %x", sum[:8])
	}

	return body
}

// sameResult compares a result of LuaJIT with a result of the parser. The
// counts of global reads and writes must be equal. LuaJIT reports a read
// at the line where the compiler emits it, which can be after the name, so
// a read may be reported at the line of the name or later.
func sameResult(want, got string) bool {
	if want == got {
		return true
	}

	if !strings.HasPrefix(want, "ok\n") || !strings.HasPrefix(got, "ok\n") {
		return false
	}

	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	if len(wl) != len(gl) {
		return false
	}

	for i := range wl {
		if !sameOp(wl[i], gl[i]) {
			return false
		}
	}

	return true
}

func sameOp(want, got string) bool {
	wf, gf := strings.Fields(want), strings.Fields(got)
	if want == got {
		return true
	}

	if len(wf) != len(gf) || len(wf) < 3 || wf[0] != "GGET" || wf[1] != gf[1] {
		return false
	}

	for i := 2; i < len(wf); i++ {
		w, errW := strconv.Atoi(wf[i])
		g, errG := strconv.Atoi(gf[i])

		if errW != nil || errG != nil || g > w {
			return false
		}
	}

	return true
}

// globalOps returns the global reads and writes the way LuaJIT compiles
// them: a GSET for an assignment to a global name and a GGET for every
// other use of a global name.
func globalOps(chunk *ast.Chunk) []globalOp {
	var (
		ops   []globalOp
		visit func(ast.Node) bool
	)

	target := func(e ast.Expr) {
		if n, ok := e.(*ast.Name); ok {
			if n.Local == nil {
				ops = append(ops, globalOp{op: "GSET", name: escape(n.Name)})
			}

			return
		}

		ast.Inspect(e, visit)
	}

	visit = func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Name:
			if n.Local == nil {
				ops = append(ops, globalOp{op: "GGET", name: escape(n.Name), line: n.At.Line})
			}
		case *ast.Assign:
			for _, t := range n.Targets {
				target(t)
			}

			for _, v := range n.Values {
				ast.Inspect(v, visit)
			}

			return false
		case *ast.FunctionStmt:
			target(n.Target)
			ast.Inspect(n.Func, visit)

			return false
		}

		return true
	}

	ast.Inspect(chunk.Block, visit)

	return ops
}

// parseResult renders the result of Parse for a source.
func parseResult(src []byte, hashed bool) string {
	chunk, err := ParseBytes(src, "m")
	if err != nil {
		var se *SyntaxError
		if !errors.As(err, &se) {
			return "error " + escape(err.Error())
		}

		return "error " + escape(se.Detail())
	}

	return render("", globalOps(chunk), hashed)
}

// runOracle runs LuaJIT on the sources and renders its results.
func runOracle(t *testing.T, cases []testCase, hashed bool) map[string]string {
	t.Helper()

	dir := t.TempDir()
	files := make([]string, len(cases))
	byFile := make(map[string]string, len(cases))

	for i, c := range cases {
		files[i] = filepath.Join(dir, strconv.Itoa(i)+".lua")
		byFile[files[i]] = c.name
		require.NoError(t, os.WriteFile(files[i], c.src, 0o600))
	}

	script, err := filepath.Abs(filepath.Join("testdata", "oracle", "oracle.lua"))
	require.NoError(t, err)

	args := append([]string{script, filepath.Dir(*luajit)}, files...)
	out, err := exec.CommandContext(t.Context(), *luajit, args...).Output()
	require.NoError(t, err)

	return parseOracleOutput(t, out, byFile, hashed)
}

func parseOracleOutput(t *testing.T, out []byte, byFile map[string]string,
	hashed bool,
) map[string]string {
	t.Helper()

	results := map[string]string{}

	var (
		name string
		ops  []globalOp
		ok   bool
	)

	flush := func() {
		if ok {
			results[name] = render("", ops, hashed)
		}
	}

	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)

	for sc.Scan() {
		line := sc.Text()

		switch {
		case strings.HasPrefix(line, "== "):
			flush()
			name, ops, ok = byFile[line[3:]], nil, false
		case strings.HasPrefix(line, "error "):
			results[name] = line
		case line == "ok":
			ok = true
		default:
			f := strings.SplitN(line, " ", 3)
			require.Len(t, f, 3, "bad oracle line %q", line)
			n, err := strconv.Atoi(f[1])
			require.NoError(t, err)

			ops = append(ops, globalOp{op: f[0], name: f[2], line: n})
		}
	}

	flush()
	require.NoError(t, sc.Err())

	return results
}

func goldenPath(suite string) string {
	return filepath.Join("testdata", "golden", suite+".golden.gz")
}

func writeGolden(t *testing.T, suite string, cases []testCase, results map[string]string) {
	t.Helper()

	var buf bytes.Buffer

	// A zero ModTime keeps the file stable: a regeneration that changes no
	// result leaves no diff.
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	require.NoError(t, err)

	zw.ModTime = time.Time{}

	for _, c := range cases {
		fmt.Fprintf(zw, "== %s\n%s\n", c.name, results[c.name])
	}

	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(goldenPath(suite), buf.Bytes(), 0o600))
}

func readGolden(t *testing.T, suite string) map[string]string {
	t.Helper()

	data, err := os.ReadFile(goldenPath(suite))
	require.NoError(t, err)

	zr, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)

	results := map[string]string{}

	var name string

	sc := bufio.NewScanner(zr)
	sc.Buffer(nil, 1<<20)

	for sc.Scan() {
		if line, ok := strings.CutPrefix(sc.Text(), "== "); ok {
			name = line
			continue
		}

		if prev, ok := results[name]; ok {
			results[name] = prev + "\n" + sc.Text()
		} else {
			results[name] = sc.Text()
		}
	}

	require.NoError(t, sc.Err())

	return results
}

// reportMismatch reports a difference. For hashed results it shows the
// full results if LuaJIT is available.
func reportMismatch(t *testing.T, c testCase, want, got string, hashed bool) {
	t.Helper()

	if hashed && *luajit != "" {
		want = runOracle(t, []testCase{c}, false)[c.name]
		got = parseResult(c.src, false)
	}

	t.Errorf("%s:\n--- luajit\n%s\n--- parser\n%s\n--- source\n%q", c.name, want, got, c.src)
}

// checkSuite compares the parser with the golden results of LuaJIT. With
// -update it regenerates the golden file first.
func checkSuite(t *testing.T, suite string, cases []testCase, hashed bool) {
	t.Helper()

	if *update {
		if *luajit == "" {
			t.Fatal("-update requires -luajit")
		}

		writeGolden(t, suite, cases, runOracle(t, cases, hashed))
	}

	golden := readGolden(t, suite)
	require.Len(t, golden, len(cases), "golden file %s is stale, run with -update",
		goldenPath(suite))

	failed := 0

	for _, c := range cases {
		want, ok := golden[c.name]
		if !ok {
			t.Fatalf("%s: no golden result, run with -update", c.name)
		}

		if got := parseResult(c.src, hashed); !sameResult(want, got) {
			failed++
			if failed <= 10 {
				reportMismatch(t, c, want, got, hashed)
			}
		}
	}

	if failed > 0 {
		t.Errorf("%d of %d sources differ from LuaJIT", failed, len(cases))
	}
}
