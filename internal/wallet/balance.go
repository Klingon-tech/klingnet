package wallet

// Balance tracks UTXO balances for an address.
type Balance struct {
	Confirmed   uint64
	Unconfirmed uint64
}
