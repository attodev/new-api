package ali

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// TestOaiImage2AliImageRequest_RejectsOversizedParametersN reproduces a real
// billing-overflow bug: Extra["parameters"].n bypasses the standard
// top-level ImageRequest.N validation entirely (it's unmarshalled directly
// into the Ali-specific Parameters struct), so an oversized value here
// still becomes a billing multiplier (AddOtherRatio("n", ...)) with no
// bound at all.
func TestOaiImage2AliImageRequest_RejectsOversizedParametersN(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	req := dto.ImageRequest{
		Model: "wanx2.1-t2i-turbo",
		Extra: map[string]json.RawMessage{
			"parameters": json.RawMessage(`{"n":999999999}`),
		},
	}

	_, err := oaiImage2AliImageRequest(info, req, true)
	require.Error(t, err, "an oversized parameters.n must be rejected before it becomes a billing multiplier")
}

func TestOaiImage2AliImageRequest_RejectsNegativeParametersN(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	req := dto.ImageRequest{
		Model: "wanx2.1-t2i-turbo",
		Extra: map[string]json.RawMessage{
			"parameters": json.RawMessage(`{"n":-5}`),
		},
	}

	_, err := oaiImage2AliImageRequest(info, req, true)
	require.Error(t, err)
}
