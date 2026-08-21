package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// fakeSunoAdaptor returns a canned "FAILURE" response for every FetchTask
// call, regardless of how many times it's called - simulating an upstream
// that keeps reporting the same failed task across multiple poll cycles.
type fakeSunoAdaptor struct {
	responseBody string
}

func (f *fakeSunoAdaptor) Init(info *relaycommon.RelayInfo) {}

func (f *fakeSunoAdaptor) FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(f.responseBody)),
	}, nil
}

func (f *fakeSunoAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (f *fakeSunoAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func sunoFailureResponseBody(t *testing.T, taskID, failReason string) string {
	t.Helper()
	resp := dto.TaskResponse[[]dto.SunoDataResponse]{
		Code: dto.TaskSuccessCode,
		Data: []dto.SunoDataResponse{
			{
				TaskID:     taskID,
				Status:     string(model.TaskStatusFailure),
				FailReason: failReason,
				Data:       json.RawMessage(`{}`),
			},
		},
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	return string(b)
}

func seedChannelWithBaseURL(t *testing.T, id int) {
	t.Helper()
	baseURL := "https://example.invalid"
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", BaseURL: &baseURL}
	require.NoError(t, model.DB.Create(ch).Error)
}

// TestUpdateSunoTasks_DoesNotDoubleRefundAcrossPolls reproduces a real race:
// updateSunoTasks called RefundTaskQuota unconditionally whenever the
// upstream reported FAILURE, with no check for whether this task was
// already refunded on a previous poll. An upstream that keeps reporting the
// same failed task (a slow-to-clear queue, overlapping poll cycles) would be
// refunded once per poll instead of once total.
func TestUpdateSunoTasks_DoesNotDoubleRefundAcrossPolls(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-suno-cas", 5000)
	seedChannelWithBaseURL(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "suno-task-1"
	task.Status = model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(task).Error)

	prevAdaptorFn := GetTaskAdaptorFunc
	t.Cleanup(func() { GetTaskAdaptorFunc = prevAdaptorFn })

	// First poll: task transitions IN_PROGRESS -> FAILURE, must refund once.
	fake1 := &fakeSunoAdaptor{responseBody: sunoFailureResponseBody(t, task.TaskID, "upstream build failed")}
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor { return fake1 }
	taskM := map[string]*model.Task{task.TaskID: task}
	require.NoError(t, updateSunoTasks(ctx, channelID, []string{task.TaskID}, taskM))
	require.Equal(t, int64(initQuota+preConsumed), getUserQuota(t, userID))

	// Second poll on a task already FAILURE, but with a *different*
	// fail_reason on the response (e.g. upstream retried its own error
	// reporting) - taskNeedsUpdate sees a real field change and lets it
	// through, but the status was already terminal before this poll, so it
	// must not refund a second time.
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	fake2 := &fakeSunoAdaptor{responseBody: sunoFailureResponseBody(t, reloaded.TaskID, "upstream build failed (retry)")}
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor { return fake2 }
	taskM2 := map[string]*model.Task{reloaded.TaskID: &reloaded}
	require.NoError(t, updateSunoTasks(ctx, channelID, []string{reloaded.TaskID}, taskM2))
	require.Equal(t, int64(initQuota+preConsumed), getUserQuota(t, userID), "a second poll on an already-failed task must not refund again")
}
