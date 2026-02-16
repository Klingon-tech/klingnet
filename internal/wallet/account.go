package wallet

import "github.com/Klingon-tech/klingnet-chain/pkg/types"

// Account represents a wallet account.
type Account struct {
	Index   uint32
	Name    string
	Address types.Address
}
