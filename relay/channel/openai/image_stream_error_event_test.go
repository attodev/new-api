package openai

import "testing"

// TestIsOpenAIImageStreamErrorEvent_NullErrorFieldIsNotAnError reproduces a
// real bug: an explicit JSON "error": null (a common discriminated-union
// convention) was misclassified as an error event, because
// json.RawMessage.UnmarshalJSON copies the literal 4-byte "null" verbatim
// rather than leaving the field empty - so len(payload.Error) > 0 was true
// even when there was no actual error. A normal image_generation.partial_image
// event carrying "error": null would be recorded as a stream error and force
// an error-level completion for what was actually a successful generation.
func TestIsOpenAIImageStreamErrorEvent_NullErrorFieldIsNotAnError(t *testing.T) {
	data := []byte(`{"type":"image_generation.partial_image","error":null}`)
	if isOpenAIImageStreamErrorEvent("", data) {
		t.Error(`isOpenAIImageStreamErrorEvent with "error": null = true, want false`)
	}
}

func TestIsOpenAIImageStreamErrorEvent_NonNullErrorFieldIsAnError(t *testing.T) {
	data := []byte(`{"type":"image_generation.partial_image","error":{"message":"boom"}}`)
	if !isOpenAIImageStreamErrorEvent("", data) {
		t.Error(`isOpenAIImageStreamErrorEvent with a populated error object = false, want true`)
	}
}

func TestIsOpenAIImageStreamErrorEvent_EventNameOverride(t *testing.T) {
	if !isOpenAIImageStreamErrorEvent("error", []byte(`{}`)) {
		t.Error(`isOpenAIImageStreamErrorEvent with eventName "error" = false, want true`)
	}
}

func TestIsOpenAIImageStreamErrorEvent_InvalidJSONIsNotAnError(t *testing.T) {
	if isOpenAIImageStreamErrorEvent("", []byte(`not json`)) {
		t.Error(`isOpenAIImageStreamErrorEvent with invalid JSON = true, want false`)
	}
}

func TestIsOpenAIImageStreamErrorEvent_ErrorTypeField(t *testing.T) {
	if !isOpenAIImageStreamErrorEvent("", []byte(`{"type":"error"}`)) {
		t.Error(`isOpenAIImageStreamErrorEvent with type "error" = false, want true`)
	}
	if !isOpenAIImageStreamErrorEvent("", []byte(`{"type":"upstream_error"}`)) {
		t.Error(`isOpenAIImageStreamErrorEvent with type "upstream_error" = false, want true`)
	}
}
