package rpc

import (
	"encoding/json"
	"testing"
)

// FuzzRPCRequestUnmarshal tests that arbitrary JSON does not panic
// when parsed as a JSON-RPC 2.0 request.
func FuzzRPCRequestUnmarshal(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","method":"chain_getInfo","params":null,"id":1}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"chain_getBlock","params":{"hash":"abc"},"id":"test"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"method":"","params":[]}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"chain_getInfo","params":[1,2,3],"id":999}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			return
		}
		_ = req.Method
		_ = req.ID
	})
}
