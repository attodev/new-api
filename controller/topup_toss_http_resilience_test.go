package controller

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTossRetryAfterDelayParsesSecondsAndHTTPDateWithCap(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	require.Equal(t, 3*time.Second, tossRetryAfterDelay("3", now))
	require.Equal(t, 4*time.Second, tossRetryAfterDelay(now.Add(4*time.Second).Format(http.TimeFormat), now))
	require.Equal(t, tossRetryAfterMaxDelay, tossRetryAfterDelay("3600", now))
	require.Equal(t, tossRetryAfterMaxDelay, tossRetryAfterDelay(now.Add(time.Hour).Format(http.TimeFormat), now))
	require.Zero(t, tossRetryAfterDelay("invalid", now))
	require.Zero(t, tossRetryAfterDelay(now.Add(-time.Second).Format(http.TimeFormat), now))
}

func TestWaitTossRetryDoesNotOutliveCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()

	err := waitTossRetry(ctx, 0, "3600")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 100*time.Millisecond)
}

func TestTossPaymentOperationContextSurvivesClientCancellationAndKeepsValues(t *testing.T) {
	type traceKey struct{}
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), traceKey{}, "trace-value"))
	operation, cancelOperation := tossPaymentOperationContext(parent, time.Second)
	defer cancelOperation()
	cancelParent()

	require.Equal(t, "trace-value", operation.Value(traceKey{}))
	select {
	case <-operation.Done():
		t.Fatalf("payment operation was canceled with browser context: %v", operation.Err())
	case <-time.After(20 * time.Millisecond):
	}
}

func TestTossPaymentOperationContextIgnoresShortRequestDeadline(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelParent()
	operation, cancelOperation := tossPaymentOperationContext(parent, 100*time.Millisecond)
	defer cancelOperation()

	time.Sleep(50 * time.Millisecond)
	require.Error(t, parent.Err())
	require.NoError(t, operation.Err(), "money-moving POST must retain its provider timeout after request deadline")
}

func TestDoTossAPIRequestHonorsRetryAfterWithoutExceedingDeadline(t *testing.T) {
	for _, retryAfter := range []string{
		"5",
		time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat),
	} {
		t.Run(retryAfter, func(t *testing.T) {
			originalClient := http.DefaultClient
			t.Cleanup(func() { http.DefaultClient = originalClient })
			var calls atomic.Int64
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				header := make(http.Header)
				header.Set("Retry-After", retryAfter)
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     header,
					Body:       io.NopCloser(strings.NewReader(`{"code":"TOO_MANY_REQUESTS"}`)),
				}, nil
			})}

			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			started := time.Now()
			status, _, err := doTossAPIRequestWithSecret(
				ctx, http.MethodGet, "https://api.test.tosspayments.local/v1/payments/safe", nil, "", "secret", http.StatusOK,
			)

			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, http.StatusTooManyRequests, status)
			require.Equal(t, int64(1), calls.Load(), "deadline must prevent an early retry before Retry-After")
			require.Less(t, time.Since(started), 100*time.Millisecond)
		})
	}
}

func TestDoTossAPIRequestDoesNotStartRetryBelowMinimumAttemptBudget(t *testing.T) {
	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })

	const minimumAttemptBudget = 200 * time.Millisecond
	var calls atomic.Int64
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		deadline, ok := request.Context().Deadline()
		require.True(t, ok)
		require.GreaterOrEqual(t, time.Until(deadline), minimumAttemptBudget-10*time.Millisecond)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":"PROVIDER_ERROR"}`)),
		}, nil
	})}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	status, _, err := doTossAPIRequestWithSecretAndMinimumAttemptBudget(
		ctx,
		http.MethodPost,
		"https://api.test.tosspayments.local/v1/payments/confirm",
		[]byte(`{"orderId":"safe_order"}`),
		"safe_order",
		"safe_secret",
		minimumAttemptBudget,
		http.StatusOK,
	)

	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, int64(1), calls.Load(), "retry must wait for a future worker when less than one full provider budget remains")
}

func TestDoTossAPIRequestDoesNotFollowRedirectOrForwardAuthorization(t *testing.T) {
	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })

	var redirectedCalls atomic.Int64
	var redirectedAuthorization atomic.Value
	var sourceCalls atomic.Int64
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "api.test.tosspayments.local":
			sourceCalls.Add(1)
			header := make(http.Header)
			header.Set("Location", "https://redirect.test.invalid/credential-target")
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		case "redirect.test.invalid":
			redirectedCalls.Add(1)
			redirectedAuthorization.Store(request.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		default:
			return nil, errors.New("unexpected test request host")
		}
	})}

	status, _, err := doTossAPIRequestWithSecret(
		context.Background(),
		http.MethodPost,
		"https://api.test.tosspayments.local/v1/payments/confirm",
		[]byte(`{"orderId":"safe_order"}`),
		"safe_order",
		"secret_must_not_be_forwarded",
		http.StatusOK,
	)

	require.Equal(t, http.StatusFound, status)
	require.Error(t, err)
	require.Equal(t, int64(1), sourceCalls.Load(), "3xx must be classified as a non-transient response")
	require.Zero(t, redirectedCalls.Load())
	value := redirectedAuthorization.Load()
	if value != nil {
		require.Empty(t, value.(string))
	}
}

