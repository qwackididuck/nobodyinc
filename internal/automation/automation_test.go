package automation

import (
	"errors"
	"testing"
)

func TestSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		to          Level
		wantErr     error
		wantChanged bool
		wantAuto    bool
	}{
		{"on", Auto, nil, true, true},
		{"already off", Off, nil, false, false},
		{"unknown", "yolo", ErrUnknownLevel, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New()
			calls := 0
			m.OnChange = func() { calls++ }

			changed, err := m.Set(tt.to)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}

			if changed != tt.wantChanged || m.AutoApprove() != tt.wantAuto {
				t.Errorf("changed=%v auto=%v, want %v %v", changed, m.AutoApprove(), tt.wantChanged, tt.wantAuto)
			}

			if (calls == 1) != tt.wantChanged {
				t.Errorf("OnChange called %d times", calls)
			}
		})
	}
}

func TestStartsOff(t *testing.T) {
	t.Parallel()

	if New().Level() != Off {
		t.Fatal("a new Mode must be off")
	}
}
