package data

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"time"
)

const (
	// MaxConvFloat64 is the largest float64 that can be converted to int64.
	MaxConvFloat64 = float64(math.MaxInt64)

	// MinConvFloat64 is the smallest float64 that can be converted to int64
	MinConvFloat64 = float64(math.MinInt64)
)

// AsBool returns a bool value only when the type of Value is TypeBool,
// otherwise it returns error.
func AsBool(v Value) (bool, error) {
	return v.asBool()
}

// AsInt returns an integer value only when the type of Value is TypeInt,
// otherwise it returns error.
func AsInt(v Value) (int64, error) {
	return v.asInt()
}

// AsFloat returns a float value only when the type of Value is TypeFloat,
// otherwise it returns error.
func AsFloat(v Value) (float64, error) {
	return v.asFloat()
}

// AsString returns a string only when the type of Value is TypeString,
// otherwise it returns error.
func AsString(v Value) (string, error) {
	return v.asString()
}

// AsBlob returns an array of bytes only when the type of Value is TypeBlob,
// otherwise it returns error.
func AsBlob(v Value) ([]byte, error) {
	return v.asBlob()
}

// AsTimestamp returns a time.Time only when the type of Value is TypeTime,
// otherwise it returns error.
func AsTimestamp(v Value) (time.Time, error) {
	return v.asTimestamp()
}

// AsArray returns an array of Values only when the type of Value is TypeArray,
// otherwise it returns error.
func AsArray(v Value) (Array, error) {
	return v.asArray()
}

// AsMap returns a map of string keys and Values only when the type of Value is
// TypeMap, otherwise it returns error.
func AsMap(v Value) (Map, error) {
	return v.asMap()
}

// ToBool converts a given Value to a bool, if possible. The conversion
// rules are similar to those in Python:
//
//  * Null: false
//  * Bool: actual boolean value
//  * Int: true if non-zero
//  * Float: true if non-zero and not NaN
//  * String: true if non-empty
//  * Blob: true if non-empty
//  * Timestamp: true if IsZero() is false
//  * Array: true if non-empty
//  * Map: true if non-empty
func ToBool(v Value) (bool, error) {
	defaultValue := false
	switch v.Type() {
	case TypeNull:
		return defaultValue, nil
	case TypeBool:
		return v.asBool()
	case TypeInt:
		val, _ := v.asInt()
		return val != 0, nil
	case TypeFloat:
		val, _ := v.asFloat()
		return val != 0.0 && !math.IsNaN(val), nil
	case TypeString:
		val, _ := v.asString()
		return len(val) > 0, nil
	case TypeBlob:
		val, _ := v.asBlob()
		return len(val) > 0, nil
	case TypeTimestamp:
		val, _ := v.asTimestamp()
		return !val.IsZero(), nil
	case TypeArray:
		val, _ := v.asArray()
		return len(val) > 0, nil
	case TypeMap:
		val, _ := v.asMap()
		return len(val) > 0, nil
	default:
		return defaultValue,
			fmt.Errorf("cannot convert %T to bool", v)
	}
}

// ToInt converts a given Value to an int64, if possible. The conversion
// rules are as follows:
//
//  * Null: 0
//  * Bool: 0 if false, 1 if true
//  * Int: actual value
//  * Float: conversion as done by int64(value)
//    (values outside of valid int64 bounds will lead to an error)
//  * String: parsed integer with base 0 as per strconv.ParseInt
//    (values outside of valid int64 bounds will lead to an error)
//  * Blob: (error)
//  * Timestamp: the number of second elapsed since January 1, 1970 UTC.
//  * Array: (error)
//  * Map: (error)
func ToInt(v Value) (int64, error) {
	defaultValue := int64(0)
	switch v.Type() {
	case TypeNull:
		return defaultValue, nil
	case TypeBool:
		val, _ := v.asBool()
		if val {
			return 1, nil
		}
		return 0, nil
	case TypeInt:
		return v.asInt()
	case TypeFloat:
		val, _ := v.asFloat()
		if val >= MinConvFloat64 && val <= MaxConvFloat64 {
			return int64(val), nil
		}
		return defaultValue,
			fmt.Errorf("%v is out of bounds for int64 conversion", val)
	case TypeString:
		val, _ := v.asString()
		return strconv.ParseInt(val, 0, 64)
	case TypeTimestamp:
		val, _ := v.asTimestamp()
		// return only second part
		seconds := time.Duration(val.Unix())
		return int64(seconds), nil
	default:
		return defaultValue,
			fmt.Errorf("cannot convert %T to int64", v)
	}
}

