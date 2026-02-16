package block

import (
	"encoding/json"
	"testing"
)

// FuzzBlockUnmarshal tests that arbitrary JSON input does not panic
// when unmarshaled into a Block struct.
func FuzzBlockUnmarshal(f *testing.F) {
	// Seed with a minimal valid block JSON.
	f.Add([]byte(`{"header":{"version":1,"prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","merkle_root":"0000000000000000000000000000000000000000000000000000000000000000","timestamp":1000,"height":0},"transactions":[]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"header":null}`))
	f.Add([]byte(`{"header":{"version":99999},"transactions":[{"inputs":[],"outputs":[]}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var blk Block
		if err := json.Unmarshal(data, &blk); err != nil {
			return // Invalid JSON is expected.
		}
		// If unmarshal succeeded, Validate and Hash must not panic.
		blk.Validate()
		blk.Hash()
	})
}

// FuzzBlockHeaderUnmarshal tests that arbitrary JSON input does not panic
// when unmarshaled into a Header struct.
func FuzzBlockHeaderUnmarshal(f *testing.F) {
	f.Add([]byte(`{"version":1,"timestamp":1000,"height":0}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"difficulty":18446744073709551615}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var h Header
		if err := json.Unmarshal(data, &h); err != nil {
			return
		}
		h.Hash()
		h.SigningBytes()
	})
}
