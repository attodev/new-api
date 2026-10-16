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

// Ported from team image_stream_error_event_test.go (b5d733722). team's
// event-name override case is dropped: develop's detector takes only the
// "data:" payload (StreamScannerHandler does not pass the SSE event name).
func TestIsOpenAIImageStreamErrorEvent_InvalidJSONIsNotAnError(t *testing.T) {
	if isOpenAIImageStreamErrorEvent([]byte(`not json`)) {
		t.Error("invalid JSON must not be classified as an error event")
	}
}

func TestIsOpenAIImageStreamErrorEvent_ErrorTypeField(t *testing.T) {
	for _, data := range []string{`{"type":"error"}`, `{"type":"upstream_error"}`, `{"type":" Error "}`} {
		if !isOpenAIImageStreamErrorEvent([]byte(data)) {
			t.Errorf("%s must be classified as an error event", data)
		}
	}
	if isOpenAIImageStreamErrorEvent([]byte(`{"type":"image_generation.completed"}`)) {
		t.Error("a completed event must not be classified as an error event")
	}
}
