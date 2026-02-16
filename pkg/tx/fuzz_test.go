package tx

import (
	"encoding/json"
	"testing"
)

// FuzzTxUnmarshal tests that arbitrary JSON input does not panic
// when unmarshaled into a Transaction struct.
func FuzzTxUnmarshal(f *testing.F) {
	f.Add([]byte(`{"inputs":[{"prev_out":{"tx_id":"0000000000000000000000000000000000000000000000000000000000000000","index":0}}],"outputs":[{"value":1000,"script":{"type":"p2pkh","data":"0000000000000000000000000000000000000000"}}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"inputs":null,"outputs":null}`))
	f.Add([]byte(`{"inputs":[{"prev_out":{"tx_id":"","index":0},"pub_key":"","signature":""}],"outputs":[{"value":0}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var tx Transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			return
		}
		// If unmarshal succeeded, these must not panic.
		tx.Hash()
		tx.SigningBytes()
		tx.Validate()
		tx.VerifySignatures() // May fail but must not panic.
	})
}
