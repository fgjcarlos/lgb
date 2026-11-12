package cov_test

import (
	"math"
	"sync"
	"testing"

	"github.com/fgjcarlos/lgb/internal/cov"
)

func always() bool { return true }
func never() bool  { return false }

func sample(v any, q string, db cov.Deadband) cov.Sample {
	return cov.Sample{Key: "plc1/Tag", Value: v, Quality: q, Deadband: db}
}

func TestFilterOffer(t *testing.T) {
	none := cov.Deadband{}
	tests := []struct {
		name  string
		first cov.Sample
		next  cov.Sample
		want  bool
	}{
		{"no change suppressed", sample(int32(5), "good", none), sample(int32(5), "good", none), false},
		{"any change without deadband emits", sample(10.0, "good", none), sample(10.001, "good", none), true},
		{"bool change emits", sample(false, "good", none), sample(true, "good", none), true},
		{"bool no change suppressed", sample(true, "good", none), sample(true, "good", none), false},
		{"string change emits", sample("a", "good", none), sample("b", "good", none), true},
		{"quality change emits with same value", sample(nil, "bad", none), sample(nil, "good", none), true},
		{"empty quality equals good", sample(1.0, "", none), sample(1.0, "good", none), false},
		{"repeated bad suppressed", sample(nil, "bad", none), sample(nil, "bad", none), false},
		{"type change emits", sample(int32(1), "good", none), sample(int64(1), "good", none), true},
		{"within absolute deadband suppressed", sample(10.0, "good", cov.Deadband{Absolute: 0.5}), sample(10.5, "good", cov.Deadband{Absolute: 0.5}), false},
		{"beyond absolute deadband emits", sample(10.0, "good", cov.Deadband{Absolute: 0.5}), sample(10.6, "good", cov.Deadband{Absolute: 0.5}), true},
		{"beyond absolute deadband downward emits", sample(int16(10), "good", cov.Deadband{Absolute: 2}), sample(int16(7), "good", cov.Deadband{Absolute: 2}), true},
		{"within percent deadband suppressed", sample(200.0, "good", cov.Deadband{Percent: 1}), sample(202.0, "good", cov.Deadband{Percent: 1}), false},
		{"beyond percent deadband emits", sample(200.0, "good", cov.Deadband{Percent: 1}), sample(202.5, "good", cov.Deadband{Percent: 1}), true},
		{"percent of zero emits any change", sample(0.0, "good", cov.Deadband{Percent: 10}), sample(0.001, "good", cov.Deadband{Percent: 10}), true},
		{"both set must exceed absolute", sample(100.0, "good", cov.Deadband{Absolute: 5, Percent: 1}), sample(104.0, "good", cov.Deadband{Absolute: 5, Percent: 1}), false},
		{"both set must exceed percent", sample(1000.0, "good", cov.Deadband{Absolute: 5, Percent: 1}), sample(1008.0, "good", cov.Deadband{Absolute: 5, Percent: 1}), false},
		{"both set exceeded emits", sample(100.0, "good", cov.Deadband{Absolute: 5, Percent: 1}), sample(106.0, "good", cov.Deadband{Absolute: 5, Percent: 1}), true},
		{"deadband ignored for bool", sample(false, "good", cov.Deadband{Absolute: 5}), sample(true, "good", cov.Deadband{Absolute: 5}), true},
		{"large int64 exact change without deadband", sample(int64(1<<62), "good", none), sample(int64(1<<62+1), "good", none), true},
		{"NaN to NaN suppressed", sample(math.NaN(), "good", none), sample(math.NaN(), "good", none), false},
		{"number to NaN emits", sample(1.0, "good", cov.Deadband{Absolute: 5}), sample(math.NaN(), "good", cov.Deadband{Absolute: 5}), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := cov.NewFilter()
			if !f.Offer(tc.first, always) {
				t.Fatal("first value must always be delivered")
			}
			if got := f.Offer(tc.next, always); got != tc.want {
				t.Fatalf("Offer(%v) = %v, want %v", tc.next.Value, got, tc.want)
			}
		})
	}
}

