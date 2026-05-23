package sparkplug_test

import (
	"errors"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/sparkplug"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

func TestEncodeMetric_Phase1Types(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		value    any
		wantType uint32
	}{
		{"bool", true, 11},
		{"int8", int8(42), 1},
		{"int16", int16(1000), 2},
		{"int32", int32(100000), 3},
		{"int64", int64(9999999), 4},
		{"uint8", uint8(255), 5},
		{"uint16", uint16(65535), 6},
		{"uint32", uint32(4294967295), 7},
		{"uint64", uint64(18446744073709551615), 8},
		{"float32", float32(3.14), 9},
		{"float64", float64(2.718281828), 10},
		{"string", "hello", 12},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := sparkplug.EncodeMetric("tag", tc.value, ts)
			if err != nil {
				t.Fatalf("EncodeMetric(%T) returned error: %v", tc.value, err)
			}
			if m.GetDatatype() != tc.wantType {
				t.Errorf("DataType = %d; want %d", m.GetDatatype(), tc.wantType)
			}
			if m.GetName() != "tag" {
				t.Errorf("Name = %q; want %q", m.GetName(), "tag")
			}

			// Round-trip through protobuf.
			payload := &pb.Payload{Metrics: []*pb.Payload_Metric{m}}
			data, err := proto.Marshal(payload)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var decoded pb.Payload
			if err := proto.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if len(decoded.Metrics) != 1 {
				t.Fatalf("decoded metrics count = %d; want 1", len(decoded.Metrics))
			}
			if decoded.Metrics[0].GetDatatype() != tc.wantType {
				t.Errorf("round-trip DataType = %d; want %d", decoded.Metrics[0].GetDatatype(), tc.wantType)
			}
		})
	}
}

func TestEncodeMetric_UnsupportedType(t *testing.T) {
	t.Parallel()
	_, err := sparkplug.EncodeMetric("tag", []byte{1, 2, 3}, time.Now())
	if err == nil {
		t.Fatal("expected error for unsupported type []byte, got nil")
	}
	if !errors.Is(err, sparkplug.ErrSparkplugEncode) {
		t.Errorf("expected ErrSparkplugEncode, got %v", err)
	}
}
