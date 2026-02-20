package handlers

import (
	"testing"
)

func TestChangeDeltaPct(t *testing.T) {
	tests := []struct {
		name      string
		c         ChangeRow
		wantApprox float64 // expected value (exact for integer results)
	}{
		{
			name:       "both old prices zero returns zero",
			c:          ChangeRow{OldInput: 0, OldOutput: 0, NewInput: 5, NewOutput: 10},
			wantApprox: 0,
		},
		{
			name:       "old output zero - only input used for delta",
			c:          ChangeRow{OldInput: 3, OldOutput: 0, NewInput: 3, NewOutput: 15},
			wantApprox: 0, // input unchanged; output going $0→$15 is new data, not a change
		},
		{
			name:       "old input zero - only output used for delta",
			c:          ChangeRow{OldInput: 0, OldOutput: 10, NewInput: 2, NewOutput: 10},
			wantApprox: 0, // output unchanged; input going $0→$2 is new data, not a change
		},
		{
			name:       "both fields non-zero, prices unchanged",
			c:          ChangeRow{OldInput: 3, OldOutput: 15, NewInput: 3, NewOutput: 15},
			wantApprox: 0,
		},
		{
			name:       "both fields non-zero, output doubles",
			c:          ChangeRow{OldInput: 3, OldOutput: 3, NewInput: 3, NewOutput: 6},
			wantApprox: 50, // total: 6→9 = +50%
		},
		{
			name:       "both fields increase",
			c:          ChangeRow{OldInput: 1, OldOutput: 1, NewInput: 2, NewOutput: 2},
			wantApprox: 100, // total doubles
		},
		{
			name:       "price decrease",
			c:          ChangeRow{OldInput: 10, OldOutput: 10, NewInput: 5, NewOutput: 5},
			wantApprox: -50,
		},
		{
			name: "typical grok-3-beta case: output was $0, should not inflate delta",
			// Before the fix this would have been +500%
			c:          ChangeRow{OldInput: 3, OldOutput: 0, NewInput: 3, NewOutput: 15},
			wantApprox: 0,
		},
		{
			name: "typical o4-mini case: output was $0, should not inflate delta",
			// Before the fix this would have been +400%
			c:          ChangeRow{OldInput: 1.1, OldOutput: 0, NewInput: 1.1, NewOutput: 4.4},
			wantApprox: 0,
		},
		{
			name: "genuine large increase: input goes from tiny to large",
			c:    ChangeRow{OldInput: 0.01, OldOutput: 0.01, NewInput: 20, NewOutput: 20},
			// (40 - 0.02) / 0.02 * 100 = 199900%
			wantApprox: 199900,
		},
	}

	const epsilon = 0.01
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changeDeltaPct(tt.c)
			diff := got - tt.wantApprox
			if diff < 0 {
				diff = -diff
			}
			if diff > epsilon {
				t.Errorf("changeDeltaPct(%+v) = %.4f, want %.4f", tt.c, got, tt.wantApprox)
			}
		})
	}
}
