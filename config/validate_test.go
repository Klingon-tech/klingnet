package config

import "testing"

func TestValidate_RPCAllowedIPs_Invalid(t *testing.T) {
	cfg := DefaultMainnet()
	cfg.RPC.AllowedIPs = []string{"127.0.0.1", "not-an-ip"}

	if err := Validate(cfg); err == nil {
		t.Fatal("Validate() should fail for invalid rpc.allowed entry")
	}
}

func TestValidate_RPCAllowedIPs_Valid(t *testing.T) {
	cfg := DefaultMainnet()
	cfg.RPC.AllowedIPs = []string{"127.0.0.1", "10.0.0.0/8", "::1"}

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}
