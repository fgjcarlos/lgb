# Generic read-only OPC UA client

`main.go` is a runnable Go client using the repository's existing `gopcua` dependency. From the repository root, run it against an isolated development server with OPC UA SecurityPolicy#None:

```sh
OPCUA_ENDPOINT=opc.tcp://127.0.0.1:4840 go run ./examples/opcua-client
```

The default endpoint is `opc.tcp://127.0.0.1:4840` (the server default; `opcua.port` can change it). The snippet only issues a Value read. Change the PLC name `Line1` and tag name `Temperature` in `main.go` to match your configuration. It discovers the namespace index from the live NamespaceArray and uses LGB's URI `urn:lgb:plc:<PLC name>`; namespace indexes must not be hard-coded.

**None security is only for isolated development and provides no PKI verification.** Production must configure the server for its certificate-backed security mode (`opcua.securityMode`, `opcua.certFile`, `opcua.keyFile`), trust and verify its certificate and application URI/hostname, and use a least-privilege read-only user if the deployed server has user authentication enabled. Do not assume this anonymous example server supports user accounts: do not add a username/password to it unless authentication has been explicitly configured. Never disable certificate validation in production.

Run only against a test gateway/simulator first. Inspect the configured OPC UA security mode and provision trust before using this sample in production.
