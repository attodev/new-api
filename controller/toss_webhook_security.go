package controller

import (
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Toss does not sign general webhooks. PAYMENT_STATUS_CHANGED is authenticated
// again by re-fetching the Payment object, while BILLING_DELETED has no
// corresponding lookup API. The router restricts every event to Toss's
// published inbound IPs before body parsing/provider lookup, and the handler
// repeats the check for the destructive billing-key event as defense in depth.
//
// TOSS_WEBHOOK_SOURCE_CIDRS can add newly published Toss source IPs before an
// application upgrade. TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS enables X-Forwarded-For
// only when the direct peer is an explicitly trusted reverse proxy; without
// that boundary an attacker could spoof the header.
var tossPublishedWebhookSourceCIDRs = []string{
	"13.124.18.147",
	"13.124.108.35",
	"3.36.173.151",
	"3.38.81.32",
	"115.92.221.121",
	"115.92.221.122",
	"115.92.221.123",
	"115.92.221.125",
	"115.92.221.126",
	"115.92.221.127",
}

func splitTossCIDREnv(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func tossWebhookSourceCIDRs() []string {
	additional := splitTossCIDREnv("TOSS_WEBHOOK_SOURCE_CIDRS")
	result := make([]string, 0, len(tossPublishedWebhookSourceCIDRs)+len(additional))
	result = append(result, tossPublishedWebhookSourceCIDRs...)
	return append(result, additional...)
}

func tossRemoteIP(request *http.Request) net.IP {
	if request == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(request.RemoteAddr)
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

func tossForwardedSourceIP(request *http.Request, trustedProxyCIDRs []string) net.IP {
	if request == nil || len(trustedProxyCIDRs) == 0 {
		return nil
	}
	direct := tossRemoteIP(request)
	if direct == nil || !common.IsIpInCIDRList(direct, trustedProxyCIDRs) {
		return nil
	}

	// Walk from the app-facing end of the chain. Every trusted proxy is skipped;
	// the first untrusted hop is the original source as asserted by that boundary.
	forwarded := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
	for i := len(forwarded) - 1; i >= 0; i-- {
		candidate := net.ParseIP(strings.TrimSpace(forwarded[i]))
		if candidate == nil {
			return nil
		}
		if common.IsIpInCIDRList(candidate, trustedProxyCIDRs) {
			continue
		}
		return candidate
	}
	return nil
}

func isTrustedTossWebhookSource(request *http.Request) bool {
	sources := tossWebhookSourceCIDRs()
	if direct := tossRemoteIP(request); direct != nil && common.IsIpInCIDRList(direct, sources) {
		return true
	}
	forwarded := tossForwardedSourceIP(request, splitTossCIDREnv("TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS"))
	return forwarded != nil && common.IsIpInCIDRList(forwarded, sources)
}

// IsTrustedTossWebhookSource exposes the same strict direct-peer/trusted-proxy
// decision to the router. It is used only to exempt authenticated Toss
// infrastructure traffic from the shared end-user API rate-limit bucket; the
// webhook handler still performs its event-specific validation and Payment GET.
func IsTrustedTossWebhookSource(request *http.Request) bool {
	return isTrustedTossWebhookSource(request)
}
