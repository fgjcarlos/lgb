package server

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/aclstore"
	"github.com/fgjcarlos/lgb/internal/config"
)

// typedTags maps every Sparkplug scalar type to a writable tag on "Typed-1".
var typedTags = []string{
	"Boolean", "Int8", "Int16", "Int32", "Int64",
	"UInt8", "UInt16", "UInt32", "UInt64",
	"Float", "Double", "String",
}

// seedTypedPLC adds PLC "Typed-1" with one writable tag per scalar type (the
// tag is named after its type) and grants the operator role write on each.
func (s *writeTestServer) seedTypedPLC(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	tags := make([]config.TagDef, 0, len(typedTags))
	for _, typ := range typedTags {
		tags = append(tags, config.TagDef{Name: typ, Type: typ, Writable: true})
	}
	if err := s.plcStore.Create(ctx, config.PLC{
		Name:          "Typed-1",
		Address:       "127.0.0.1:44818",
		ScanRate:      "1s",
		SocketTimeout: "5s",
		Tags:          tags,
	}); err != nil {
		t.Fatalf("seed typed PLC: %v", err)
	}
	for _, typ := range typedTags {
		if err := s.aclStore.CreateRule(ctx, aclstore.ACLRule{
			Role: "operator", PLC: "Typed-1", Tag: typ, AllowWrite: true,
		}); err != nil {
			t.Fatalf("create ACL rule %s: %v", typ, err)
		}
	}
}

// TestHandleWriteTag_CoercesValueToDeclaredType verifies every Sparkplug scalar
// type reaches the driver as the Go type matching the tag's declared type.
// (issue #61, TWA-HTTP-3.1)
func TestHandleWriteTag_CoercesValueToDeclaredType(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()
	ts.seedTypedPLC(t)
	tok := ts.operatorToken(t)

	cases := []struct {
		tag  string
		body string
		want any
	}{
		{"Boolean", `{"value":true}`, true},
		{"Boolean", `{"value":false}`, false},
		{"Int8", `{"value":-128}`, int8(-128)},
		{"Int16", `{"value":32767}`, int16(32767)},
		{"Int32", `{"value":-2147483648}`, int32(math.MinInt32)},
		{"Int64", `{"value":9223372036854775807}`, int64(math.MaxInt64)},
		{"UInt8", `{"value":255}`, uint8(255)},
		{"UInt16", `{"value":65535}`, uint16(65535)},
		{"UInt32", `{"value":4294967295}`, uint32(math.MaxUint32)},
		{"UInt64", `{"value":18446744073709551615}`, uint64(math.MaxUint64)},
		{"Float", `{"value":2.5}`, float32(2.5)},
		{"Double", `{"value":2.5}`, 2.5},
		{"String", `{"value":"run"}`, "run"},
		{"UInt8", `{"value":-0}`, uint8(0)},
		{"Int16", `{"value":1e3}`, int16(1000)},
		{"Float", `{"value":3.4028234663852886e38}`, float32(math.MaxFloat32)},
		{"Int64", `{"value":9007199254740993.0}`, int64(9007199254740993)},
	}
	for _, tc := range cases {
		t.Run(tc.tag+"/"+tc.body, func(t *testing.T) {
			resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Typed-1", tc.tag), tc.body, tok)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			call, ok := ts.mgr.lastWrite()
			if !ok {
				t.Fatal("expected Manager.WriteTag to be called")
			}
			if call.PLC != "Typed-1" || call.Tag != tc.tag {
				t.Errorf("write target = %s/%s; want Typed-1/%s", call.PLC, call.Tag, tc.tag)
			}
			if !reflect.DeepEqual(call.Val, tc.want) {
				t.Errorf("written value = %#v (%T); want %#v (%T)", call.Val, call.Val, tc.want, tc.want)
			}
		})
	}
}

// TestHandleWriteTag_InvalidValue_400 verifies a value that cannot represent
// the declared type is rejected with 400 invalid_value, audited as a deny and
// never reaches the driver. (issue #61, TWA-HTTP-3.1, TWA-AUDIT-4.1)
func TestHandleWriteTag_InvalidValue_400(t *testing.T) {
	cases := []struct {
		tag     string
		body    string
		message string
	}{
		{"Float", `{"value":"2.5"}`, "value is not a valid Float: expected a number"},
		{"Boolean", `{"value":"true"}`, "value is not a valid Boolean: expected true or false"},
		{"Boolean", `{"value":1}`, "value is not a valid Boolean: expected true or false"},
		{"Int16", `{"value":2.5}`, "value is not a valid Int16: expected an integer between -32768 and 32767"},
		{"Int16", `{"value":32768}`, "value is not a valid Int16: expected an integer between -32768 and 32767"},
		{"UInt8", `{"value":-1}`, "value is not a valid UInt8: expected an integer between 0 and 255"},
		{"Int64", `{"value":9223372036854775808}`, "value is not a valid Int64: expected an integer between -9223372036854775808 and 9223372036854775807"},
		{"Float", `{"value":1e100}`, "value is not a valid Float: expected a number"},
		{"String", `{"value":1}`, "value is not a valid String: expected a string"},
		{"Double", `{"value":null}`, "value is not a valid Double: expected a number"},
		{"Double", `{}`, "value is not a valid Double: expected a number"},
		{"Double", `null`, "value is not a valid Double: expected a number"},
		{"Double", `{"value":[1]}`, "value is not a valid Double: expected a number"},
		{"String", `{"value":{"a":1}}`, "value is not a valid String: expected a string"},
		{"Int16", `{"value":1.00000000000000001}`, "value is not a valid Int16: expected an integer between -32768 and 32767"},
	}
	for _, tc := range cases {
		t.Run(tc.tag+"/"+tc.body, func(t *testing.T) {
			ts := newWriteTestServer(t)
			defer ts.stop()
			ts.seedTypedPLC(t)

			resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Typed-1", tc.tag), tc.body, ts.operatorToken(t))
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Error.Code != "invalid_value" {
				t.Errorf("code = %q; want invalid_value", body.Error.Code)
			}
			if body.Error.Message != tc.message {
				t.Errorf("message = %q; want %q", body.Error.Message, tc.message)
			}
			if call, ok := ts.mgr.lastWrite(); ok {
				t.Errorf("unexpected write call: %+v", call)
			}
			events := ts.readAuditEvents(t)
			if len(events) != 1 {
				t.Fatalf("audit events = %d; want 1", len(events))
			}
			for _, want := range []string{"plc=Typed-1", "tag=" + tc.tag, "outcome=deny", "source=http", "reason=invalid value"} {
				if !strings.Contains(events[0].Detail, want) {
					t.Errorf("audit Detail = %q; want it to contain %q", events[0].Detail, want)
				}
			}
			if events[0].Username != "op1" {
				t.Errorf("audit Username = %q; want op1", events[0].Username)
			}
		})
	}
}
