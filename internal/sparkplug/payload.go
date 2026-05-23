package sparkplug

import (
	"time"

	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

// TagDef defines a PLC tag to Sparkplug metric mapping.
type TagDef struct {
	Name          string
	SparkplugType string
}

// TagUpdate represents a single tag read from a PLC scan tick.
type TagUpdate struct {
	PLCName   string
	Tag       string
	Value     any
	Timestamp time.Time
}

// BuildNBIRTH produces the Sparkplug B NBIRTH payload. The sequence tracker
// is reset and advanced so seq=0 per the Sparkplug B specification.
func BuildNBIRTH(seq *SeqTracker, tags []TagDef, ) ([]byte, error) {
	seq.Reset()
	seqVal := seq.Next()
	now := uint64(time.Now().UnixMilli())

	payload := &pb.Payload{
		Timestamp: &now,
		Seq:       &seqVal,
	}

	for _, tag := range tags {
		m := &pb.Payload_Metric{
			Name:      strPtr(tag.Name),
			Timestamp: &now,
		}
		dt := sparkplugTypeToDataType(tag.SparkplugType)
		m.Datatype = &dt
		payload.Metrics = append(payload.Metrics, m)
	}

	return proto.Marshal(payload)
}

// BuildNDEATH produces the NDEATH payload with a single bdSeq metric.
func BuildNDEATH(bdSeq uint64) ([]byte, error) {
	now := uint64(time.Now().UnixMilli())
	name := "bdSeq"
	dt := dtUInt64

	payload := &pb.Payload{
		Timestamp: &now,
		Metrics: []*pb.Payload_Metric{
			{
				Name:     &name,
				Datatype: &dt,
				Value:    &pb.Payload_Metric_LongValue{LongValue: bdSeq},
			},
		},
	}
	return proto.Marshal(payload)
}

// BuildDBIRTH produces a DBIRTH payload with all tag metrics for a device.
func BuildDBIRTH(deviceID string, tagValues map[string]any, seq uint64) ([]byte, error) {
	now := uint64(time.Now().UnixMilli())
	ts := time.Now()

	payload := &pb.Payload{
		Timestamp: &now,
		Seq:       &seq,
	}

	for name, value := range tagValues {
		m, err := EncodeMetric(name, value, ts)
		if err != nil {
			return nil, err
		}
		payload.Metrics = append(payload.Metrics, m)
	}
	return proto.Marshal(payload)
}

// BuildDDATA produces a DDATA payload from tag updates.
func BuildDDATA(updates []TagUpdate, seq uint64) ([]byte, error) {
	now := uint64(time.Now().UnixMilli())

	payload := &pb.Payload{
		Timestamp: &now,
		Seq:       &seq,
	}

	for _, u := range updates {
		m, err := EncodeMetric(u.Tag, u.Value, u.Timestamp)
		if err != nil {
			return nil, err
		}
		payload.Metrics = append(payload.Metrics, m)
	}
	return proto.Marshal(payload)
}

// BuildDDEATH produces a DDEATH payload with empty metrics.
func BuildDDEATH(deviceID string, seq uint64) ([]byte, error) {
	now := uint64(time.Now().UnixMilli())
	payload := &pb.Payload{
		Timestamp: &now,
		Seq:       &seq,
	}
	return proto.Marshal(payload)
}

func strPtr(s string) *string { return &s }

var sparkplugTypeMap = map[string]uint32{
	"Boolean": dtBoolean,
	"Int8":    dtInt8, "Int16": dtInt16, "Int32": dtInt32, "Int64": dtInt64,
	"UInt8":   dtUInt8, "UInt16": dtUInt16, "UInt32": dtUInt32, "UInt64": dtUInt64,
	"Float":   dtFloat, "Double": dtDouble, "String": dtString,
}

func sparkplugTypeToDataType(typeName string) uint32 {
	if dt, ok := sparkplugTypeMap[typeName]; ok {
		return dt
	}
	return 0
}