func TestSecureTossHTTPTransportDoesNotMutateGlobalInsecureTLSConfig(t *testing.T) {
	original := &http.Transport{
		ResponseHeaderTimeout: time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // mirrors TLS_INSECURE_SKIP_VERIFY=true
			MinVersion:         tls.VersionTLS10,
			ServerName:         "api.tosspayments.com",
		},
	}

	secured, ok := secureTossHTTPTransport(original).(*http.Transport)

	require.True(t, ok)
	require.NotSame(t, original, secured)
	require.NotSame(t, original.TLSClientConfig, secured.TLSClientConfig)
	require.True(t, original.TLSClientConfig.InsecureSkipVerify, "global transport must remain unchanged")
	require.False(t, secured.TLSClientConfig.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS10), original.TLSClientConfig.MinVersion)
	require.Equal(t, uint16(tls.VersionTLS12), secured.TLSClientConfig.MinVersion)
	require.Equal(t, time.Second, original.ResponseHeaderTimeout)
	require.Zero(t, secured.ResponseHeaderTimeout)
	require.Equal(t, original.TLSClientConfig.ServerName, secured.TLSClientConfig.ServerName)
}

func TestSecureTossHTTPTransportPreservesStrongerTLSMinimum(t *testing.T) {
	original := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}

	secured, ok := secureTossHTTPTransport(original).(*http.Transport)

	require.True(t, ok)
	require.Equal(t, uint16(tls.VersionTLS13), secured.TLSClientConfig.MinVersion)
}

func TestSecureTossHTTPTransportClonesDefaultForNilClientTransport(t *testing.T) {
	originalDefault := http.DefaultTransport
	defaultTransport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	http.DefaultTransport = defaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalDefault })

	secured, ok := secureTossHTTPTransport(nil).(*http.Transport)

	require.True(t, ok)
	require.NotSame(t, defaultTransport, secured)
	require.True(t, defaultTransport.TLSClientConfig.InsecureSkipVerify)
	require.False(t, secured.TLSClientConfig.InsecureSkipVerify)
}

func TestSecureTossHTTPTransportPreservesCustomRoundTripper(t *testing.T) {
	var calls atomic.Int64
	custom := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})

	secured := secureTossHTTPTransport(custom)
	response, err := secured.RoundTrip(httptest.NewRequest(http.MethodGet, "https://api.test.tosspayments.local", nil))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.Equal(t, int64(1), calls.Load())
}

func TestDoTossHTTPRequestUsesRequestContextInsteadOfShortGlobalClientTimeout(t *testing.T) {
	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })
	http.DefaultClient = &http.Client{
		Timeout: time.Millisecond,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			select {
			case <-request.Context().Done():
				return nil, request.Context().Err()
			case <-time.After(20 * time.Millisecond):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("ok")),
					Request:    request,
				}, nil
			}
		}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.test.tosspayments.local/v1/payments/safe", nil)
	require.NoError(t, err)

	response, err := doTossHTTPRequest(req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
}

func TestDoTossHTTPRequestIgnoresShortGlobalResponseHeaderTimeout(t *testing.T) {
	originalClient := http.DefaultClient
	transport := &http.Transport{
		ResponseHeaderTimeout: time.Millisecond,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			go func() {
				defer server.Close()
				request, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					return
				}
				_ = request.Body.Close()
				time.Sleep(20 * time.Millisecond)
				_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			}()
			return client, nil
		},
	}
	http.DefaultClient = &http.Client{Transport: transport}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		http.DefaultClient = originalClient
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://api.test.tosspayments.local/v1/payments/safe", nil)
	require.NoError(t, err)

	response, err := doTossHTTPRequest(req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.Equal(t, time.Millisecond, transport.ResponseHeaderTimeout, "global transport must remain unchanged")
}

type tossReadErrorBody struct{}

func (tossReadErrorBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (tossReadErrorBody) Close() error             { return nil }

func TestDoTossAPIRequestRetriesAcceptedResponseReadError(t *testing.T) {
	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })

	var calls atomic.Int64
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       tossReadErrorBody{},
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"DONE"}`)),
		}, nil
	})}

	status, body, err := doTossAPIRequestWithSecret(
		context.Background(), http.MethodGet, "https://api.test.tosspayments.local/v1/payments/safe", nil, "", "secret", http.StatusOK,
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"status":"DONE"}`, string(body))
	require.Equal(t, int64(2), calls.Load())
}

func TestDoTossAPIRequestKeepsReadErrorURLFreeAfterRetries(t *testing.T) {
	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: tossReadErrorBody{}}, nil
	})}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := doTossAPIRequestWithSecret(
		ctx, http.MethodGet, "https://api.test.tosspayments.local/v1/payments/pay_sensitive", nil, "", "secret", http.StatusOK,
	)

	require.Error(t, err)
	var transportErr *tossAPITransportError
	require.True(t, errors.As(err, &transportErr))
	require.NotContains(t, err.Error(), "pay_sensitive")
}
