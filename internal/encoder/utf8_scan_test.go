package encoder

import (
	"errors"
	"fmt"
	"testing"
)

func isMostlyUTF8Before(data []byte, pos, mask, length uint, minFraction float64) bool {
	sizeUTF8 := uint(0)
	i := uint(0)
	for i < length {
		bytesRead, isUTF8 := parseAsUTF8(data, (pos+i)&mask, length-i, mask)
		i += bytesRead
		if isUTF8 {
			sizeUTF8 += bytesRead
		}
	}
	return float64(sizeUTF8) > minFraction*float64(length)
}

func decideMultiByteStatsLevelBefore(data []byte, pos, length, mask uint) uint {
	var counts [3]uint
	maxUTF8 := uint(1)
	lastC := uint(0)
	for i := range length {
		c := uint(data[(pos+i)&mask])
		counts[utf8Position(lastC, c, 2)]++
		lastC = c
	}
	if counts[2] < 500 {
		maxUTF8 = 1
	}
	if counts[1]+counts[2] < 25 {
		maxUTF8 = 0
	}
	return maxUTF8
}

func utf8ScanFixtures(tb testing.TB) map[string][]byte {
	tb.Helper()
	euro := []byte{0xE2, 0x82, 0xAC}
	umlaut := []byte{0xC3, 0xA4}
	emoji := []byte{0xF0, 0x9F, 0x98, 0x80}

	build := func(n int, f func(i int) byte) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = f(i)
		}
		return b
	}
	repeat := func(unit []byte, n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = unit[i%len(unit)]
		}
		return b
	}
	multiByteAt := func(n, count int, unit []byte) []byte {
		b := build(n, func(i int) byte { return byte(32 + i%90) })
		for k := range count {
			copy(b[k*len(unit)*3:], unit)
		}
		return b
	}

	return map[string][]byte{
		"ascii_printable":              build(4096, func(i int) byte { return byte(32 + (i*7)%90) }),
		"ascii_with_nuls":              build(4096, func(i int) byte { return byte((i * 37) % 128) }),
		"all_nuls":                     make([]byte, 4096),
		"high_bytes_only":              build(4096, func(i int) byte { return byte(128 + i%128) }),
		"euro_3byte":                   repeat(euro, 4096),
		"umlaut_2byte":                 repeat(umlaut, 4096),
		"emoji_4byte":                  repeat(emoji, 4096),
		"invalid_continuations":        build(4096, func(i int) byte { return byte(0x80 + i%16) }),
		"truncated_sequences":          build(4096, func(i int) byte { return []byte{0xE2, 0x82, 'a', 0xC3}[i%4] }),
		"multibyte_24_just_under":      multiByteAt(4096, 24, umlaut),
		"multibyte_25_at_the_edge":     multiByteAt(4096, 25, umlaut),
		"multibyte_26_just_over":       multiByteAt(4096, 26, umlaut),
		"ascii_then_multibyte":         append(build(2048, func(i int) byte { return 'a' }), repeat(euro, 2048)...),
		"ascii_then_bare_continuation": repeat([]byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 0x82, 0xA4}, 4096),
		"ascii_run_then_continuation": build(4096, func(i int) byte {
			if i%16 == 15 {
				return 0x9F
			}
			return byte('a' + i%26)
		}),
		"single_byte":      {'x'},
		"single_high_byte": {0xC3},
		"empty":            {},
	}
}

func TestIsMostlyUTF8MatchesPerPositionParsingForEveryFixtureLengthAndWrapOffset(t *testing.T) {
	var errs []error
	for name, data := range utf8ScanFixtures(t) {
		if len(data) == 0 {
			continue
		}
		mask := uint(len(data) - 1)
		if len(data)&(len(data)-1) != 0 {
			continue
		}
		for _, length := range []uint{1, 2, 7, 8, 9, 15, 16, 17, 31, 63, 64, 1000, uint(len(data))} {
			if length > uint(len(data)) {
				continue
			}
			for _, pos := range []uint{0, 1, 7, uint(len(data)) / 2, uint(len(data)) - 3} {
				want := isMostlyUTF8Before(data, pos, mask, length, minUTF8Ratio)
				got := isMostlyUTF8(data, pos, mask, length, minUTF8Ratio)
				if got != want {
					errs = append(errs, fmt.Errorf(
						"the eight-byte ASCII skip changed the UTF-8 verdict, which selects the whole cost model: fixture=%s pos=%d length=%d got=%v want=%v",
						name, pos, length, got, want))
				}
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
}

func TestDecideMultiByteStatsLevelMatchesCountingEveryPositionIncludingTheTwentyFiveByteThreshold(t *testing.T) {
	var errs []error
	for name, data := range utf8ScanFixtures(t) {
		if len(data) == 0 || len(data)&(len(data)-1) != 0 {
			continue
		}
		mask := uint(len(data) - 1)
		for _, length := range []uint{1, 7, 8, 9, 16, 64, 1000, uint(len(data))} {
			if length > uint(len(data)) {
				continue
			}
			for _, pos := range []uint{0, 1, 7, uint(len(data)) / 2, uint(len(data)) - 3} {
				want := decideMultiByteStatsLevelBefore(data, pos, length, mask)
				got := decideMultiByteStatsLevel(data, pos, length, mask)
				if got != want {
					errs = append(errs, fmt.Errorf(
						"the early exit changed the histogram count, which changes every literal cost: fixture=%s pos=%d length=%d got=%d want=%d",
						name, pos, length, got, want))
				}
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
}

func TestNonZeroASCIIRunStopsAtTheFirstZeroOrHighBitByteInBothTheWordAndTheTailLoop(t *testing.T) {
	var errs []error
	for _, tc := range []struct {
		name string
		data []byte
		want uint
	}{
		{"empty", []byte{}, 0},
		{"one_ascii", []byte{'a'}, 1},
		{"stops_on_leading_nul", []byte{0, 'a', 'b'}, 0},
		{"stops_on_leading_high_bit", []byte{0xC3, 'a'}, 0},
		{"seven_ascii_tail_only", []byte("abcdefg"), 7},
		{"eight_ascii_one_word", []byte("abcdefgh"), 8},
		{"nine_ascii_word_plus_tail", []byte("abcdefghi"), 9},
		{"nul_inside_first_word", []byte{'a', 'b', 0, 'd', 'e', 'f', 'g', 'h', 'i'}, 2},
		{"high_bit_inside_first_word", []byte{'a', 'b', 'c', 0xE2, 'e', 'f', 'g', 'h', 'i'}, 3},
		{"nul_in_second_word", []byte("abcdefgh\x00jklmnopq"), 8},
		{"high_bit_at_word_boundary", []byte("abcdefg\xC3ijklmnop"), 7},
		{"all_ascii_two_words", []byte("abcdefghijklmnop"), 16},
	} {
		if got := nonZeroASCIIRun(tc.data, 0, uint(len(tc.data))); got != tc.want {
			errs = append(errs, fmt.Errorf(
				"a wrong run length silently reclassifies bytes as valid one-byte sequences: case=%s got=%d want=%d",
				tc.name, got, tc.want))
		}
	}
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
}
