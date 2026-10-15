package main

import (
	"bytes"
	"os"
	"strings"
)

// fork: O11 — the team chatbot widget (team 3757c8244) is injected into the
// SPA index only when CHATBOT_WIDGET_URL is set (base URL, e.g.
// https://chatbot.alrouter.ai), deferred so it never blocks first paint.
// Mirrors InjectUmamiAnalytics / InjectGoogleAnalytics.
func InjectChatbotWidget() {
	injectBuilder := &strings.Builder{}
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("CHATBOT_WIDGET_URL")), "/"); base != "" {
		injectBuilder.WriteString("<script defer src=\"")
		injectBuilder.WriteString(base)
		injectBuilder.WriteString("/widget.js\" data-api-base=\"")
		injectBuilder.WriteString(base)
		injectBuilder.WriteString("\"></script>")
	}
	injectBuilder.WriteString("<!--Chatbot QuantumNous-->\n")
	placeholder := []byte("<!--chatbot-->\n")
	indexPage = bytes.ReplaceAll(indexPage, placeholder, []byte(injectBuilder.String()))
}
