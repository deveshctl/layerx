package sizebudget

import "testing"

func TestLimitsExactBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  Config
		before  *int64
		current int64
		pass    bool
		wantErr bool
	}{
		{"decimal equality", Config{Max: "1.5MB"}, nil, 1500000, true, false},
		{"decimal over", Config{Max: "1.5MB"}, nil, 1500001, false, false},
		{"binary equality", Config{Max: "1MiB"}, nil, 1 << 20, true, false},
		{"zero maximum", Config{Max: "0B"}, nil, 1, false, false},
		{"zero growth", Config{MaxGrowth: "0B"}, intPointer(100), 101, false, false},
		{"percentage equality", Config{MaxGrowth: "5%"}, intPointer(100), 105, true, false},
		{"percentage over", Config{MaxGrowth: "5%"}, intPointer(100), 106, false, false},
		{"fractional allowance", Config{MaxGrowth: "0.5%"}, intPointer(101), 102, false, false},
		{"shrinking", Config{MaxGrowth: "0%"}, intPointer(100), 99, true, false},
		{"missing baseline", Config{MaxGrowth: "1MB"}, nil, 100, false, true},
		{"zero baseline percent", Config{MaxGrowth: "5%"}, intPointer(0), 1, false, true},
		{"both empty", Config{MaxGrowth: "5%"}, intPointer(0), 0, true, false},
		{"zero baseline bytes", Config{MaxGrowth: "1B"}, intPointer(0), 1, true, false},
		{"both limits", Config{Max: "104B", MaxGrowth: "5%"}, intPointer(100), 105, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits, err := Parse(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			checks, err := limits.Evaluate(tc.config, tc.current, tc.before)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			pass := true
			for _, c := range checks {
				pass = pass && c.Passed
			}
			if pass != tc.pass {
				t.Fatalf("passed=%v checks=%+v", pass, checks)
			}
		})
	}
}

func intPointer(n int64) *int64 { return &n }

func TestInvalidLimitsAndOmission(t *testing.T) {
	for _, s := range []string{"-1MB", "NaN", "Inf", "5%", "1mb", "1.1B", "9223372036854775808B", "1e6", ""} {
		l, err := Parse(Config{Max: s})
		if s == "" {
			if err != nil || l.Enabled() {
				t.Fatal("omission should disable limits")
			}
			continue
		}
		if err == nil {
			t.Errorf("accepted invalid maximum %q", s)
		}
	}
	for _, s := range []string{"-5%", "NaN%", "0.1B", "9223372036854775808B"} {
		if _, err := Parse(Config{MaxGrowth: s}); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
