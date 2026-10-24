// Package sizebudget parses and evaluates opt-in uncompressed image-size limits.
package sizebudget

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
)

type Config struct {
	Max       string `yaml:"max,omitempty" json:"max,omitempty"`
	MaxGrowth string `yaml:"max-growth,omitempty" json:"maxGrowth,omitempty"`
}

type Limits struct {
	Max     *int64
	Growth  *big.Rat
	Percent bool
}

type Result struct {
	Name        string `json:"name"`
	ActualBytes int64  `json:"actualBytes"`
	LimitBytes  int64  `json:"limitBytes"`
	Limit       string `json:"limit"`
	Passed      bool   `json:"passed"`
}

var quantity = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*(B|KB|MB|GB|TB|KiB|MiB|GiB|TiB|%)?$`)

// Parse accepts standard decimal/binary units. Omission disables a limit;
// explicit zero is a real ceiling. A fractional byte is rejected.
func Parse(c Config) (Limits, error) {
	var out Limits
	if c.Max != "" {
		n, percent, err := parseQuantity(c.Max)
		if err != nil || percent {
			return out, fmt.Errorf("size-budget.max: use a byte size such as 500MB or 512MiB")
		}
		v, err := byteValue(n)
		if err != nil {
			return out, fmt.Errorf("size-budget.max: %w", err)
		}
		out.Max = &v
	}
	if c.MaxGrowth != "" {
		n, percent, err := parseQuantity(c.MaxGrowth)
		if err != nil {
			return out, fmt.Errorf("size-budget.max-growth: use a size such as 20MB or a percentage such as 5%%")
		}
		if !percent {
			if _, err := byteValue(n); err != nil {
				return out, fmt.Errorf("size-budget.max-growth: %w", err)
			}
		}
		out.Growth, out.Percent = n, percent
	}
	return out, nil
}

func parseQuantity(s string) (*big.Rat, bool, error) {
	m := quantity.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return nil, false, fmt.Errorf("invalid quantity")
	}
	n, ok := new(big.Rat).SetString(m[1])
	if !ok {
		return nil, false, fmt.Errorf("invalid number")
	}
	if m[2] == "%" {
		return n.Quo(n, big.NewRat(100, 1)), true, nil
	}
	multipliers := map[string]int64{"": 1, "B": 1, "KB": 1000, "MB": 1000000, "GB": 1000000000, "TB": 1000000000000, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}
	return n.Mul(n, new(big.Rat).SetInt64(multipliers[m[2]])), false, nil
}

func byteValue(n *big.Rat) (int64, error) {
	if !n.IsInt() || !n.Num().IsInt64() {
		return 0, fmt.Errorf("size must be a whole number of bytes within int64 range")
	}
	return n.Num().Int64(), nil
}

func (l Limits) Enabled() bool { return l.Max != nil || l.Growth != nil }

// Evaluate uses exact integer/rational comparisons, never rounded display values.
func (l Limits) Evaluate(c Config, current int64, baseline *int64) ([]Result, error) {
	var results []Result
	if l.Max != nil {
		results = append(results, Result{"image size", current, *l.Max, c.Max, current <= *l.Max})
	}
	if l.Growth == nil {
		return results, nil
	}
	if baseline == nil {
		return nil, fmt.Errorf("size-budget.max-growth requires a baseline; use --baseline OLD with ci, or compare OLD NEW")
	}
	allowance := new(big.Rat).Set(l.Growth)
	if l.Percent {
		if *baseline == 0 && current > 0 {
			return nil, fmt.Errorf("percentage growth from a zero-byte baseline is undefined; use a byte limit such as --max-growth 20MB")
		}
		allowance.Mul(allowance, new(big.Rat).SetInt64(*baseline))
	}
	whole := new(big.Int).Quo(allowance.Num(), allowance.Denom())
	if !whole.IsInt64() {
		return nil, fmt.Errorf("growth allowance exceeds int64 byte range")
	}
	delta := current - *baseline
	results = append(results, Result{"image growth", delta, whole.Int64(), c.MaxGrowth, delta <= whole.Int64()})
	return results, nil
}

// Format uses decimal units with explicit signs only when requested by callers.
func Format(n int64) string {
	value := float64(n)
	for _, unit := range []struct {
		size float64
		name string
	}{{1e12, "TB"}, {1e9, "GB"}, {1e6, "MB"}, {1e3, "KB"}} {
		if math.Abs(value) >= unit.size {
			return fmt.Sprintf("%.2f %s", value/unit.size, unit.name)
		}
	}
	return fmt.Sprintf("%d B", n)
}
