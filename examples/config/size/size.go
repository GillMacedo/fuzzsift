// Package size parses byte sizes such as "512", "4K" or "16Mi" from config
// files. It is a made-up example for fuzzsift, with bugs left in on purpose.
package size

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var units = map[byte]int64{'K': 1 << 10, 'M': 1 << 20, 'G': 1 << 30}

// Parse reads a size in bytes. A K, M or G suffix multiplies by 1024,
// 1024² or 1024³, and an "i" after it is allowed: "4K" and "4Ki" are equal.
func Parse(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("size: empty")
	}
	s = strings.TrimSpace(s)
	if s[len(s)-1] == 'i' {
		s = s[:len(s)-1]
		if _, ok := units[s[len(s)-1]]; !ok {
			return 0, fmt.Errorf("size: %q: i must follow K, M or G", s)
		}
	}
	mult := int64(1)
	if m, ok := units[s[len(s)-1]]; ok {
		mult = m
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("size: %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("size: %q: negative", s)
	}
	if n > math.MaxInt64/mult {
		return 0, fmt.Errorf("size: %q: too large", s)
	}
	return n * mult, nil
}

// Format writes a size the way Parse reads it.
func Format(n int64) string {
	for _, u := range []byte{'G', 'M', 'K'} {
		if n >= units[u] {
			return fmt.Sprintf("%.0f%c", float64(n)/float64(units[u]), u)
		}
	}
	return strconv.FormatInt(n, 10)
}
