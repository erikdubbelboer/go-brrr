package encoder

import "testing"

// literalCostSink keeps the benchmarked calls from being elided.
// literalCostBenchBytes is the payload size both literal-cost benchmarks use.
const literalCostBenchBytes = 256 << 10

var literalCostSink float32

// The *Before functions recompute fastLog2 of the window count on every
// byte, as both paths did before the value was cached.
func estimateBitCostsForLiteralsRawBefore(data []byte, pos, length, mask uint, histogram []uint, cost []float32) {
	windowHalf := uint(2000)
	inWindow := min(windowHalf, length)

	clear(histogram[:256])

	// Bootstrap histogram.
	for i := range inWindow {
		histogram[data[(pos+i)&mask]]++
	}

	// Compute bit costs with sliding window.
	for i := range length {
		if i >= windowHalf {
			histogram[data[(pos+i-windowHalf)&mask]]--
			inWindow--
		}
		if i+windowHalf < length {
			histogram[data[(pos+i+windowHalf)&mask]]++
			inWindow++
		}
		histo := histogram[data[(pos+i)&mask]]
		if histo == 0 {
			histo = 1
		}
		litCost := fastLog2(int(inWindow)) - fastLog2(int(histo))
		litCost += 0.029
		if litCost < 1.0 {
			litCost = litCost*0.5 + 0.5
		}
		cost[i] = float32(litCost)
	}
}

func estimateBitCostsForLiteralsUTF8Before(data []byte, pos, length, mask uint, histogram []uint, cost []float32) {
	maxUTF8 := decideMultiByteStatsLevel(data, pos, length, mask)
	windowHalf := uint(495)
	inWindow := min(windowHalf, length)
	var inWindowUTF8 [3]uint

	// Clear histograms: 3 * 256 entries.
	clear(histogram[:3*256])

	// Bootstrap histograms from the initial window.
	lastC := uint(0)
	utf8Pos := uint(0)
	for i := range inWindow {
		c := uint(data[(pos+i)&mask])
		histogram[256*utf8Pos+c]++
		inWindowUTF8[utf8Pos]++
		utf8Pos = utf8Position(lastC, c, maxUTF8)
		lastC = c
	}

	// Compute bit costs with sliding window.
	for i := range length {
		if i >= windowHalf {
			// Remove a byte in the past.
			var c, lc uint
			if i >= windowHalf+1 {
				c = uint(data[(pos+i-windowHalf-1)&mask])
			}
			if i >= windowHalf+2 {
				lc = uint(data[(pos+i-windowHalf-2)&mask])
			}
			utf8Pos2 := utf8Position(lc, c, maxUTF8)
			histogram[256*utf8Pos2+uint(data[(pos+i-windowHalf)&mask])]--
			inWindowUTF8[utf8Pos2]--
		}
		if i+windowHalf < length {
			// Add a byte in the future.
			c := uint(data[(pos+i+windowHalf-1)&mask])
			lc := uint(data[(pos+i+windowHalf-2)&mask])
			utf8Pos2 := utf8Position(lc, c, maxUTF8)
			histogram[256*utf8Pos2+uint(data[(pos+i+windowHalf)&mask])]++
			inWindowUTF8[utf8Pos2]++
		}

		var c uint
		if i >= 1 {
			c = uint(data[(pos+i-1)&mask])
		}
		var lc uint
		if i >= 2 {
			lc = uint(data[(pos+i-2)&mask])
		}
		curUTF8Pos := utf8Position(lc, c, maxUTF8)
		maskedPos := (pos + i) & mask
		histo := histogram[256*curUTF8Pos+uint(data[maskedPos])]
		if histo == 0 {
			histo = 1
		}
		litCost := fastLog2(int(inWindowUTF8[curUTF8Pos])) - fastLog2(int(histo))
		litCost += 0.02905
		if litCost < 1.0 {
			litCost = litCost*0.5 + 0.5
		}
		// Make the first bytes more expensive to account for the statistical
		// anomaly at the beginning of the data.
		const prologueLength = 2000
		const multiplier = 0.35 / prologueLength
		if i < prologueLength {
			litCost += 0.35 + multiplier*float64(i)
		}
		cost[i] = float32(litCost)
	}
}

func literalCostData(tb testing.TB, kind string, n int) []byte {
	tb.Helper()
	data := make([]byte, n)
	euro := []byte{0xE2, 0x82, 0xAC}
	switch kind {
	case "raw":
		for i := range data {
			data[i] = byte(i * 251)
		}
	case "ascii":
		for i := range data {
			data[i] = byte(32 + (i*7)%90)
		}
	case "multibyte":
		for i := range data {
			data[i] = euro[i%3]
		}
	case "mixed":
		for i := 0; i < n; {
			for j := 0; j < 4 && i < n; j++ {
				data[i] = byte(97 + (i*11)%26)
				i++
			}
			for j := 0; j < 3 && i < n; j++ {
				data[i] = euro[j]
				i++
			}
		}
	default:
		tb.Fatalf("unknown fixture kind %q", kind)
	}
	return data
}