// ToFloat converts a given Value to a float64, if possible. The conversion
// rules are as follows:
//
//  * Null: 0.0
//  * Bool: 0.0 if false, 1.0 if true
//  * Int: conversion as done by float64(value)
//  * Float: actual value
//  * String: parsed float as per strconv.ParseFloat
//    (values outside of valid float64 bounds will lead to an error)
//  * Blob: (error)
//  * Timestamp: the number of seconds (not microseconds!) elapsed since
//    January 1, 1970 UTC, with a decimal part
//  * Array: (error)
//  * Map: (error)
func ToFloat(v Value) (float64, error) {
	defaultValue := float64(0)
	switch v.Type() {
	case TypeNull:
		return defaultValue, nil
	case TypeBool:
		val, _ := v.asBool()
		if val {
			return 1.0, nil
		}
		return 0.0, nil
	case TypeInt:
		val, _ := v.asInt()
		return float64(val), nil
	case TypeFloat:
		return v.asFloat()
	case TypeString:
		val, _ := v.asString()
		return strconv.ParseFloat(val, 64)
	case TypeTimestamp:
		val, _ := v.asTimestamp()
		// We want to compute `val.UnixNano()/1e9`, but sometimes `UnixNano()`
		// is not defined, so we switch to `val.Unix() + val.Nanosecond()/1e9`.
		// Note that due to numerical issues, this sometimes yields different
		// results within the range of machine precision.
		return float64(val.Unix()) + float64(val.Nanosecond())/1e9, nil
	default:
		return defaultValue,
			fmt.Errorf("cannot convert %T to float64", v)
	}
}

// ToString converts a given Value to a string. The conversion
// rules are as follows:
//
//  * Null: ""
//  * String: the actual string
//  * Blob: base64-encoded string
//  * Timestamp: ISO 8601 representation, see time.RFC3339
//  * other: Go's "%#v" representation
func ToString(v Value) (string, error) {
	switch v.Type() {
	case TypeNull:
		return "", nil
	case TypeString:
		// if we used "%#v", we will get a quoted string; if
		// we used "%v", we will get the result of String()
		// (which is JSON, i.e., also quoted)
		return v.asString()
	case TypeBlob:
		val, _ := v.asBlob()
		return base64.StdEncoding.EncodeToString(val), nil
	case TypeTimestamp:
		val, _ := v.asTimestamp()
		return val.Format(time.RFC3339Nano), nil
	case TypeArray, TypeMap:
		return v.String(), nil
	default:
		return fmt.Sprintf("%#v", v), nil
	}
}

// ToBlob converts a given Value to []byte, if possible.
// The conversion rules are as follows:
//
//  * Null: nil
//  * String: []byte just copied from string
//  * Blob: actual value
//  * other: (error)
func ToBlob(v Value) ([]byte, error) {
	switch v.Type() {
	case TypeNull:
		return nil, nil
	case TypeString:
		val, _ := v.asString()
		return base64.StdEncoding.DecodeString(val)
	case TypeBlob:
		return v.asBlob()
	case TypeArray:
		a, _ := v.asArray()
		b := make([]byte, len(a))
		for i, e := range a {
			v, err := e.asInt()
			if err != nil {
				return nil, fmt.Errorf("cannot convert %v to Blob value", e.Type())
			} else if !(0 <= v && v <= 255) {
				return nil, fmt.Errorf("cannot convert int to Blob value: %v", v)
			}
			b[i] = byte(v)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to Blob", v)
	}
}

// ToTimestamp converts a given Value to a time.Time struct, if possible.
// The conversion rules are as follows:
//
//  * Null: zero time (this is *not* the time with Unix time 0!)
//  * Int: Time with the given Unix time in seconds
//  * Float: Time with the given Unix time in seconds, where the decimal
//    part will be considered as a part of a second
//    (values outside of valid int64 bounds will lead to an error)
//  * String: Time with the given RFC3339/ISO8601 representation
//  * Timestamp: actual time
//  * other: (error)
func ToTimestamp(v Value) (time.Time, error) {
	defaultValue := time.Time{}
	switch v.Type() {
	case TypeNull:
		return defaultValue, nil
	case TypeInt:
		val, _ := v.asInt()
		return time.Unix(val, 0), nil
	case TypeFloat:
		val, _ := v.asFloat()
		if val >= MinConvFloat64 && val <= MaxConvFloat64 {
			// say val is 3.7 or -4.6
			integralPart := int64(val)                 // 3 or -4
			decimalPart := val - float64(integralPart) // 0.7 or -0.6
			ns := int64(1e9 * decimalPart)             // nanosecond part
			return time.Unix(integralPart, ns), nil
		}
		return defaultValue,
			fmt.Errorf("%v is out of bounds for int64 conversion", val)
	case TypeString:
		val, _ := v.asString()
		return time.Parse(time.RFC3339Nano, val)
	case TypeTimestamp:
		return v.asTimestamp()
	default:
		return defaultValue,
			fmt.Errorf("cannot convert %T to Time", v)
	}
}

// ToDuration converts a Value to time.Duration, if possible.
// The conversion rules are as follows:
//
//  * Null: 0
//	* Int: Converted to seconds (e.g. 3 is equal to 3 seconds)
//	* Float: Converted to seconds (e.g. 3.141592 equals 3s + 141ms + 592us)
//	* String: time.ParseDuration will be called
//	* other: (error)
func ToDuration(v Value) (time.Duration, error) {
	switch v.Type() {
	case TypeNull:
		var defaultValue time.Duration
		return defaultValue, nil
	case TypeInt:
		i, _ := v.asInt()
		return time.Duration(i) * time.Second, nil
	case TypeFloat:
		f, _ := v.asFloat()
		return time.Duration(f * float64(time.Second)), nil
	case TypeString:
		s, _ := v.asString()
		return time.ParseDuration(s)
	default:
		return 0, fmt.Errorf("cannot convert %T to Duration", v)
	}
}
