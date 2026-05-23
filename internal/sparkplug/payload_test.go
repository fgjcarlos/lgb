package sparkplug_test

import (
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/sparkplug"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

func TestBuildNBIRTH_SeqIsZero(t *testing.T) {
	t.Parallel()
	var seq sparkplug.SeqTracker
	for i := 0; i < 10; i++ {
		seq.Next()
	}

	data, err := sparkplug.BuildNBIRTH(&seq, nil)
	if err != nil {
		t.Fatalf("BuildNBIRTH returned error: %v", err)
	}

	var p pb.Payload
	if err := proto.Unmarshal(data, &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p.GetSeq() != 0 {
		t.Errorf("NBIRTH seq = %d; want 0", p.GetSeq())
	}
}

func TestBuildNBIRTH_RoundTrip(t *testing.T) {
	t.Parallel()
	var seq sparkplug.SeqTracker

	tags := []sparkplug.TagDef{
		{Name: "Motor.Speed", SparkplugType: "Float"},
		{Name: "Motor.Running", SparkplugType: "Boolean"},
	}

	data, err := sparkplug.BuildNBIRTH(&seq, tags)
	if err != nil {
		t.Fatalf("BuildNBIRTH returned error: %v", err)
	}

	var p pb.Payload
	if err := proto.Unmarshal(data, &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(p.Metrics) < 2 {
		t.Fatalf("expected at least 2 metrics, got %d", len(p.Metrics))
	}
}

func TestBuildNDEATH_CarriesBdSeq(t *testing.T) {
	t.Parallel()
	data, err := sparkplug.BuildNDEATH(3)
	if err != nil {
		t.Fatalf("BuildNDEATH returned error: %v", err)
	}

	var p pb.Payload
	if err := proto.Unmarshal(data, &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(p.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(p.Metrics))
	}
	m := p.Metrics[0]
	if m.GetName() != "bdSeq" {
		t.Errorf("metric name = %q; want %q", m.GetName(), "bdSeq")
	}
	if m.GetLongValue() != 3 {
		t.Errorf("bdSeq value = %d; want 3", m.GetLongValue())
	}
}

func TestBuildDBIRTH_ContainsAllMetrics(t *testing.T) {
	t.Parallel()
	tagValues := map[string]any{
		"Motor.Speed":   float32(1200.5),
		"Motor.Running": true,
		"Temp":          int32(72),
	}

	data, err := sparkplug.BuildDBIRTH("plc-a", tagValues, 1)
	if err != nil {
		t.Fatalf("BuildDBIRTH returned error: %v", err)
	}

	var p pb.Payload
	if err := proto.Unmarshal(data, &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(p.Metrics) != 3 {
		t.Errorf("expected 3 metrics, got %d", len(p.Metrics))
	}
}

func TestBuildDDATA_EncodesTagUpdate(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	updates := []sparkplug.TagUpdate{
		{PLCName: "plc-a", Tag: "Motor.Speed", Value: float32(1200.5), Timestamp: ts},
	}

	data, err := sparkplug.BuildDDATA(updates, 5)
	if err != nil {
		t.Fatalf("BuildDDATA returned error: %v", err)
	}

	var p pb.Payload
	if err := proto.Unmarshal(data, &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(p.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(p.Metrics))
	}
	if p.Metrics[0].GetName() != "Motor.Speed" {
		t.Errorf("metric name = %q; want %q", p.Metrics[0].GetName(), "Motor.Speed")
	}
	if p.GetSeq() != 5 {
		t.Errorf("DDATA seq = %d; want 5", p.GetSeq())
	}
}

func TestBuildDDEATH_EmptyMetrics(t *testing.T) {
	t.Parallel()
	data, err := sparkplug.BuildDDEATH("plc-a", 7)
	if err != nil {
		t.Fatalf("BuildDDEATH returned error: %v", err)
	}

	var p pb.Payload
	if err := proto.Unmarshal(data, &p); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(p.Metrics) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(p.Metrics))
	}
	if p.GetSeq() != 7 {
		t.Errorf("DDEATH seq = %d; want 7", p.GetSeq())
	}
}
