package ali

import (
	"github.com/QuantumNous/new-api/dto"
	"github.com/samber/lo"
)

// https://help.aliyun.com/document_detail/613695.html?spm=a2c4g.2399480.0.0.1adb778fAdzP9w#341800c0f8w0r

const EnableSearchModelSuffix = "-internet"

func requestOpenAI2Ali(request dto.GeneralOpenAIRequest) *dto.GeneralOpenAIRequest {
	// A request that omits top_p is left untouched: reading it via
	// lo.FromPtrOr(request.TopP, 0) treats "not set" the same as
	// "explicitly 0" and would force near-greedy decoding the client never
	// asked for. DashScope also rejects top_p at the 0 and 1 boundaries, so
	// an explicit value there is clamped into the open interval.
	if request.TopP != nil {
		if *request.TopP >= 1 {
			request.TopP = lo.ToPtr(0.999)
		} else if *request.TopP <= 0 {
			request.TopP = lo.ToPtr(0.001)
		}
	}
	return &request
}
