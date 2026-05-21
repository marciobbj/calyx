# Implementation Task List - Calyx Production Upgrades

We are implementing and validating binary SGX quote attestation, a physical weights loader, UDP STUN NAT traversal, and a model discovery directory.

- `[x]` Implement Binary SGX Quote serialization and verification
  - `[x]` Define the `SGXQuote` struct representing standard SGX/SEV binary fields in `crypto/tee.go`
  - `[x]` Implement `SerializeSGXQuote` and `DeserializeSGXQuote` using network byte order (BigEndian)
  - `[x]` Integrate serialization and deserialization in `GenerateAttestationReport` and `VerifyAttestationReport`
- `[x]` Implement UDP STUN Client for NAT Discovery
  - `[x]` Create `crypto/stun.go` with `GetExternalIPMappedAddress` querying a public STUN server via UDP
  - `[x]` Integrate STUN address querying in `server/server.go` startup logs
- `[x]` Implement Physical Model Weights Loading
  - `[x]` Create `engine/weights.go` defining the custom binary weight file format (`CALYXW`)
  - `[x]` Implement `SaveWeights` and `LoadWeights` reading/writing layer weight matrices
  - `[x]` Implement `EnsureWeightsExist` to dynamically generate stable default weights if the file is absent
  - `[x]` Hook weight loading into `server/server.go` using a `-weights` flag
- `[x]` Implement Model Discovery Directory
  - `[x]` Modify `bootstrap/bootstrap.go` to store node registrations segmented by `model-id` via gRPC metadata headers
  - `[x]` Modify `server/server.go` to pass its `-model` ID during Bootstrap registration metadata
  - `[x]` Update `client/client.go` to query model-specific routes and fetch/list the global network model catalog
  - `[x]` Update `main.go` to support `-weights`, `-model`, `-stun-server` flags and `-mode=list-models` CLI command
- `[x]` Create Upgrade Test Suite & Verify
  - `[x]` Write `tests/production_upgrades_test.go` to validate binary quotes, weight files, STUN UDP client, and model directory routing
  - `[x]` Run `make all` to verify all tests pass green
  - `[x]` Update walkthrough documentation in `walkthrough.md`
