// Command bridge decodes LGB's named numeric Sparkplug DDATA metrics for Prometheus.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	paho "github.com/eclipse/paho.mqtt.golang"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

type sample struct {
	group, node, device, metric string
	value                       float64
}
type samples struct {
	sync.RWMutex
	values map[string]sample
}

func brokerAddress(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ssl" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("MQTT_BROKER must be ssl://hostname[:port] without userinfo")
	}
	return u, nil
}

func numeric(topic string, payload []byte, values *samples) error {
	var p pb.Payload
	if err := proto.Unmarshal(payload, &p); err != nil {
		return err
	}
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[0] != "spBv1.0" || parts[2] != "DDATA" {
		return fmt.Errorf("unexpected Sparkplug topic %q", topic)
	}
	values.Lock()
	defer values.Unlock()
	for _, m := range p.GetMetrics() {
		if m.GetName() == "" || m.GetIsNull() {
			continue
		}
		var n float64
		if m.Datatype == nil {
			continue
		}
		switch typ := *m.Datatype; typ {
		case 1:
			if v, ok := m.GetValue().(*pb.Payload_Metric_IntValue); ok {
				n = float64(int8(v.IntValue))
			} else {
				continue
			}
		case 2:
			if v, ok := m.GetValue().(*pb.Payload_Metric_IntValue); ok {
				n = float64(int16(v.IntValue))
			} else {
				continue
			}
		case 3:
			if v, ok := m.GetValue().(*pb.Payload_Metric_IntValue); ok {
				n = float64(int32(v.IntValue))
			} else {
				continue
			}
		case 4:
			if v, ok := m.GetValue().(*pb.Payload_Metric_LongValue); ok {
				n = float64(int64(v.LongValue))
			} else {
				continue
			}
		case 5, 6, 7:
			if v, ok := m.GetValue().(*pb.Payload_Metric_IntValue); ok {
				n = float64(v.IntValue)
			} else {
				continue
			}
		case 8:
			if v, ok := m.GetValue().(*pb.Payload_Metric_LongValue); ok {
				n = float64(v.LongValue)
			} else {
				continue
			}
		case 9:
			if v, ok := m.GetValue().(*pb.Payload_Metric_FloatValue); ok {
				n = float64(v.FloatValue)
			} else {
				continue
			}
		case 10:
			if v, ok := m.GetValue().(*pb.Payload_Metric_DoubleValue); ok {
				n = v.DoubleValue
			} else {
				continue
			}
		default:
			continue
		}
		key := strings.Join([]string{parts[1], parts[3], parts[4], m.GetName()}, "\x00")
		values.values[key] = sample{parts[1], parts[3], parts[4], m.GetName(), n}
	}
	return nil
}

func main() {
	ca, err := os.ReadFile(os.Getenv("MQTT_CA"))
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		panic("MQTT_CA contains no certificates")
	}
	v := &samples{values: map[string]sample{}}
	broker := os.Getenv("MQTT_BROKER") // e.g. ssl://broker.example:8883
	if _, err := brokerAddress(broker); err != nil {
		panic(err)
	}
	client := paho.NewClient(paho.NewClientOptions().AddBroker(broker).SetClientID("lgb-prometheus-readonly").SetTLSConfig(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}).SetUsername(os.Getenv("MQTT_USER")).SetPassword(os.Getenv("MQTT_PASSWORD")))
	if t := client.Connect(); !t.Wait() || t.Error() != nil {
		panic(fmt.Sprint("MQTT connect: ", t.Error()))
	}
	defer client.Disconnect(250)
	if t := client.Subscribe("spBv1.0/+/DDATA/+/+", 0, func(_ paho.Client, m paho.Message) {
		if err := numeric(m.Topic(), m.Payload(), v); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}); !t.Wait() || t.Error() != nil {
		panic(t.Error())
	}
	http.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		v.RLock()
		defer v.RUnlock()
		for _, s := range v.values {
			fmt.Fprintf(w, "lgb_sparkplug_metric{group=%q,node=%q,device=%q,metric=%q} %s\n", s.group, s.node, s.device, s.metric, strconv.FormatFloat(s.value, 'g', -1, 64))
		}
	})
	if err := http.ListenAndServe("127.0.0.1:9108", nil); err != nil {
		panic(err)
	}
}
