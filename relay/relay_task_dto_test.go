package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	assert.Empty(t, dto.VideoURL)
	assert.Empty(t, dto.ResultURL)
	assert.Equal(t, model.MediaStatusSuccess, dto.MediaStatus)
	assert.Equal(t, "下载成功", dto.MediaStatusDesc)
}

func TestTaskModel2DtoDoesNotAddVideoAliasesToSunoAudio(t *testing.T) {
	task := &model.Task{
		TaskID:      "suno_audio_task",
		Platform:    constant.TaskPlatformSuno,
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		MediaURL:    "https://huajingapi.top/media/song.mp3",
		MediaStatus: model.MediaStatusSuccess,
	}

	result := TaskModel2Dto(task)

	assert.Equal(t, task.MediaURL, result.MediaURL)
	assert.Empty(t, result.VideoURL)
	assert.Empty(t, result.ResultURL)
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
	assert.Empty(t, result.VideoURL)
	assert.Empty(t, result.ResultURL)
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
	assert.Equal(t, task.MediaURL, result.VideoURL)
	assert.Equal(t, task.MediaURL, result.ResultURL)
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
	assert.Empty(t, result.VideoURL)
	assert.Empty(t, result.ResultURL)
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

func TestVideoFetchEndpointsExposeOnlyCachedPublicURLAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&model.Task{}))
	model.DB = testDB
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, dbErr := testDB.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	const mediaURL = "https://huajingapi.top/media/task_cached.mp4"
	tasks := []*model.Task{
		{
			TaskID:      "task_cached",
			UserId:      2276,
			Platform:    constant.TaskPlatform("76"),
			Status:      model.TaskStatusSuccess,
			Progress:    "100%",
			MediaURL:    mediaURL,
			MediaStatus: model.MediaStatusSuccess,
			PrivateData: model.TaskPrivateData{ResultURL: "https://upstream.example/private.mp4"},
		},
		{
			TaskID:      "task_pending",
			UserId:      2276,
			Platform:    constant.TaskPlatform("76"),
			Status:      model.TaskStatusSuccess,
			Progress:    "100%",
			MediaURL:    "https://huajingapi.top/media/stale.mp4",
			MediaStatus: model.MediaStatusDownloading,
			PrivateData: model.TaskPrivateData{ResultURL: "https://upstream.example/pending-private.mp4"},
		},
		{
			TaskID:      "task_failed",
			UserId:      2276,
			Platform:    constant.TaskPlatform("76"),
			Status:      model.TaskStatusSuccess,
			Progress:    "100%",
			MediaStatus: model.MediaStatusFailed,
			FailReason:  "cache download failed",
		},
	}
	for _, task := range tasks {
		require.NoError(t, task.Insert())
	}

	tests := []struct {
		name       string
		path       string
		taskID     string
		dataPrefix string
		wantStatus string
		wantURL    bool
	}{
		{name: "openai cached", path: "/v1/videos/task_cached", taskID: "task_cached", wantStatus: dto.VideoStatusCompleted, wantURL: true},
		{name: "legacy cached", path: "/v1/video/generations/task_cached", taskID: "task_cached", dataPrefix: "data.", wantStatus: string(model.TaskStatusSuccess), wantURL: true},
		{name: "openai pending", path: "/v1/videos/task_pending", taskID: "task_pending", wantStatus: dto.VideoStatusInProgress},
		{name: "legacy pending", path: "/v1/video/generations/task_pending", taskID: "task_pending", dataPrefix: "data.", wantStatus: string(model.TaskStatusInProgress)},
		{name: "openai failed", path: "/v1/videos/task_failed", taskID: "task_failed", wantStatus: dto.VideoStatusFailed},
		{name: "legacy failed", path: "/v1/video/generations/task_failed", taskID: "task_failed", dataPrefix: "data.", wantStatus: string(model.TaskStatusFailure)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, test.path, nil)
			ctx.Params = gin.Params{{Key: "task_id", Value: test.taskID}}
			ctx.Set("id", 2276)

			body, taskErr := videoFetchByIDRespBodyBuilder(ctx)
			require.Nil(t, taskErr)
			assert.NotContains(t, string(body), "upstream.example")
			assert.Equal(t, test.wantStatus, stringValueAtPath(t, body, test.dataPrefix+"status"))
			if test.dataPrefix == "" {
				assert.Equal(t, test.taskID, stringValueAtPath(t, body, "id"))
			} else {
				assert.Equal(t, test.taskID, stringValueAtPath(t, body, "data.task_id"))
			}
			if test.wantURL {
				assert.Equal(t, mediaURL, stringValueAtPath(t, body, test.dataPrefix+"video_url"))
				assert.Equal(t, mediaURL, stringValueAtPath(t, body, test.dataPrefix+"result_url"))
				if test.dataPrefix != "" {
					assert.Equal(t, mediaURL, stringValueAtPath(t, body, test.dataPrefix+"media_url"))
				} else {
					assert.Equal(t, mediaURL, stringValueAtPath(t, body, "metadata.url"))
				}
				return
			}
			if test.dataPrefix == "" {
				assert.NotContains(t, string(body), "stale.mp4")
			}
			assert.False(t, pathExists(t, body, test.dataPrefix+"video_url"))
			assert.False(t, pathExists(t, body, test.dataPrefix+"result_url"))
		})
	}
}

func TestCachedPublicVideoURLRejectsNonPublicStates(t *testing.T) {
	const staleURL = "https://huajingapi.top/media/stale.mp4"
	tests := []struct {
		name string
		task *model.Task
	}{
		{name: "nil task"},
		{name: "image task", task: &model.Task{Platform: constant.TaskPlatformImage, Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusSuccess, MediaURL: staleURL}},
		{name: "suno audio task", task: &model.Task{Platform: constant.TaskPlatformSuno, Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusSuccess, MediaURL: staleURL}},
		{name: "unknown task platform", task: &model.Task{Platform: constant.TaskPlatform("custom"), Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusSuccess, MediaURL: staleURL}},
		{name: "upstream failure", task: &model.Task{Platform: constant.TaskPlatform("76"), Status: model.TaskStatusFailure, MediaStatus: model.MediaStatusSuccess, MediaURL: staleURL}},
		{name: "cache pending", task: &model.Task{Platform: constant.TaskPlatform("76"), Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusPending, MediaURL: staleURL}},
		{name: "cache failed", task: &model.Task{Platform: constant.TaskPlatform("76"), Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusFailed, MediaURL: staleURL}},
		{name: "cache cleaned", task: &model.Task{Platform: constant.TaskPlatform("76"), Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusCleaned, MediaURL: staleURL}},
		{name: "missing URL", task: &model.Task{Platform: constant.TaskPlatform("76"), Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusSuccess}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Empty(t, cachedPublicVideoURL(test.task))
		})
	}
}

func stringValueAtPath(t *testing.T, body []byte, path string) string {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(body, &decoded))
	value, ok := valueAtPath(decoded, path).(string)
	require.True(t, ok, "expected string at %s in %s", path, string(body))
	return value
}

func pathExists(t *testing.T, body []byte, path string) bool {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(body, &decoded))
	return valueAtPath(decoded, path) != nil
}

func valueAtPath(decoded map[string]any, path string) any {
	current := any(decoded)
	for _, key := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = object[key]
		if !ok {
			return nil
		}
	}
	return current
}
