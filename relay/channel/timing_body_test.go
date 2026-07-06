package channel

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type errCloser struct {
	io.Reader
	closeErr error
	closed   bool
}

func (e *errCloser) Close() error {
	e.closed = true
	return e.closeErr
}

func TestTimingReadCloser_CallsOnDoneOnceOnEOF(t *testing.T) {
	calls := 0
	rc := &errCloser{Reader: strings.NewReader("hello")}
	trc := newTimingReadCloser(rc, func() { calls++ })

	buf := make([]byte, 3)
	_, err := trc.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 0, calls, "must not fire before EOF")

	_, err = trc.Read(buf)
	require.NoError(t, err)

	_, err = trc.Read(buf)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 1, calls)

	// Further reads past EOF must not double-fire.
	_, _ = trc.Read(buf)
	require.Equal(t, 1, calls)
}

func TestTimingReadCloser_CallsOnDoneOnClose(t *testing.T) {
	calls := 0
	rc := &errCloser{Reader: strings.NewReader("hello")}
	trc := newTimingReadCloser(rc, func() { calls++ })

	require.NoError(t, trc.Close())
	require.Equal(t, 1, calls)
	require.True(t, rc.closed)
}

func TestTimingReadCloser_ReadErrorTriggersOnDoneOnce(t *testing.T) {
	calls := 0
	rc := &errCloser{Reader: strings.NewReader(""), closeErr: errors.New("boom")}
	trc := newTimingReadCloser(rc, func() { calls++ })

	buf := make([]byte, 3)
	_, err := trc.Read(buf)
	require.Error(t, err)
	require.Equal(t, 1, calls)

	err = trc.Close()
	require.EqualError(t, err, "boom")
	require.Equal(t, 1, calls, "onDone must not fire twice")
}
