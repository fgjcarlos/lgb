// Package tagvalue converts a value supplied by a write surface (HTTP, OPC UA)
// into the exact Go type the PLC driver expects for a tag's declared
// Sparkplug type.
//
// The PLC driver derives the on-wire CIP type from the Go type of the value it
// receives (float64 → LREAL, float32 → REAL, int16 → INT, …), so every write
// MUST be coerced to the declared type before it reaches the driver. A value
// that cannot represent the declared type is rejected, never narrowed.
package tagvalue

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidValue is matched (errors.Is) by every error Coerce returns.
var ErrInvalidValue = errors.New("tagvalue: invalid value")

// invalidError carries the human-readable reason a value was rejected.
type invalidError struct {
	msg string
}

func (e *invalidError) Error() string        { return "tagvalue: " + e.msg }
func (e *invalidError) Is(target error) bool { return target == ErrInvalidValue }

// Message returns the human-readable reason carried by an error from Coerce,
// without the package prefix. Other errors are returned verbatim.
func Message(err error) string {
	var ie *invalidError
	if errors.As(err, &ie) {
		return ie.msg
	}
	return err.Error()
}

// expectations describes what each declared type accepts, for error messages.
var expectations = map[string]string{
	"Boolean": "true or false",
	"Int8":    "an integer between -128 and 127",
	"Int16":   "an integer between -32768 and 32767",
	"Int32":   "an integer between -2147483648 and 2147483647",
	"Int64":   "an integer between -9223372036854775808 and 9223372036854775807",
	"UInt8":   "an integer between 0 and 255",
	"UInt16":  "an integer between 0 and 65535",
	"UInt32":  "an integer between 0 and 4294967295",
	"UInt64":  "an integer between 0 and 18446744073709551615",
	"Float":   "a number",
	"Double":  "a number",
	"String":  "a string",
}

func invalid(declaredType string) error {
	want, ok := expectations[declaredType]
	if !ok {
		return &invalidError{msg: fmt.Sprintf("value cannot be written: unsupported tag type %q", declaredType)}
	}
	return &invalidError{msg: fmt.Sprintf("value is not a valid %s: expected %s", declaredType, want)}
}

// Coerce converts raw to the Go type matching declaredType:
//
//	Boolean → bool, Int8..Int64 → int8..int64, UInt8..UInt64 → uint8..uint64,
//	Float → float32, Double → float64, String → string.
//
// Accepted inputs are strict: Boolean only from bool, String only from string,
// numeric types from Go numbers or json.Number. Strings are never parsed as
// numbers or booleans. Integer types accept only integral, in-range values;
// Float rejects values outside the float32 range. On failure the returned
// error matches ErrInvalidValue and the value is nil.
func Coerce(raw any, declaredType string) (any, error) {
	v, ok := convert(raw, declaredType)
	if !ok {
		return nil, invalid(declaredType)
	}
	return v, nil
}

func convert(raw any, declaredType string) (any, bool) {
	switch declaredType {
	case "Boolean":
		b, ok := raw.(bool)
		return b, ok
	case "Int8":
		i, ok := toInt64(raw)
		if !ok || i < math.MinInt8 || i > math.MaxInt8 {
			return nil, false
		}
		return int8(i), true
	case "Int16":
		i, ok := toInt64(raw)
		if !ok || i < math.MinInt16 || i > math.MaxInt16 {
			return nil, false
		}
		return int16(i), true
	case "Int32":
		i, ok := toInt64(raw)
		if !ok || i < math.MinInt32 || i > math.MaxInt32 {
			return nil, false
		}
		return int32(i), true
	case "Int64":
		i, ok := toInt64(raw)
		if !ok {
			return nil, false
		}
		return i, true
	case "UInt8":
		i, ok := toInt64(raw)
		if !ok || i < 0 || i > math.MaxUint8 {
			return nil, false
		}
		return uint8(i), true
	case "UInt16":
		i, ok := toInt64(raw)
		if !ok || i < 0 || i > math.MaxUint16 {
			return nil, false
		}
		return uint16(i), true
	case "UInt32":
		i, ok := toInt64(raw)
		if !ok || i < 0 || i > math.MaxUint32 {
			return nil, false
		}
		return uint32(i), true
	case "UInt64":
		i, ok := toUint64(raw)
		if !ok {
			return nil, false
		}
		return i, true
	case "Float":
		f, ok := toFloat64(raw)
		if !ok {
			return nil, false
		}
		// A double outside the float32 range narrows to ±Inf, which is not the
		// number the client sent and not a value a PLC should ever receive.
		if f > math.MaxFloat32 || f < -math.MaxFloat32 {
			return nil, false
		}
		return float32(f), true
	case "Double":
		f, ok := toFloat64(raw)
		if !ok {
			return nil, false
		}
		return f, true
	case "String":
		s, ok := raw.(string)
		return s, ok
	default:
		// Unknown declared type — the value cannot be converted safely.
		return nil, false
	}
}

