package openwebui

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestTokenNameForTask(t *testing.T) {
	require.Equal(t, TokenNameChat, TokenNameForTask(""))
	require.Equal(t, TokenNameChat, TokenNameForTask("   "))
	require.Equal(t, TokenNameTask, TokenNameForTask("title_generation"))
}

func TestGetOrCreateUserToken_CreatesThenReuses(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")

	first, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.Equal(t, 7, first.UserId)
	require.Equal(t, TokenNameChat, first.Name)
	require.NotEmpty(t, first.Key)

	again, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.Equal(t, first.Id, again.Id)
	require.Equal(t, first.Key, again.Key, "key must be populated on cache hits too")

	task, err := GetOrCreateUserToken(7, TokenNameTask)
	require.NoError(t, err)
	require.NotEqual(t, first.Id, task.Id)
	require.Equal(t, TokenNameTask, task.Name)

	var count int64
	require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", 7).Count(&count).Error)
	require.Equal(t, int64(2), count)
}

func TestGetOrCreateUserToken_RecreatesAfterUserDeletesIt(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")

	first, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.NoError(t, model.DeleteTokenById(first.Id, 7))

	second, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.NotEqual(t, first.Id, second.Id)
	require.NotEqual(t, first.Key, second.Key)
}

func TestGetOrCreateUserToken_ConcurrentFirstRequestsCreateOneRow(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")

	const n = 10
	ids := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := GetOrCreateUserToken(7, TokenNameChat)
			if err == nil {
				ids[i] = tok.Id
			}
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ? AND name = ?", 7, TokenNameChat).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
