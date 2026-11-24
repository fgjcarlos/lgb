package main

import (
	"testing"

	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

func TestBrokerURLRequiresTLSHostnameWithoutUserinfo(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{{"ssl://broker.example:8883", true}, {"tcp://broker.example:1883", false}, {"ssl://user:pass@broker.example:8883", false}, {"ssl://:8883", false}} {
		_, err := brokerAddress(tc.url)
		if (err == nil) != tc.valid {
			t.Errorf("brokerAddress(%q) err=%v, valid=%v", tc.url, err, tc.valid)
		}
	}
}

func TestNumericNamedSparkplugMetrics(t *testing.T) {
	name, typ := "Temperature", uint32(10)
	payload, err := proto.Marshal(&pb.Payload{Metrics: []*pb.Payload_Metric{{Name: &name, Datatype: &typ, Value: &pb.Payload_Metric_DoubleValue{DoubleValue: 21.5}}}})
	if err != nil {
		t.Fatal(err)
	}
	v := &samples{values: map[string]sample{}}
	if err := numeric("spBv1.0/Factory/DDATA/LGB/Line1", payload, v); err != nil {
		t.Fatal(err)
	}
	if got := v.values["Factory\x00LGB\x00Line1\x00Temperature"].value; got != 21.5 {
		t.Fatalf("Temperature = %v, want 21.5", got)
	}
	if err := numeric("spBv1.0/Factory/DDATA/LGB/Line1", []byte("invalid protobuf"), v); err == nil {
		t.Fatal("malformed payload accepted")
	}
}
