package parser

import (
	"math"
	"testing"
)

// acceptedCorpus returns the sources of the corpus that LuaJIT accepts,
// 6.2 MB of Lua in 961 files.
func acceptedCorpus(b *testing.B) []testCase {
	b.Helper()

	var cases []testCase

	for _, suite := range corpusSuites {
		for _, c := range loadCorpus(b, suite) {
			if _, err := ParseBytes(c.src, "=m"); err == nil {
				cases = append(cases, c)
			}
		}
	}

	return cases
}

// benchParse parses the sources once per iteration and reports the
// throughput in bytes of source.
func benchParse(b *testing.B, cases []testCase) {
	b.Helper()

	size := 0
	for _, c := range cases {
		size += len(c.src)
	}

	b.SetBytes(int64(size))
	b.ReportAllocs()

	for b.Loop() {
		for _, c := range cases {
			if _, err := ParseBytes(c.src, "=m"); err != nil {
				b.Fatal(c.name, err)
			}
		}
	}
}

// BenchmarkParseCorpus parses the corpus, the throughput the CHANGELOG
// gives for the parser.
func BenchmarkParseCorpus(b *testing.B) {
	benchParse(b, acceptedCorpus(b))
}

// BenchmarkParseFileSize parses the corpus by the size of a file, one call
// per file, which is how a tool checking files one by one uses the parser:
// a fixed cost per call shows up on the small files.
func BenchmarkParseFileSize(b *testing.B) {
	cases := acceptedCorpus(b)

	for _, size := range []struct {
		name     string
		min, max int
	}{
		{"under-5KB", 0, 5 << 10},
		{"5-50KB", 5 << 10, 50 << 10},
		{"over-50KB", 50 << 10, math.MaxInt},
	} {
		var sized []testCase

		for _, c := range cases {
			if len(c.src) >= size.min && len(c.src) < size.max {
				sized = append(sized, c)
			}
		}

		b.Run(size.name, func(b *testing.B) {
			benchParse(b, sized)
			b.ReportMetric(float64(len(sized)), "files")
		})
	}
}
