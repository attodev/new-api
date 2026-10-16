package service

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func withRelayResponseHeaderTimeout(t *testing.T, seconds int) {
	t.Helper()
	old := common.RelayResponseHeaderTimeout
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = old })
	common.RelayResponseHeaderTimeout = seconds
}

func TestRelayHTTPTransportBoundsResponseHeaderWait(t *testing.T) {
	withRelayResponseHeaderTimeout(t, 1)
	require.Equal(t, time.Second, newRelayHTTPTransport().ResponseHeaderTimeout)

	common.RelayResponseHeaderTimeout = 0
	require.Zero(t, newRelayHTTPTransport().ResponseHeaderTimeout)

	common.RelayResponseHeaderTimeout = -5
	require.Zero(t, newRelayHTTPTransport().ResponseHeaderTimeout)

	// Values that would overflow time.Duration are clamped instead of wrapping
	// into a tiny positive timeout.
	common.RelayResponseHeaderTimeout = math.MaxInt
	require.Equal(t, time.Duration(maxTimeoutSeconds)*time.Second, newRelayHTTPTransport().ResponseHeaderTimeout)
}

func TestRelayHTTPTransportKeepsExistingSettings(t *testing.T) {
	oldIdle, oldPerHost := common.RelayMaxIdleConns, common.RelayMaxIdleConnsPerHost
	t.Cleanup(func() {
		common.RelayMaxIdleConns, common.RelayMaxIdleConnsPerHost = oldIdle, oldPerHost
	})
	common.RelayMaxIdleConns, common.RelayMaxIdleConnsPerHost = 11, 7
	withRelayResponseHeaderTimeout(t, 2)

	tr := newRelayHTTPTransport()
	require.Equal(t, 11, tr.MaxIdleConns)
	require.Equal(t, 7, tr.MaxIdleConnsPerHost)
	require.True(t, tr.ForceAttemptHTTP2)
	require.NotNil(t, tr.Proxy)
	require.Equal(t, 2*time.Second, tr.ResponseHeaderTimeout)
}

func TestProxyTransportsBoundResponseHeaderWait(t *testing.T) {
	withRelayResponseHeaderTimeout(t, 3)
	ResetProxyClientCache()
	t.Cleanup(ResetProxyClientCache)

	for _, rawProxyURL := range []string{"http://127.0.0.1:1", "socks5://127.0.0.1:1"} {
		proxyURL, err := url.Parse(rawProxyURL)
		require.NoError(t, err)
		factory, err := newTransportFactory(proxyURL, nil)
		require.NoError(t, err, rawProxyURL)
		require.Equal(t, 3*time.Second, factory().ResponseHeaderTimeout, rawProxyURL)

		// The cached public entry point builds its transport the same way.
		client, err := NewProxyHttpClient(rawProxyURL)
		require.NoError(t, err, rawProxyURL)
		tr, ok := client.Transport.(*http.Transport)
		require.True(t, ok, rawProxyURL)
		require.Equal(t, 3*time.Second, tr.ResponseHeaderTimeout, rawProxyURL)
	}
}

func TestRelayHTTPTransportFailsWhenUpstreamWithholdsHeaders(t *testing.T) {
	// The setting is whole seconds, so the smallest bound is 1s; the upstream
	// withholds headers for longer than that.
	withRelayResponseHeaderTimeout(t, 1)

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(1500 * time.Millisecond):
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	tr := newRelayHTTPTransport()
	t.Cleanup(tr.CloseIdleConnections)
	client := &http.Client{Transport: tr}

	start := time.Now()
	resp, err := client.Get(srv.URL)
	elapsed := time.Since(start)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout awaiting response headers")
	require.Less(t, elapsed, 1400*time.Millisecond)
}

func TestRelayHTTPTransportAllowsSlowBodyAfterHeaders(t *testing.T) {
	withRelayResponseHeaderTimeout(t, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// Slower than the header bound: only the wait for headers is limited.
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(srv.Close)

	tr := newRelayHTTPTransport()
	t.Cleanup(tr.CloseIdleConnections)
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "done", string(body))
}
