package openai

import "testing"

func TestIsOpenAIImageStreamErrorEvent_NullErrorFieldIsNotAnError(t *testing.T) {
	data := []byte(`{"type":"image_generation.partial_image","error":null}`)
	if isOpenAIImageStreamErrorEvent(data) {
		t.Error(`"error": null must not be classified as an error event`)
	}
}

func TestIsOpenAIImageStreamErrorEvent_NonNullErrorFieldIsAnError(t *testing.T) {
	data := []byte(`{"type":"image_generation.partial_image","error":{"message":"boom"}}`)
	if !isOpenAIImageStreamErrorEvent(data) {
		t.Error("a populated error object must be classified as an error event")
	}
}

func TestExtractOpenAIImageStreamErrorMessage_NullErrorFallsBackToDefault(t *testing.T) {
	got := extractOpenAIImageStreamErrorMessage([]byte(`{"error":null}`))
	if got != "upstream image stream returned error event" {
		t.Errorf("got %q", got)
	}
}
