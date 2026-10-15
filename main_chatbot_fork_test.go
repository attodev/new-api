package main

import (
	"strings"
	"testing"
)

// fork: O11 — CHATBOT_WIDGET_URL gates the deferred chatbot widget script.
const chatbotSampleIndex = "<body>\n    <div id=\"root\"></div>\n    <!--chatbot-->\n  </body>\n"

func withChatbotSampleIndex(t *testing.T) {
	t.Helper()
	original := indexPage
	t.Cleanup(func() { indexPage = original })
	indexPage = []byte(chatbotSampleIndex)
}

func TestInjectChatbotWidgetWithURL(t *testing.T) {
	withChatbotSampleIndex(t)
	t.Setenv("CHATBOT_WIDGET_URL", "https://chatbot.alrouter.ai/")

	InjectChatbotWidget()

	page := string(indexPage)
	want := `<script defer src="https://chatbot.alrouter.ai/widget.js" data-api-base="https://chatbot.alrouter.ai"></script><!--Chatbot QuantumNous-->` + "\n"
	if !strings.Contains(page, want) {
		t.Fatalf("expected deferred widget script, got:\n%s", page)
	}
	if strings.Contains(page, "<!--chatbot-->") {
		t.Fatalf("placeholder not replaced:\n%s", page)
	}
}

func TestInjectChatbotWidgetWithoutURL(t *testing.T) {
	withChatbotSampleIndex(t)
	t.Setenv("CHATBOT_WIDGET_URL", "")

	InjectChatbotWidget()

	page := string(indexPage)
	if strings.Contains(page, "<script") {
		t.Fatalf("expected no script when CHATBOT_WIDGET_URL is unset, got:\n%s", page)
	}
	if !strings.Contains(page, "    <!--Chatbot QuantumNous-->\n  </body>") {
		t.Fatalf("expected the placeholder to become the marker, got:\n%s", page)
	}
	if strings.Contains(page, "<!--chatbot-->") {
		t.Fatalf("placeholder not replaced:\n%s", page)
	}
}
