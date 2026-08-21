package model_setting

import "testing"

// TestIsGeminiModelSupportImagine_GAImageModels reproduces a real bug: when
// Google promoted gemini-3-pro-image-preview/gemini-3.1-flash-image-preview
// to GA as gemini-3-pro-image/gemini-3.1-flash-image, only the preview names
// were in SupportedImagineModels - a client on the GA model name would be
// treated as not supporting image generation at all.
func TestIsGeminiModelSupportImagine_GAImageModels(t *testing.T) {
	for _, model := range []string{"gemini-3-pro-image", "gemini-3.1-flash-image"} {
		if !IsGeminiModelSupportImagine(model) {
			t.Errorf("IsGeminiModelSupportImagine(%q) = false, want true", model)
		}
	}
}
