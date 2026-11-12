// Package cov implements change-on-value (COV) filtering of tag updates.
//
// A Filter belongs to one consumer (a Sparkplug edge node, one realtime
// subscriber, the historian). It remembers the last value it delivered for
// each key and suppresses updates that do not differ from it by more than the
// tag's deadband. Because each consumer owns its own Filter, one consumer's
// state or dropped deliveries never affect what another consumer receives.
package cov

import (
	"math"
	"reflect"
	"sync"
)

// Deadband is the optional numeric deadband of a tag. Zero values disable the
// corresponding check. A numeric update is emitted only when
//
//	|new - lastDelivered| > max(Absolute, Percent/100 * |lastDelivered|)
//
// so when both are set the change must exceed both. Deadbands apply only to
// numeric values; bool and string values emit on any change.
type Deadband struct {
	Absolute float64
	Percent  float64
}

// IsZero reports whether no deadband is configured.
func (d Deadband) IsZero() bool { return d.Absolute == 0 && d.Percent == 0 }

// Sample is one tag update offered to a Filter. Key identifies the tag
// (typically "<plc>/<tag>"). An empty Quality is treated as "good".
type Sample struct {
	Key      string
	Value    any
	Quality  string
	Deadband Deadband
}

type entry struct {
	value   any
	quality string
}

// Filter is a per-consumer change-on-value filter. It is safe for concurrent
// use. A nil *Filter means COV is disabled: every sample is delivered.
type Filter struct {
	mu   sync.Mutex
	last map[string]entry
}

// NewFilter returns an empty Filter.
func NewFilter() *Filter {
	return &Filter{last: make(map[string]entry)}
}

// Offer calls deliver when s differs from the last value delivered for s.Key
// and returns the delivery result. The sample becomes the new reference only
// when deliver returns true, so a dropped delivery is retried on the next
// offer. deliver runs under the filter lock: it must not block or call back
// into the filter.
func (f *Filter) Offer(s Sample, deliver func() bool) bool {
	if f == nil {
		return deliver()
	}
	quality := normQuality(s.Quality)

	f.mu.Lock()
	defer f.mu.Unlock()

	if prev, ok := f.last[s.Key]; ok && !changed(prev, s.Value, quality, s.Deadband) {
		return false
	}
	if !deliver() {
		return false
	}
	f.last[s.Key] = entry{value: s.Value, quality: quality}
	return true
}

// Reset forgets every delivered value, so the next sample of each key is
// delivered unconditionally (used after a Sparkplug BIRTH).
func (f *Filter) Reset() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.last)
}

func normQuality(q string) string {
	if q == "" {
		return "good"
	}
	return q
}

func changed(prev entry, value any, quality string, db Deadband) bool {
	if prev.quality != quality {
		return true
	}
	if reflect.TypeOf(prev.value) != reflect.TypeOf(value) {
		return true
	}
	a, aNum := toFloat(prev.value)
	b, bNum := toFloat(value)
	if aNum && bNum {
		aNaN, bNaN := math.IsNaN(a), math.IsNaN(b)
		if aNaN || bNaN {
			return aNaN != bNaN
		}
		if !db.IsZero() {
			threshold := math.Max(db.Absolute, db.Percent/100*math.Abs(a))
			return math.Abs(b-a) > threshold
		}
	}
	return !equalValues(prev.value, value)
}

// equalValues compares two values of the same dynamic type without panicking
// on non-comparable types (which are treated as always changed).
func equalValues(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if !reflect.TypeOf(a).Comparable() {
		return false
	}
	return a == b
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
