package gemini

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"testing"
)

// TestResolveVeoDuration_CapsAtMaxTaskDurationSeconds reproduces a real
// billing-overflow bug: the metadata["durationSeconds"] path bypasses
// standard request validation entirely, so an attacker-controlled duration
// used directly as a billing multiplier had no upper bound.
func TestResolveVeoDuration_CapsAtMaxTaskDurationSeconds(t *testing.T) {
	metadata := map[string]any{"durationSeconds": float64(999999999)}
	got := ResolveVeoDuration(metadata, 0, "")
	if got > relaycommon.MaxTaskDurationSeconds {
		t.Fatalf("ResolveVeoDuration = %d, want capped at %d", got, relaycommon.MaxTaskDurationSeconds)
	}
}

func TestResolveVeoDuration_CapsStdDuration(t *testing.T) {
	got := ResolveVeoDuration(nil, relaycommon.MaxTaskDurationSeconds+1000, "")
	if got > relaycommon.MaxTaskDurationSeconds {
		t.Fatalf("ResolveVeoDuration = %d, want capped at %d", got, relaycommon.MaxTaskDurationSeconds)
	}
}

func TestResolveVeoDuration_CapsStdSeconds(t *testing.T) {
	got := ResolveVeoDuration(nil, 0, "999999999")
	if got > relaycommon.MaxTaskDurationSeconds {
		t.Fatalf("ResolveVeoDuration = %d, want capped at %d", got, relaycommon.MaxTaskDurationSeconds)
	}
}

func TestResolveVeoDuration_NormalValueUnaffected(t *testing.T) {
	got := ResolveVeoDuration(nil, 8, "")
	if got != 8 {
		t.Fatalf("ResolveVeoDuration = %d, want 8", got)
	}
}
