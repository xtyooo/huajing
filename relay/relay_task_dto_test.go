package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskModel2DtoIncludesCachedImageFields(t *testing.T) {
	task := &model.Task{
		ID:          1,
		CreatedAt:   1_700_000_000,
		UpdatedAt:   1_700_000_010,
		TaskID:      "task_image",
		Platform:    constant.TaskPlatformImage,
		Action:      constant.TaskActionImageGenerate,
		Status:      model.TaskStatusSuccess,
		MediaURL:    "https://huajingapi.top/media/task_image.png",
		MediaStatus: model.MediaStatusSuccess,
		SubmitTime:  1_700_000_000,
		StartTime:   1_700_000_000,
		FinishTime:  1_700_000_010,
		Progress:    "100%",
		PrivateData: model.TaskPrivateData{ResultURL: "https://upstream.example/private.png"},
	}

	dto := TaskModel2Dto(task)

	assert.Equal(t, task.CreatedAt, dto.CreatedAt)
	assert.Equal(t, task.SubmitTime, dto.SubmitTime)
	assert.Equal(t, task.MediaURL, dto.MediaURL)
	assert.Equal(t, model.MediaStatusSuccess, dto.MediaStatus)
	assert.Equal(t, "下载成功", dto.MediaStatusDesc)
}

func TestTaskModel2DtoKeepsVideoInProgressUntilMediaCached(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_video_pending",
		Platform:    constant.TaskPlatform("60"),
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		FinishTime:  1_700_000_010,
		MediaStatus: model.MediaStatusDownloading,
	}

	result := TaskModel2Dto(task)

	assert.Equal(t, string(model.TaskStatusInProgress), result.Status)
	assert.Equal(t, "99%", result.Progress)
	assert.Zero(t, result.FinishTime)
	assert.Empty(t, result.MediaURL)
}

func TestTaskModel2DtoCompletesVideoAfterMediaCached(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_video_cached",
		Platform:    constant.TaskPlatform("60"),
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		FinishTime:  1_700_000_010,
		MediaURL:    "https://huajingapi.top/media/task_video_cached.mp4",
		MediaStatus: model.MediaStatusSuccess,
	}

	result := TaskModel2Dto(task)

	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, task.FinishTime, result.FinishTime)
	assert.Equal(t, task.MediaURL, result.MediaURL)
}

func TestTaskModel2DtoReportsVideoCacheFailure(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_video_failed",
		Platform:    constant.TaskPlatform("60"),
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		FailReason:  "download failed with status 403 Forbidden",
		MediaStatus: model.MediaStatusFailed,
	}

	result := TaskModel2Dto(task)

	assert.Equal(t, string(model.TaskStatusFailure), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, task.FailReason, result.FailReason)
}

func TestApplyVideoMediaPresentationMasksCompletedResponseUntilCached(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_video_pending",
		Platform:    constant.TaskPlatform("60"),
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		MediaStatus: model.MediaStatusPending,
	}
	raw := []byte(`{"id":"task_video_pending","status":"completed","progress":100,"completed_at":1700000010}`)

	result := applyVideoMediaPresentation(raw, task)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(result, &video))
	assert.Equal(t, dto.VideoStatusInProgress, video.Status)
	assert.Equal(t, 99, video.Progress)
	assert.Zero(t, video.CompletedAt)
}

func TestOverwriteResultMediaURLsReplacesNestedResultURL(t *testing.T) {
	raw := []byte(`{"status":"completed","metadata":{"result_url":"https://upstream.example/private.mp4"}}`)

	result := overwriteResultMediaURLs(raw, "https://local.example/media/task.mp4")

	var body map[string]any
	require.NoError(t, common.Unmarshal(result, &body))
	metadata, ok := body["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://local.example/media/task.mp4", metadata["result_url"])
}
