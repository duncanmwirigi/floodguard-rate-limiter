package stepup_test

import (
	"context"
	"testing"

	"github.com/ultimateprogrammer/floodguard/stepup"
)

func TestDefaultAssessor_Matrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		known           bool
		velocityFlagged bool
		want            stepup.RiskLevel
		wantStepUp      bool
	}{
		{"known clean", true, false, stepup.RiskLow, false},
		{"known velocity", true, true, stepup.RiskMedium, true},
		{"unknown clean", false, false, stepup.RiskMedium, true},
		{"unknown velocity", false, true, stepup.RiskHigh, true},
	}

	a := stepup.DefaultAssessor{}
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := a.Assess(ctx, stepup.Signals{
				AccountID: "a1", KnownDevice: tt.known, VelocityFlagged: tt.velocityFlagged,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("risk = %v, want %v", got, tt.want)
			}
			if got.RequiresStepUp() != tt.wantStepUp {
				t.Fatalf("step-up = %v, want %v", got.RequiresStepUp(), tt.wantStepUp)
			}
		})
	}
}

func TestManager_CheckAndVerify(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := stepup.NewManager(nil, nil)

	signals := stepup.Signals{AccountID: "a1", Action: "withdraw", KnownDevice: false}
	_, _, err := mgr.Check(ctx, signals, "")
	if err != stepup.ErrStepUpRequired {
		t.Fatalf("expected step-up required, got %v", err)
	}

	// Simulate OTP completion by issuing then verifying via store directly not exposed;
	// use second Check with token from Issue path by calling Check twice - first gets token in error response path
	// Instead test medium risk with token flow via memory store
	store := stepup.NewMemoryChallengeStore()
	mgr = stepup.NewManager(nil, store)
	token, level, err := mgr.Check(ctx, signals, "")
	if err != stepup.ErrStepUpRequired || token == "" || level != stepup.RiskMedium {
		t.Fatalf("token=%q level=%v err=%v", token, level, err)
	}
	_, _, err = mgr.Check(ctx, signals, token)
	if err != nil {
		t.Fatalf("verified token should pass: %v", err)
	}
}
