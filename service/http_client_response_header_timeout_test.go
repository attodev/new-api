package service

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRelayTransportBoundsResponseHeaderWait(t *testing.T) {
	old := common.RelayResponseHeaderTimeout
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = old })

	common.RelayResponseHeaderTimeout = 1
	tr := newRelayTransport()
	require.Equal(t, time.Second, tr.ResponseHeaderTimeout)

	common.RelayResponseHeaderTimeout = 0
	require.Zero(t, newRelayTransport().ResponseHeaderTimeout)

	common.RelayResponseHeaderTimeout = -5
	require.Zero(t, newRelayTransport().ResponseHeaderTimeout)

	common.RelayResponseHeaderTimeout = math.MaxInt
	require.Equal(t, time.Duration(maxTimeoutSeconds)*time.Second, newRelayTransport().ResponseHeaderTimeout)
}

func TestRelayTransportKeepsExistingSettings(t *testing.T) {
	oldIdle, oldPerHost := common.RelayMaxIdleConns, common.RelayMaxIdleConnsPerHost
	t.Cleanup(func() {
		common.RelayMaxIdleConns, common.RelayMaxIdleConnsPerHost = oldIdle, oldPerHost
	})
	common.RelayMaxIdleConns, common.RelayMaxIdleConnsPerHost = 11, 7

	tr := newRelayTransport()
	require.Equal(t, 11, tr.MaxIdleConns)
	require.Equal(t, 7, tr.MaxIdleConnsPerHost)
	require.True(t, tr.ForceAttemptHTTP2)
	require.NotNil(t, tr.Proxy)
}

func TestProxyTransportsBoundResponseHeaderWait(t *testing.T) {
	old := common.RelayResponseHeaderTimeout
	t.Cleanup(func() {
		common.RelayResponseHeaderTimeout = old
		ResetProxyClientCache()
	})
	common.RelayResponseHeaderTimeout = 3
	ResetProxyClientCache()

	for _, proxyURL := range []string{"http://127.0.0.1:1", "socks5://127.0.0.1:1"} {
		client, err := NewProxyHttpClient(proxyURL)
		require.NoError(t, err)
		tr, ok := client.Transport.(*http.Transport)
		require.True(t, ok)
		require.Equal(t, 3*time.Second, tr.ResponseHeaderTimeout, proxyURL)
	}
}

func TestRelayTransportFailsWhenUpstreamWithholdsHeaders(t *testing.T) {
	old := common.RelayResponseHeaderTimeout
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = old })
	common.RelayResponseHeaderTimeout = 1

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	tr := newRelayTransport()
	t.Cleanup(tr.CloseIdleConnections)
	client := &http.Client{Transport: tr}

	start := time.Now()
	resp, err := client.Get(srv.URL)
	elapsed := time.Since(start)
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout awaiting response headers")
	require.Less(t, elapsed, 1900*time.Millisecond)
}

func TestRelayTransportAllowsSlowBodyAfterHeaders(t *testing.T) {
	old := common.RelayResponseHeaderTimeout
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = old })
	common.RelayResponseHeaderTimeout = 1

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(srv.Close)

	tr := newRelayTransport()
	t.Cleanup(tr.CloseIdleConnections)
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "done", string(body))
}
