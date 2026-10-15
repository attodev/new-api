package tokenkit

// fork: O11 — Warm builds the o200k and cl100k codecs now instead of on the
// first GPT/o-series Count call (hundreds of ms). Safe to call repeatedly and
// concurrently; after the first call it returns immediately (team 338576ab2).
func Warm() {
	o200kBase()
	cl100kBase()
}