// toInt64 converts a numeric value to an int64. Floating point values are
// accepted only when they are integral and in range. A json.Number is
// evaluated exactly in decimal (see jsonInteger), never through float64, so a
// literal is accepted only when it denotes an integer and is never rounded.
func toInt64(raw any) (int64, bool) {
	switch v := raw.(type) {
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case float32:
		return floatToInt64(float64(v))
	case float64:
		return floatToInt64(v)
	case json.Number:
		neg, mag, ok := jsonInteger(v)
		if !ok {
			return 0, false
		}
		if neg {
			mag = "-" + mag
		}
		i, err := strconv.ParseInt(mag, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

// floatToInt64 converts an integral float to an int64.
//
// The upper bound is exclusive: float64 cannot represent math.MaxInt64
// exactly, so it rounds up to 2^63. Comparing against math.MaxInt64 would
// therefore let 2^63 through, and converting it to int64 overflows into a
// wrapped negative value instead of the number the client sent.
func floatToInt64(f float64) (int64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if f != math.Trunc(f) {
		return 0, false
	}
	if f < math.MinInt64 || f >= math.Exp2(63) {
		return 0, false
	}
	return int64(f), true
}

// toUint64 converts a non-negative integral value to a uint64 without
// narrowing the full uint64 input range through int64.
func toUint64(raw any) (uint64, bool) {
	switch v := raw.(type) {
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	case int8:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int16:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int32:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case float32:
		return floatToUint64(float64(v))
	case float64:
		return floatToUint64(v)
	case json.Number:
		neg, mag, ok := jsonInteger(v)
		if !ok || (neg && mag != "0") {
			return 0, false
		}
		u, err := strconv.ParseUint(mag, 10, 64)
		return u, err == nil
	default:
		return 0, false
	}
}

// floatToUint64 converts an integral float in [0, 2^64) to uint64. The upper
// bound is exclusive because float64 cannot represent math.MaxUint64 exactly.
func floatToUint64(f float64) (uint64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f != math.Trunc(f) || f >= math.Exp2(64) {
		return 0, false
	}
	return uint64(f), true
}

// toFloat64 converts a numeric value to a float64.
func toFloat64(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		return jsonNumberToFloat64(v)
	default:
		return 0, false
	}
}

// maxIntegerDigits bounds the decimal digits of an integer literal: 2^64 has
// 20 digits, so anything longer is out of every supported integer range.
const maxIntegerDigits = 20

// jsonInteger evaluates a JSON number literal exactly and reports whether it
// denotes an integer. It returns the sign and the magnitude as plain decimal
// digits without leading zeros ("0" for zero), e.g. "1.5e1" → (false, "15"),
// "-12E+2" → (true, "1200"), "1.0" → (false, "1"). Non-integral values
// ("12e-1", "1.00000000000000001") and malformed literals are rejected. The
// work is bounded by the literal length: the exponent is never expanded past
// maxIntegerDigits, so "1e1000000000" is rejected without allocating.
func jsonInteger(n json.Number) (neg bool, mag string, ok bool) {
	s := string(n)
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	}
	mant, expPart, hasExp := strings.Cut(strings.ToLower(s), "e")
	intPart, frac, hasFrac := strings.Cut(mant, ".")
	if !isDigits(intPart) || (hasFrac && !isDigits(frac)) {
		return false, "", false
	}
	if len(intPart) > 1 && intPart[0] == '0' {
		return false, "", false // JSON forbids leading zeros
	}
	digits := strings.TrimLeft(intPart+frac, "0")
	if digits == "" {
		// Zero: any well-formed exponent still denotes zero.
		if hasExp && !validExponent(expPart) {
			return false, "", false
		}
		return neg, "0", true
	}
	// value = digits × 10^shift
	shift := int64(-len(frac))
	if hasExp {
		if !validExponent(expPart) {
			return false, "", false
		}
		e, err := strconv.ParseInt(expPart, 10, 64)
		if err != nil {
			// |exponent| beyond int64: either far too large or non-integral.
			return false, "", false
		}
		if e > maxIntegerDigits || e < -int64(len(s)) {
			// Either out of every integer range, or more negative than the
			// literal has digits (so the value cannot be integral).
			return false, "", false
		}
		shift += e
	}
	trimmed := strings.TrimRight(digits, "0")
	shift += int64(len(digits) - len(trimmed))
	digits = trimmed
	if shift < 0 {
		return false, "", false // fractional part is non-zero
	}
	if int64(len(digits))+shift > maxIntegerDigits {
		return false, "", false
	}
	return neg, digits + strings.Repeat("0", int(shift)), true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// validExponent reports whether s is a JSON exponent: optional sign, digits.
func validExponent(s string) bool {
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		s = s[1:]
	}
	return isDigits(s)
}

// jsonNumberToFloat64 parses a JSON number literal, rejecting literals outside
// the float64 range (strconv returns ±Inf with ErrRange for those).
func jsonNumberToFloat64(n json.Number) (float64, bool) {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
