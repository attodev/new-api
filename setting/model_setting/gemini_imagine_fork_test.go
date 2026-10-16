package model_setting

import "testing"

// When Google promoted the preview image models to GA under new names, only
// the preview names were listed, so clients on the GA names were treated as
// not supporting image generation at all.
func TestIsGeminiModelSupportImagineGemini3(t *testing.T) {
	for _, model := range []string{
		"gemini-3-pro-image",
		"gemini-3-pro-image-preview",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
	} {
		if !IsGeminiModelSupportImagine(model) {
			t.Errorf("IsGeminiModelSupportImagine(%q) = false, want true", model)
		}
	}
	if IsGeminiModelSupportImagine("gemini-3-pro") {
		t.Error("IsGeminiModelSupportImagine(\"gemini-3-pro\") = true, want false")
	}
}
