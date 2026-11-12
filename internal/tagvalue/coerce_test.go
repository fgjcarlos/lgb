package tagvalue_test

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/tagvalue"
)

// TestCoerce_EachScalarType pins the Go type produced for every Sparkplug
// scalar type, from both native Go values and json.Number (the form the HTTP
// write endpoint decodes with UseNumber).
func TestCoerce_EachScalarType(t *testing.T) {
	cases := []struct {
		typ  string
		raw  any
		want any
	}{
		{"Boolean", true, true},
		{"Boolean", false, false},
		{"Int8", json.Number("-128"), int8(-128)},
		{"Int8", json.Number("127"), int8(127)},
		{"Int16", json.Number("-32768"), int16(-32768)},
		{"Int16", json.Number("32767"), int16(32767)},
		{"Int16", json.Number("1e3"), int16(1000)},
		{"Int32", json.Number("2147483647"), int32(2147483647)},
		{"Int64", json.Number("9223372036854775807"), int64(math.MaxInt64)},
		{"Int64", json.Number("-9223372036854775808"), int64(math.MinInt64)},
		{"UInt8", json.Number("255"), uint8(255)},
		{"UInt16", json.Number("65535"), uint16(65535)},
		{"UInt32", json.Number("4294967295"), uint32(4294967295)},
		{"UInt64", json.Number("18446744073709551615"), uint64(math.MaxUint64)},
		{"Float", json.Number("2.5"), float32(2.5)},
		{"Float", json.Number("-3"), float32(-3)},
		{"Double", json.Number("2.5"), 2.5},
		{"Double", json.Number("1e300"), 1e300},
		{"String", "hello", "hello"},
		{"String", "", ""},
		// Native Go values (OPC UA variants) keep converting as before.
		{"Int16", float64(42), int16(42)},
		{"Float", float64(2.5), float32(2.5)},
		{"UInt64", uint64(math.MaxUint64), uint64(math.MaxUint64)},
		// Exact integer literals in decimal or exponent form.
		{"Int16", json.Number("1.0"), int16(1)},
		{"Int16", json.Number("1.5e1"), int16(15)},
		{"Int16", json.Number("-12E+2"), int16(-1200)},
		{"UInt8", json.Number("-0"), uint8(0)},
		{"Int8", json.Number("0e99999999999999999999"), int8(0)},
		{"Int64", json.Number("9.223372036854775807e18"), int64(math.MaxInt64)},
		{"UInt64", json.Number("1.8446744073709551615e19"), uint64(math.MaxUint64)},
		// Beyond 2^53: evaluated exactly, never rounded through float64.
		{"Int64", json.Number("9007199254740993.0"), int64(9007199254740993)},
		{"Int64", json.Number("9.007199254740993e15"), int64(9007199254740993)},
	}
	for _, tc := range cases {
		got, err := tagvalue.Coerce(tc.raw, tc.typ)
		if err != nil {
			t.Errorf("Coerce(%#v, %s) error = %v; want %#v", tc.raw, tc.typ, err, tc.want)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Coerce(%#v, %s) = %#v (%T); want %#v (%T)", tc.raw, tc.typ, got, got, tc.want, tc.want)
		}
	}
}

// TestCoerce_RejectsUnrepresentableValues covers the strict JSON rules: no
// string parsing, no number↔bool/string conversion, integral and in-range
// integers, Float within the float32 range, and unknown declared types.
func TestCoerce_RejectsUnrepresentableValues(t *testing.T) {
	cases := []struct {
		typ string
		raw any
	}{
		{"Boolean", "true"},
		{"Boolean", json.Number("1")},
		{"Boolean", nil},
		{"Int8", json.Number("128")},
		{"Int8", json.Number("-129")},
		{"Int16", json.Number("32768")},
		{"Int16", json.Number("2.5")},
		{"Int16", "2"},
		{"Int16", true},
		{"Int32", json.Number("2147483648")},
		{"Int64", json.Number("9223372036854775808")},
		{"Int64", json.Number("-9223372036854775809")},
		{"UInt8", json.Number("-1")},
		{"UInt8", json.Number("256")},
		{"UInt16", json.Number("65536")},
		{"UInt32", json.Number("4294967296")},
		{"UInt64", json.Number("-1")},
		{"UInt64", json.Number("18446744073709551616")},
		// Non-integer or out-of-range literals must not be rounded through
		// float64 into a different integer.
		{"Int16", json.Number("1.00000000000000001")},
		{"Int64", json.Number("9.223372036854775808e18")},
		{"Int64", json.Number("9007199254740993.5")},
		{"Int16", json.Number("01")},
		{"Int16", json.Number("1.")},
		{"Int16", json.Number("1e")},
		{"Int16", json.Number("-")},
		{"Int16", json.Number("12e-1")},
		{"Int16", json.Number("1e-400")},
		{"Int64", json.Number("1e1000000000")},
		{"UInt64", json.Number("1e99999999999999999999")},
		{"UInt64", json.Number("-1e0")},
		{"Int16", json.Number("0x10")},
		{"Int16", json.Number("")},
		{"Float", "2.5"},
		{"Float", json.Number("1e100")},
		{"Float", nil},
		{"Double", json.Number("1e400")},
		{"Double", "2.5"},
		{"String", json.Number("2")},
		{"String", true},
		{"String", nil},
		{"Struct", json.Number("1")},
		{"", "x"},
	}
	for _, tc := range cases {
		got, err := tagvalue.Coerce(tc.raw, tc.typ)
		if err == nil {
			t.Errorf("Coerce(%#v, %q) = %#v; want error", tc.raw, tc.typ, got)
			continue
		}
		if !errors.Is(err, tagvalue.ErrInvalidValue) {
			t.Errorf("Coerce(%#v, %q) error = %v; want ErrInvalidValue", tc.raw, tc.typ, err)
		}
		if got != nil {
			t.Errorf("Coerce(%#v, %q) value = %#v; want nil on error", tc.raw, tc.typ, got)
		}
	}
}

// TestCoerce_ErrorNamesExpectedType pins the human-readable message the HTTP
// endpoint returns, so a UI can show what the tag accepts.
func TestCoerce_ErrorNamesExpectedType(t *testing.T) {
	cases := []struct {
		typ, want string
	}{
		{"Boolean", "value is not a valid Boolean: expected true or false"},
		{"Int16", "value is not a valid Int16: expected an integer between -32768 and 32767"},
		{"UInt64", "value is not a valid UInt64: expected an integer between 0 and 18446744073709551615"},
		{"Float", "value is not a valid Float: expected a number"},
		{"String", "value is not a valid String: expected a string"},
		{"Struct", `value cannot be written: unsupported tag type "Struct"`},
	}
	for _, tc := range cases {
		_, err := tagvalue.Coerce([]int{1}, tc.typ)
		if err == nil {
			t.Fatalf("Coerce(%s) unexpectedly succeeded", tc.typ)
		}
		if msg := tagvalue.Message(err); msg != tc.want {
			t.Errorf("Message(%s) = %q; want %q", tc.typ, msg, tc.want)
		}
		if !strings.HasPrefix(err.Error(), "tagvalue: ") {
			t.Errorf("err = %q; want tagvalue: prefix", err.Error())
		}
	}
}