// The deadband compares against the last delivered value, so slow drift
// accumulates until it crosses the deadband.
func TestFilterComparesAgainstLastDelivered(t *testing.T) {
	db := cov.Deadband{Absolute: 1}
	f := cov.NewFilter()
	f.Offer(sample(10.0, "good", db), always)
	for _, v := range []float64{10.4, 10.8} {
		if f.Offer(sample(v, "good", db), always) {
			t.Fatalf("%v within deadband of 10 must be suppressed", v)
		}
	}
	if !f.Offer(sample(11.2, "good", db), always) {
		t.Fatal("11.2 is beyond the deadband of the last delivered 10, must emit")
	}
}

func TestFilterFailedDeliveryDoesNotAdvance(t *testing.T) {
	f := cov.NewFilter()
	f.Offer(sample(1.0, "good", cov.Deadband{}), always)
	calls := 0
	if f.Offer(sample(2.0, "good", cov.Deadband{}), func() bool { calls++; return false }) {
		t.Fatal("Offer must report a failed delivery")
	}
	if calls != 1 {
		t.Fatalf("deliver called %d times, want 1", calls)
	}
	if !f.Offer(sample(2.0, "good", cov.Deadband{}), always) {
		t.Fatal("value whose delivery failed must be offered again")
	}
}

func TestFilterSuppressedDoesNotCallDeliver(t *testing.T) {
	f := cov.NewFilter()
	f.Offer(sample(1.0, "good", cov.Deadband{}), always)
	f.Offer(sample(1.0, "good", cov.Deadband{}), func() bool {
		t.Fatal("deliver must not be called for a suppressed value")
		return true
	})
}

func TestFilterKeysAreIndependent(t *testing.T) {
	f := cov.NewFilter()
	f.Offer(cov.Sample{Key: "a", Value: 1, Quality: "good"}, always)
	if !f.Offer(cov.Sample{Key: "b", Value: 1, Quality: "good"}, always) {
		t.Fatal("first value of another key must be delivered")
	}
}

func TestFilterReset(t *testing.T) {
	f := cov.NewFilter()
	f.Offer(sample(1.0, "good", cov.Deadband{}), always)
	f.Reset()
	if !f.Offer(sample(1.0, "good", cov.Deadband{}), always) {
		t.Fatal("after Reset the current value must be delivered again")
	}
}

func TestDisabledFilterDeliversEverything(t *testing.T) {
	var f *cov.Filter // nil filter = COV disabled
	if !f.Offer(sample(1.0, "good", cov.Deadband{}), always) || !f.Offer(sample(1.0, "good", cov.Deadband{}), always) {
		t.Fatal("nil filter must deliver every value")
	}
	if f.Offer(sample(1.0, "good", cov.Deadband{}), never) {
		t.Fatal("nil filter must report delivery result")
	}
	f.Reset() // must not panic
}

// Two consumers fed the same sequence keep independent state: one dropping
// deliveries does not change what the other receives.
func TestFiltersArePerConsumer(t *testing.T) {
	fast, slow := cov.NewFilter(), cov.NewFilter()
	var fastGot, slowGot []any
	seq := []float64{1, 1, 2, 2, 3}
	for i, v := range seq {
		s := sample(v, "good", cov.Deadband{})
		fast.Offer(s, func() bool { fastGot = append(fastGot, v); return true })
		slowFull := i == 2 // slow consumer's buffer is full when 2 arrives
		slow.Offer(s, func() bool {
			if slowFull {
				return false
			}
			slowGot = append(slowGot, v)
			return true
		})
	}
	if want := []any{1.0, 2.0, 3.0}; !equal(fastGot, want) {
		t.Fatalf("fast got %v, want %v", fastGot, want)
	}
	if want := []any{1.0, 2.0, 3.0}; !equal(slowGot, want) {
		t.Fatalf("slow got %v, want %v (2 retried after the drop)", slowGot, want)
	}
}

func TestFilterConcurrentUse(t *testing.T) {
	f := cov.NewFilter()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				f.Offer(cov.Sample{Key: string(rune('a' + i)), Value: j, Quality: "good"}, always)
				if j%10 == 0 {
					f.Reset()
				}
			}
		}(i)
	}
	wg.Wait()
}

func equal(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
