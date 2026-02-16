package p2p

import (
	"encoding/json"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
)

// FuzzHeartbeatUnmarshal tests that arbitrary JSON does not panic
// when unmarshaled into a HeartbeatMessage.
func FuzzHeartbeatUnmarshal(f *testing.F) {
	f.Add([]byte(`{"pubkey":"AQID","height":100,"timestamp":1700000000,"signature":"BAUG"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"pubkey":null,"height":0}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var msg HeartbeatMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}
		_ = msg.PubKey
		_ = msg.Height
		_ = msg.Timestamp
		_ = msg.Signature
	})
}

// FuzzBlockMessageUnmarshal tests that arbitrary JSON does not panic
// when unmarshaled as a gossip block message.
func FuzzBlockMessageUnmarshal(f *testing.F) {
	f.Add([]byte(`{"header":{"version":1,"timestamp":1000,"height":0},"transactions":[]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"header":null}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var blk block.Block
		if err := json.Unmarshal(data, &blk); err != nil {
			return
		}
		blk.Validate()
		blk.Hash()
	})
}

// FuzzTxMessageUnmarshal tests that arbitrary JSON does not panic
// when unmarshaled as a gossip transaction message.
func FuzzTxMessageUnmarshal(f *testing.F) {
	f.Add([]byte(`{"inputs":[],"outputs":[]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var t2 tx.Transaction
		if err := json.Unmarshal(data, &t2); err != nil {
			return
		}
		t2.Hash()
		t2.Validate()
	})
}
