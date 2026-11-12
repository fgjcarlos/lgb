package cov_test

import (
	"math"
	"testing"

	"github.com/fgjcarlos/lgb/internal/cov"
)

func TestFilterEdgeCases(t *testing.T) {
	cases := []struct {
		name      string
		prev, val any
		db        cov.Deadband
		want      bool
	}{
		{"abs boundary suppressed", 10.0, 10.5, cov.Deadband{Absolute: 0.5}, false},
		{"both: must exceed abs", 100.0, 101.5, cov.Deadband{Absolute: 2, Percent: 1}, false},
		{"both: must exceed pct", 100.0, 101.5, cov.Deadband{Absolute: 1, Percent: 2}, false},
		{"both exceeded", 100.0, 102.5, cov.Deadband{Absolute: 1, Percent: 2}, true},
		{"negative last pct", -100.0, -105.0, cov.Deadband{Percent: 10}, false},
		{"int64 2^53 exact", int64(1 << 53), int64(1<<53 + 1), cov.Deadband{}, true},
		{"+Inf -> 5 pct", math.Inf(1), 5.0, cov.Deadband{Percent: 1}, true},
		{"+Inf -> -Inf abs", math.Inf(1), math.Inf(-1), cov.Deadband{Absolute: 1}, true},
		{"5 -> +Inf abs", 5.0, math.Inf(1), cov.Deadband{Absolute: 1}, true},
		{"+Inf -> +Inf abs suppressed", math.Inf(1), math.Inf(1), cov.Deadband{Absolute: 1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := cov.NewFilter()
			f.Offer(cov.Sample{Key: "k", Value: c.prev, Deadband: c.db}, always)
			if got := f.Offer(cov.Sample{Key: "k", Value: c.val, Deadband: c.db}, always); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}
