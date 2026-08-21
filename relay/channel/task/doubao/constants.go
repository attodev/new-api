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

// videoPriceKey is the price table key: output resolution tier (is1080p and
// is4k both false means 480p/720p, the baseline tier) and whether the input
// included video.
type videoPriceKey struct {
	is1080p  bool
	is4k     bool
	hasVideo bool
}

// videoPriceTable holds each model's unit price (per 1M tokens) for each
// (resolution tier, has-video-input) combination. The zero-value key
// {480p/720p, no video} is the baseline - it should equal the ModelRatio an
// admin configures. Billing takes actualPrice/basePrice as the OtherRatio.
var videoPriceTable = map[string]map[videoPriceKey]float64{
	"doubao-seedance-2-0-260128": {
		{hasVideo: false}:                46.0,
		{hasVideo: true}:                 28.0,
		{is1080p: true, hasVideo: false}: 51.0,
		{is1080p: true, hasVideo: true}:  31.0,
		{is4k: true, hasVideo: false}:    26.0,
		{is4k: true, hasVideo: true}:     16.0,
	},
	"doubao-seedance-2-0-fast-260128": {
		{hasVideo: false}: 37.0,
		{hasVideo: true}:  22.0,
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
	res := strings.ToLower(strings.TrimSpace(resolution))
	price, ok := prices[videoPriceKey{is1080p: res == "1080p", is4k: res == "4k", hasVideo: hasVideo}]
	if !ok {
		// An unconfigured combination (e.g. the fast model has no 1080p/4k
		// price) - bill at the baseline; the upstream will reject the
		// request itself if the combination is actually invalid.
		return 1.0, true
	}
	return price / base, true
}
