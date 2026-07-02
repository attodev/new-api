package channel

import (
	"io"
	"sync"
)

// timingReadCloser wraps an io.ReadCloser and invokes onDone exactly once,
// at whichever happens first: the wrapped reader returning any error
// (typically io.EOF) or Close() being called. This lets a single wrap point
// (doRequest) capture "upstream response fully consumed" for both streaming
// readers (which read to EOF chunk by chunk) and non-streaming callers
// (which io.ReadAll the whole body) without changing either call site.
type timingReadCloser struct {
	io.ReadCloser
	once   sync.Once
	onDone func()
}

func newTimingReadCloser(rc io.ReadCloser, onDone func()) *timingReadCloser {
	return &timingReadCloser{ReadCloser: rc, onDone: onDone}
}

func (t *timingReadCloser) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if err != nil {
		t.markDone()
	}
	return n, err
}

func (t *timingReadCloser) Close() error {
	t.markDone()
	return t.ReadCloser.Close()
}

func (t *timingReadCloser) markDone() {
	t.once.Do(t.onDone)
}
