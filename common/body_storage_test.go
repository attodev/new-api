package common

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertNewReaderContract(t *testing.T, storage BodyStorage, payload []byte) {
	t.Helper()

	// Two readers own independent cursors.
	a, err := storage.NewReader()
	require.NoError(t, err)
	b, err := storage.NewReader()
	require.NoError(t, err)

	half := len(payload) / 2
	aHead := make([]byte, half)
	_, err = io.ReadFull(a, aHead)
	require.NoError(t, err)
	assert.Equal(t, payload[:half], aHead)

	bAll, err := io.ReadAll(b)
	require.NoError(t, err)
	assert.Equal(t, payload, bAll, "reader B must see the full payload while A is mid-read")

	aRest, err := io.ReadAll(a)
	require.NoError(t, err)
	assert.Equal(t, payload[half:], aRest, "reader A must resume from its own cursor")

	// Closing a reader must not close the storage.
	require.NoError(t, a.Close())
	require.NoError(t, b.Close())
	c, err := storage.NewReader()
	require.NoError(t, err)
	all, err := io.ReadAll(c)
	require.NoError(t, err)
	require.NoError(t, c.Close())
	assert.Equal(t, payload, all)

	// The primary cursor is untouched by replay readers.
	primary, err := io.ReadAll(storage)
	require.NoError(t, err)
	assert.Equal(t, payload, primary)

	// After the storage is closed, NewReader fails loudly.
	require.NoError(t, storage.Close())
	_, err = storage.NewReader()
	require.ErrorIs(t, err, ErrStorageClosed)
}

func TestMemoryBodyStorageNewReader(t *testing.T) {
	payload := []byte(`{"model":"test","input":"abcdefghijklmnopqrstuvwxyz"}`)
	storage, err := CreateBodyStorage(payload)
	require.NoError(t, err)
	require.False(t, storage.IsDisk())
	assertNewReaderContract(t, storage, payload)
}

func TestDiskBodyStorageNewReader(t *testing.T) {
	prev := GetDiskCacheConfig()
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 0,
		MaxSizeMB:   64,
		Path:        t.TempDir(),
	})
	defer SetDiskCacheConfig(prev)

	payload := []byte(`{"model":"test","input":"abcdefghijklmnopqrstuvwxyz"}`)
	storage, err := CreateBodyStorage(payload)
	require.NoError(t, err)
	require.True(t, storage.IsDisk(), "payload must take the diskStorage path")
	assertNewReaderContract(t, storage, payload)
}
