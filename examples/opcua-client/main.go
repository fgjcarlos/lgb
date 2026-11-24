// Command opcua-read performs one read from an LGB OPC UA server.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

func main() {
	endpoint := os.Getenv("OPCUA_ENDPOINT")
	if endpoint == "" {
		endpoint = "opc.tcp://127.0.0.1:4840"
	}
	// SecurityPolicy#None is suitable only for isolated development without PKI verification.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := opcua.NewClient(endpoint, opcua.SecurityMode(ua.MessageSecurityModeNone), opcua.SecurityPolicy(ua.SecurityPolicyURINone))
	if err != nil {
		panic(err)
	}
	if err := client.Connect(ctx); err != nil {
		panic(err)
	}
	defer client.Close(ctx)
	ns, err := client.NamespaceArray(ctx)
	if err != nil {
		panic(err)
	}
	index := -1
	for i, uri := range ns {
		if uri == "urn:lgb:plc:Line1" {
			index = i
			break
		}
	}
	if index < 0 {
		panic("PLC namespace urn:lgb:plc:Line1 not found; set this URI to the configured PLC name")
	}
	request := &ua.ReadRequest{MaxAge: 0, TimestampsToReturn: ua.TimestampsToReturnBoth, NodesToRead: []*ua.ReadValueID{{NodeID: ua.NewStringNodeID(uint16(index), "Temperature"), AttributeID: ua.AttributeIDValue}}}
	response, err := client.Read(ctx, request)
	if err != nil {
		panic(err)
	}
	if len(response.Results) == 0 || response.Results[0].Status != ua.StatusOK {
		panic("tag read failed; check the configured tag name")
	}
	fmt.Println(response.Results[0].Value.Value())
}
