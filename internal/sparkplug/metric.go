package sparkplug

import (
	"fmt"
	"time"

	errs "github.com/fgjcarlos/lgb/internal/errors"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
)

var ErrSparkplugEncode = errs.ErrSparkplugEncode

// Sparkplug B DataType enum values.
const (
	dtInt8    uint32 = 1
	dtInt16   uint32 = 2
	dtInt32   uint32 = 3
	dtInt64   uint32 = 4
	dtUInt8   uint32 = 5
	dtUInt16  uint32 = 6
	dtUInt32  uint32 = 7
	dtUInt64  uint32 = 8
	dtFloat   uint32 = 9
	dtDouble  uint32 = 10
	dtBoolean uint32 = 11
	dtString  uint32 = 12
)

// EncodeMetric creates a Sparkplug B Metric from a Go value.
// Supports Phase 1 scalar types only.
func EncodeMetric(name string, value any, ts time.Time) (*pb.Payload_Metric, error) {
	m := &pb.Payload_Metric{
		Name:      &name,
		Timestamp: uint64Ptr(uint64(ts.UnixMilli())),
	}

	switch v := value.(type) {
	case bool:
		m.Datatype = uint32Ptr(dtBoolean)
		m.Value = &pb.Payload_Metric_BooleanValue{BooleanValue: v}
	case int8:
		m.Datatype = uint32Ptr(dtInt8)
		m.Value = &pb.Payload_Metric_IntValue{IntValue: uint32(v)}
	case int16:
		m.Datatype = uint32Ptr(dtInt16)
		m.Value = &pb.Payload_Metric_IntValue{IntValue: uint32(v)}
	case int32:
		m.Datatype = uint32Ptr(dtInt32)
		m.Value = &pb.Payload_Metric_IntValue{IntValue: uint32(v)}
	case int64:
		m.Datatype = uint32Ptr(dtInt64)
		m.Value = &pb.Payload_Metric_LongValue{LongValue: uint64(v)}
	case uint8:
		m.Datatype = uint32Ptr(dtUInt8)
		m.Value = &pb.Payload_Metric_IntValue{IntValue: uint32(v)}
	case uint16:
		m.Datatype = uint32Ptr(dtUInt16)
		m.Value = &pb.Payload_Metric_IntValue{IntValue: uint32(v)}
	case uint32:
		m.Datatype = uint32Ptr(dtUInt32)
		m.Value = &pb.Payload_Metric_IntValue{IntValue: v}
	case uint64:
		m.Datatype = uint32Ptr(dtUInt64)
		m.Value = &pb.Payload_Metric_LongValue{LongValue: v}
	case float32:
		m.Datatype = uint32Ptr(dtFloat)
		m.Value = &pb.Payload_Metric_FloatValue{FloatValue: v}
	case float64:
		m.Datatype = uint32Ptr(dtDouble)
		m.Value = &pb.Payload_Metric_DoubleValue{DoubleValue: v}
	case string:
		m.Datatype = uint32Ptr(dtString)
		m.Value = &pb.Payload_Metric_StringValue{StringValue: v}
	default:
		return nil, fmt.Errorf("sparkplug: unsupported type %T for metric %q: %w", value, name, ErrSparkplugEncode)
	}
	return m, nil
}

// DecodeMetricValue extracts a Go value from a Sparkplug B Metric.
func DecodeMetricValue(m *pb.Payload_Metric) any {
	if m == nil || m.Datatype == nil {
		return nil
	}
	switch *m.Datatype {
	case dtBoolean:
		if v, ok := m.Value.(*pb.Payload_Metric_BooleanValue); ok {
			return v.BooleanValue
		}
	case dtInt8:
		if v, ok := m.Value.(*pb.Payload_Metric_IntValue); ok {
			return int8(v.IntValue)
		}
	case dtInt16:
		if v, ok := m.Value.(*pb.Payload_Metric_IntValue); ok {
			return int16(v.IntValue)
		}
	case dtInt32:
		if v, ok := m.Value.(*pb.Payload_Metric_IntValue); ok {
			return int32(v.IntValue)
		}
	case dtInt64:
		if v, ok := m.Value.(*pb.Payload_Metric_LongValue); ok {
			return int64(v.LongValue)
		}
	case dtUInt8:
		if v, ok := m.Value.(*pb.Payload_Metric_IntValue); ok {
			return uint8(v.IntValue)
		}
	case dtUInt16:
		if v, ok := m.Value.(*pb.Payload_Metric_IntValue); ok {
			return uint16(v.IntValue)
		}
	case dtUInt32:
		if v, ok := m.Value.(*pb.Payload_Metric_IntValue); ok {
			return v.IntValue
		}
	case dtUInt64:
		if v, ok := m.Value.(*pb.Payload_Metric_LongValue); ok {
			return v.LongValue
		}
	case dtFloat:
		if v, ok := m.Value.(*pb.Payload_Metric_FloatValue); ok {
			return v.FloatValue
		}
	case dtDouble:
		if v, ok := m.Value.(*pb.Payload_Metric_DoubleValue); ok {
			return v.DoubleValue
		}
	case dtString:
		if v, ok := m.Value.(*pb.Payload_Metric_StringValue); ok {
			return v.StringValue
		}
	}
	return nil
}

func uint32Ptr(v uint32) *uint32 { return &v }
func uint64Ptr(v uint64) *uint64 { return &v }