func literalCostBuffers(n int) (histogram []uint, cost []float32) {
	return make([]uint, 3*256), make([]float32, n)
}

func TestCachedLog2AgreesWithRecomputingItPerByteInEveryWindowPhase(t *testing.T) {
	impls := []struct {
		name          string
		before, after func(data []byte, pos, length, mask uint, histogram []uint, cost []float32)
	}{
		{"raw", estimateBitCostsForLiteralsRawBefore, estimateBitCostsForLiteralsRaw},
		{"utf8", estimateBitCostsForLiteralsUTF8Before, estimateBitCostsForLiteralsUTF8},
	}

	for _, kind := range []string{"raw", "ascii", "multibyte", "mixed"} {
		for _, length := range []int{0, 1, 2, 64, 494, 495, 496, 990, 1999, 2000, 2001, 4001, 8192} {
			const size = 1 << 14
			data := literalCostData(t, kind, size)
			mask := uint(size - 1)
			for _, pos := range []uint{0, mask - uint(length)/2} {
				for _, impl := range impls {
					wantHisto, wantCost := literalCostBuffers(length)
					gotHisto, gotCost := literalCostBuffers(length)
					impl.before(data, pos, uint(length), mask, wantHisto, wantCost)
					impl.after(data, pos, uint(length), mask, gotHisto, gotCost)

					for i := range wantCost {
						if gotCost[i] != wantCost[i] {
							t.Fatalf("%s/%s length=%d pos=%d: cost[%d] = %v, recomputing the "+
								"logarithm every byte gives %v; one stale window count reprices "+
								"every literal the Zopfli DP sees, and the C-reference tests stop "+
								"comparing bytes above quality 9, so nothing else catches it",
								impl.name, kind, length, pos, i, gotCost[i], wantCost[i])
						}
					}
					for i := range wantHisto {
						if gotHisto[i] != wantHisto[i] {
							t.Fatalf("%s/%s length=%d pos=%d: histogram[%d] = %d, want %d; caching "+
								"the logarithm must not disturb the sliding window it reads from",
								impl.name, kind, length, pos, i, gotHisto[i], wantHisto[i])
						}
					}
				}
			}
		}
	}
}

func benchmarkLiteralCost(b *testing.B, utf8 bool, kind string, n int) {
	data := literalCostData(b, kind, n)
	histogram, cost := literalCostBuffers(n)
	before, after := estimateBitCostsForLiteralsRawBefore, estimateBitCostsForLiteralsRaw
	if utf8 {
		before, after = estimateBitCostsForLiteralsUTF8Before, estimateBitCostsForLiteralsUTF8
	}
	b.Run("impl=before_recompute_log", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(n))
		for range b.N {
			before(data, 0, uint(n), uint(n-1), histogram, cost)
		}
		literalCostSink = cost[0]
	})
	b.Run("impl=after_cached_log", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(n))
		for range b.N {
			after(data, 0, uint(n), uint(n-1), histogram, cost)
		}
		literalCostSink = cost[0]
	})
}

func BenchmarkEstimateBitCostsForLiteralsRaw256KiB(b *testing.B) {
	benchmarkLiteralCost(b, false, "raw", literalCostBenchBytes)
}

func BenchmarkEstimateBitCostsForLiteralsRaw4KiB(b *testing.B) {
	benchmarkLiteralCost(b, false, "raw", 4<<10)
}

func BenchmarkEstimateBitCostsForLiteralsUTF8Ascii256KiB(b *testing.B) {
	benchmarkLiteralCost(b, true, "ascii", literalCostBenchBytes)
}

func BenchmarkEstimateBitCostsForLiteralsUTF8Ascii4KiB(b *testing.B) {
	benchmarkLiteralCost(b, true, "ascii", 4<<10)
}

func BenchmarkEstimateBitCostsForLiteralsUTF8MultiByte256KiB(b *testing.B) {
	benchmarkLiteralCost(b, true, "multibyte", literalCostBenchBytes)
}

func BenchmarkEstimateBitCostsForLiteralsUTF8MixedAsciiAndMultiByte256KiB(b *testing.B) {
	benchmarkLiteralCost(b, true, "mixed", literalCostBenchBytes)
}

func TestZeroHistogramCountCostsTheSameAsOneSoTheGuardInTheCostLoopsIsRedundant(t *testing.T) {
	if fastLog2(0) != fastLog2(1) {
		t.Fatalf("both cost loops now call fastLog2(histo) without the histo==0 guard, which is only equivalent while fastLog2(0)==fastLog2(1); got %v vs %v",
			fastLog2(0), fastLog2(1))
	}
}
