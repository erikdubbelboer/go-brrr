package encoder

import (
	"os"
	"testing"
)

var (
	literalCostBoolSink bool
	literalCostUintSink uint
)

const (
	splitN    = 128 << 10
	splitHTML = "../../testdata/gh_172KB.html"
	splitJS   = "../../testdata/reactcore_187KB.js"
)

func literalCostRealData(tb testing.TB, path string) []byte {
	tb.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	if len(data) < splitN {
		tb.Fatalf("%s holds %d bytes, the benchmark name promises %d", path, len(data), splitN)
	}
	return data[:splitN]
}

func benchmarkIsMostlyUTF8(b *testing.B, path string) {
	data := literalCostRealData(b, path)
	b.ReportAllocs()
	b.SetBytes(int64(splitN))
	for range b.N {
		literalCostBoolSink = isMostlyUTF8(data, 0, uint(splitN-1), uint(splitN))
	}
}

func benchmarkDecideMultiByteStatsLevel(b *testing.B, path string) {
	data := literalCostRealData(b, path)
	b.ReportAllocs()
	b.SetBytes(int64(splitN))
	for range b.N {
		literalCostUintSink = decideMultiByteStatsLevel(data, 0, uint(splitN), uint(splitN-1))
	}
}

func benchmarkEstimateUTF8Only(b *testing.B, path string) {
	data := literalCostRealData(b, path)
	histogram, cost := literalCostBuffers(splitN)
	b.ReportAllocs()
	b.SetBytes(int64(splitN))
	for range b.N {
		estimateBitCostsForLiteralsUTF8(data, 0, uint(splitN), uint(splitN-1), histogram, cost)
	}
	literalCostSink = cost[0]
}

func benchmarkEstimateDispatch(b *testing.B, path string) {
	data := literalCostRealData(b, path)
	histogram, cost := literalCostBuffers(splitN)
	b.ReportAllocs()
	b.SetBytes(int64(splitN))
	for range b.N {
		estimateBitCostsForLiterals(data, 0, uint(splitN), uint(splitN-1), histogram, cost)
	}
	literalCostSink = cost[0]
}

func BenchmarkIsMostlyUTF8Html128KiB(b *testing.B) { benchmarkIsMostlyUTF8(b, splitHTML) }
func BenchmarkIsMostlyUTF8Js128KiB(b *testing.B)   { benchmarkIsMostlyUTF8(b, splitJS) }
func BenchmarkDecideMultiByteStatsLevelHtml128KiB(b *testing.B) {
	benchmarkDecideMultiByteStatsLevel(b, splitHTML)
}
func BenchmarkDecideMultiByteStatsLevelJs128KiB(b *testing.B) {
	benchmarkDecideMultiByteStatsLevel(b, splitJS)
}
func BenchmarkEstimateBitCostsForLiteralsUTF8OnlyHtml128KiB(b *testing.B) {
	benchmarkEstimateUTF8Only(b, splitHTML)
}
func BenchmarkEstimateBitCostsForLiteralsUTF8OnlyJs128KiB(b *testing.B) {
	benchmarkEstimateUTF8Only(b, splitJS)
}
func BenchmarkEstimateBitCostsForLiteralsDispatchHtml128KiB(b *testing.B) {
	benchmarkEstimateDispatch(b, splitHTML)
}
func BenchmarkEstimateBitCostsForLiteralsDispatchJs128KiB(b *testing.B) {
	benchmarkEstimateDispatch(b, splitJS)
}
