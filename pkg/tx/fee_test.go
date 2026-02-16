package tx

import "testing"

func TestEstimateTxFee(t *testing.T) {
	tests := []struct {
		name       string
		numInputs  int
		numOutputs int
		feeRate    uint64
		want       uint64
	}{
		{"zero rate", 1, 2, 0, 0},
		{"simple 1-in 2-out", 1, 2, 10, (20 + 36 + 66) * 10},   // 122 * 10 = 1220
		{"2-in 2-out", 2, 2, 10, (20 + 72 + 66) * 10},           // 158 * 10 = 1580
		{"consolidate 10-in 1-out", 10, 1, 10, (20 + 360 + 33) * 10}, // 413 * 10 = 4130
		{"rate 1", 1, 1, 1, 20 + 36 + 33},                       // 89
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTxFee(tt.numInputs, tt.numOutputs, tt.feeRate)
			if got != tt.want {
				t.Errorf("EstimateTxFee(%d, %d, %d) = %d, want %d",
					tt.numInputs, tt.numOutputs, tt.feeRate, got, tt.want)
			}
		})
	}
}
