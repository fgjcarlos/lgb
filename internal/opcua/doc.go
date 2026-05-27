// Package opcua provides an OPC UA server that exposes PLC tag data
// to SCADA/HMI clients using gopcua/opcua.
//
// The server creates one namespace per PLC and maps each tag to an
// OPC UA variable node. Tag values are updated from the PLC Manager's
// in-memory tag store.
//
// Security modes (None, Sign, SignAndEncrypt) and username/password
// authentication are configurable via lgb.yaml.
package opcua
