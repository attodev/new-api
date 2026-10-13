package service

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// statusSunoAdaptor replays a canned body with a configurable HTTP status.
type statusSunoAdaptor struct {
	fakeSunoAdaptor
	status int
}

func (f *statusSunoAdaptor) FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error) {
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.responseBody)),
	}, nil
}

func TestUpdateSunoTasks_AcceptsAny2xxPollStatus(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			truncate(t)
			const userID, tokenID, channelID = 22, 22, 22
			const initQuota, preConsumed = 10000, 3000

			seedUser(t, userID, initQuota)
			seedToken(t, tokenID, userID, "sk-suno-2xx", 5000)
			seedChannelWithBaseURL(t, channelID)

			task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
			task.TaskID = "suno-task-2xx"
			task.Status = model.TaskStatusInProgress
			require.NoError(t, model.DB.Create(task).Error)

			prevAdaptorFn := GetTaskAdaptorFunc
			t.Cleanup(func() { GetTaskAdaptorFunc = prevAdaptorFn })
			fake := &statusSunoAdaptor{
				fakeSunoAdaptor: fakeSunoAdaptor{responseBody: sunoFailureResponseBody(t, task.TaskID, "upstream build failed")},
				status:          status,
			}
			GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor { return fake }

			taskM := map[string]*model.Task{task.TaskID: task}
			require.NoError(t, updateSunoTasks(context.Background(), channelID, []string{task.TaskID}, taskM))

			var reloaded model.Task
			require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
			require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status, "a %d poll response must be parsed", status)
			require.Equal(t, int64(initQuota+preConsumed), getUserQuota(t, userID))
		})
	}
}

func TestUpdateSunoTasks_RejectsNon2xxPollStatus(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusBadGateway} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			truncate(t)
			const userID, tokenID, channelID = 23, 23, 23
			const initQuota, preConsumed = 10000, 3000

			seedUser(t, userID, initQuota)
			seedToken(t, tokenID, userID, "sk-suno-non2xx", 5000)
			seedChannelWithBaseURL(t, channelID)

			task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
			task.TaskID = "suno-task-non2xx"
			task.Status = model.TaskStatusInProgress
			require.NoError(t, model.DB.Create(task).Error)

			prevAdaptorFn := GetTaskAdaptorFunc
			t.Cleanup(func() { GetTaskAdaptorFunc = prevAdaptorFn })
			fake := &statusSunoAdaptor{
				fakeSunoAdaptor: fakeSunoAdaptor{responseBody: sunoFailureResponseBody(t, task.TaskID, "upstream build failed")},
				status:          status,
			}
			GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor { return fake }

			taskM := map[string]*model.Task{task.TaskID: task}
			err := updateSunoTasks(context.Background(), channelID, []string{task.TaskID}, taskM)
			require.Error(t, err)
			require.Contains(t, err.Error(), strconv.Itoa(status))

			var reloaded model.Task
			require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
			require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status, "a non-2xx poll must not update the task")
			require.Equal(t, int64(initQuota), getUserQuota(t, userID))
		})
	}
}

func TestTaskPollStatusAccepted(t *testing.T) {
	for code, want := range map[int]bool{
		199: false, 200: true, 201: true, 202: true, 299: true,
		300: false, 400: false, 502: false,
	} {
		require.Equal(t, want, taskPollStatusAccepted(code), "status %d", code)
	}
}
