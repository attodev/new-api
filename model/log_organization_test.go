package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetLogsByUserIDsScopesToMembers(t *testing.T) {
	t.Cleanup(func() { DB.Exec("DELETE FROM logs") })
	for _, l := range []*Log{
		{UserId: 11, Username: "m1", ModelName: "gpt-x", Type: LogTypeConsume, CreatedAt: 100},
		{UserId: 12, Username: "m2", ModelName: "gpt-x", Type: LogTypeConsume, CreatedAt: 101},
		{UserId: 99, Username: "outsider", ModelName: "gpt-x", Type: LogTypeConsume, CreatedAt: 102},
	} {
		require.NoError(t, LOG_DB.Create(l).Error)
	}
	logs, total, err := GetLogsByUserIDs([]int{11, 12}, LogFilter{ModelName: "gpt-x"}, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	require.Equal(t, "m2", logs[0].Username, "newest first")

	logs, total, err = GetLogsByUserIDs(nil, LogFilter{}, 0, 10)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, logs)
}

func TestMidjourneyTaskQueryParamsFilterByUserIDs(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Midjourney{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM midjourneys") })
	for _, task := range []*Midjourney{
		{UserId: 11, MjId: "a", SubmitTime: 1},
		{UserId: 12, MjId: "b", SubmitTime: 2},
		{UserId: 99, MjId: "c", SubmitTime: 3},
	} {
		require.NoError(t, DB.Create(task).Error)
	}
	params := TaskQueryParams{UserIDs: []int{11, 12}}
	tasks := GetAllTasks(0, 10, params)
	require.Len(t, tasks, 2)
	require.EqualValues(t, 2, CountAllTasks(params))
	require.EqualValues(t, 3, CountAllTasks(TaskQueryParams{}))
}
