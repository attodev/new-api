package doubao

import "strings"

var ModelList = []string{
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-5-pro-251215",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
}

var ChannelName = "doubao-video"

// videoPriceKey is the 2D price table key: output resolution tier and
// whether the input included video.
type videoPriceKey struct {
	is1080p  bool
	hasVideo bool
}

// videoPriceTable holds each model's unit price (per 1M tokens) for each
// (resolution tier, has-video-input) combination. The zero-value key
// {480p/720p, no video} is the baseline - it should equal the ModelRatio an
// admin configures. Billing takes actualPrice/basePrice as the OtherRatio;
// an unspecified resolution is treated as the 480p/720p tier.
var videoPriceTable = map[string]map[videoPriceKey]float64{
	"doubao-seedance-2-0-260128": {
		{is1080p: false, hasVideo: false}: 46.0,
		{is1080p: false, hasVideo: true}:  28.0,
		{is1080p: true, hasVideo: false}:  51.0,
		{is1080p: true, hasVideo: true}:   31.0,
	},
	"doubao-seedance-2-0-fast-260128": {
		{is1080p: false, hasVideo: false}: 37.0,
		{is1080p: false, hasVideo: true}:  22.0,
	},
}

// GetVideoInputRatio returns the billing multiplier (relative to the
// baseline price) for a model at the given output resolution and
// has-video-input flag. The second return value reports whether this model
// has a price table configured at all; a 1.0 ratio means the caller may
// skip applying this OtherRatio.
func GetVideoInputRatio(modelName, resolution string, hasVideo bool) (float64, bool) {
	prices, ok := videoPriceTable[modelName]
	if !ok {
		return 0, false
	}
	base := prices[videoPriceKey{}] // zero-value key = {480p/720p, no video} baseline
	if base <= 0 {
		return 0, false
	}
	price, ok := prices[videoPriceKey{is1080p: strings.EqualFold(resolution, "1080p"), hasVideo: hasVideo}]
	if !ok {
		// An unconfigured combination (e.g. the fast model has no 1080p
		// price) - bill at the baseline; the upstream will reject the
		// request itself if the combination is actually invalid.
		return 1.0, true
	}
	return price / base, true
}
