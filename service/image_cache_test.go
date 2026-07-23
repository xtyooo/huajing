package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
}

func TestCacheImageResponseDownloadsURLAndPersistsTask(t *testing.T) {
	truncate(t)
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)
	t.Setenv("MEDIA_BASE_URL", "https://huajingapi.top/")

	previousFetch := fetchImageResult
	fetchImageResult = func(_ context.Context, rawURL string) (*http.Response, error) {
		require.Equal(t, "https://upstream.example/generated.png", rawURL)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{"Content-Type": []string{"image/png"}},
			Body:          io.NopCloser(bytes.NewReader(testPNG)),
			ContentLength: int64(len(testPNG)),
		}, nil
	}
	t.Cleanup(func() { fetchImageResult = previousFetch })

	body := []byte(`{"created":123,"data":[{"url":"https://upstream.example/generated.png","revised_prompt":"revised"}]}`)
	rewritten, err := CacheImageResponse(body, ImageCacheParams{
		UserID:            7,
		ChannelID:         9,
		Quota:             100,
		Group:             "default",
		Action:            constant.TaskActionImageGenerate,
		Prompt:            "draw a lake",
		ModelName:         "image-model",
		UpstreamModelName: "upstream-image-model",
	})

	require.NoError(t, err)
	var response dto.ImageResponse
	require.NoError(t, common.Unmarshal(rewritten, &response))
	require.Len(t, response.Data, 1)
	assert.True(t, strings.HasPrefix(response.Data[0].Url, "https://huajingapi.top/media/"))
	assert.Empty(t, response.Data[0].B64Json)
	assert.Equal(t, "revised", response.Data[0].RevisedPrompt)

	var tasks []model.Task
	require.NoError(t, model.DB.Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 1)
	task := tasks[0]
	assert.Equal(t, constant.TaskPlatform(constant.TaskPlatformImage), task.Platform)
	assert.Equal(t, constant.TaskActionImageGenerate, task.Action)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, model.MediaStatusSuccess, task.MediaStatus)
	assert.Equal(t, response.Data[0].Url, task.MediaURL)
	assert.Equal(t, "https://upstream.example/generated.png", task.PrivateData.ResultURL)
	assert.Equal(t, "draw a lake", task.Properties.Input)
	assert.Equal(t, "image-model", task.Properties.OriginModelName)
	assert.Equal(t, "upstream-image-model", task.Properties.UpstreamModelName)
	assert.Equal(t, 100, task.Quota)
	assert.NotZero(t, task.CreatedAt)
	assert.NotZero(t, task.UpdatedAt)

	fileName := filepath.Base(response.Data[0].Url)
	assert.Regexp(t, `^\d{14}_task_\w+\.png$`, fileName)
	stored, readErr := os.ReadFile(filepath.Join(mediaDir, fileName))
	require.NoError(t, readErr)
	assert.Equal(t, testPNG, stored)
	assert.NotContains(t, string(task.Data), "upstream.example")
}

func TestCacheImageResponsePreservesTopLevelFieldsAndRemovesUpstreamURLs(t *testing.T) {
	truncate(t)
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)

	previousFetch := fetchImageResult
	fetchImageResult = func(_ context.Context, _ string) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{"Content-Type": []string{"image/png"}},
			Body:          io.NopCloser(bytes.NewReader(testPNG)),
			ContentLength: int64(len(testPNG)),
		}, nil
	}
	t.Cleanup(func() { fetchImageResult = previousFetch })

	body := []byte(`{"id":"img_123","created":123,"usage":{"total_tokens":9},"metadata":{"output":{"url":"https://upstream.example/generated.png"}},"data":[{"url":"https://upstream.example/generated.png","b64_json":"unused","revised_prompt":"revised","seed":42}]}`)
	rewritten, err := CacheImageResponse(body, ImageCacheParams{Action: constant.TaskActionImageGenerate})

	require.NoError(t, err)
	var response map[string]interface{}
	require.NoError(t, common.Unmarshal(rewritten, &response))
	assert.Equal(t, "img_123", response["id"])
	assert.Equal(t, float64(9), response["usage"].(map[string]interface{})["total_tokens"])
	item := response["data"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, float64(42), item["seed"])
	assert.NotContains(t, item, "b64_json")
	assert.NotContains(t, string(rewritten), "upstream.example")
}

func TestImageCacheSessionRecordsRequestStartTimeAndFailure(t *testing.T) {
	truncate(t)
	createdAt := int64(1_700_000_000)
	session, err := BeginImageCacheSession(ImageCacheParams{
		UserID:    15,
		ChannelID: 16,
		Action:    constant.TaskActionImageEdit,
		CreatedAt: createdAt,
	})
	require.NoError(t, err)

	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, createdAt, task.CreatedAt)
	assert.Equal(t, createdAt, task.SubmitTime)
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)

	session.Fail(assert.AnError)
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Equal(t, "100%", task.Progress)
	assert.NotZero(t, task.FinishTime)
}

func TestImageCacheSessionCanMoveToAnotherChannelBeforeCompletion(t *testing.T) {
	truncate(t)
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)
	t.Setenv("MEDIA_BASE_URL", "https://huajingapi.top")
	session, err := BeginImageCacheSession(ImageCacheParams{
		UserID:            17,
		ChannelID:         1,
		Action:            constant.TaskActionImageGenerate,
		UpstreamModelName: "first-model",
	})
	require.NoError(t, err)

	require.NoError(t, session.SetAttempt(2, "second-model"))
	require.False(t, session.Finished())

	var tasks []model.Task
	require.NoError(t, model.DB.Find(&tasks).Error)
	require.Len(t, tasks, 1)
	assert.Equal(t, 2, tasks[0].ChannelId)
	assert.Equal(t, "second-model", tasks[0].Properties.UpstreamModelName)
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), tasks[0].Status)

	body := []byte(`{"created":123,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(testPNG) + `"}]}`)
	rewritten, err := session.CacheResponse(body)
	require.NoError(t, err)
	assert.Contains(t, string(rewritten), "https://huajingapi.top/media/")
	require.NoError(t, model.DB.Find(&tasks).Error)
	require.Len(t, tasks, 1)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), tasks[0].Status)
	assert.Equal(t, 2, tasks[0].ChannelId)
}

func TestCacheImageResponseInvalidBodyRecordsFailure(t *testing.T) {
	truncate(t)
	_, err := CacheImageResponse([]byte(`{"data":`), ImageCacheParams{
		Action: constant.TaskActionImageGenerate,
	})

	require.Error(t, err)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Equal(t, model.MediaStatusFailed, task.MediaStatus)
}

func TestCacheImageResponseMissingMediaDirectoryRecordsFailure(t *testing.T) {
	truncate(t)
	t.Setenv(common.MediaDirEnv, filepath.Join(t.TempDir(), "missing"))
	body := []byte(`{"created":123,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(testPNG) + `"}]}`)

	_, err := CacheImageResponse(body, ImageCacheParams{Action: constant.TaskActionImageGenerate})

	require.Error(t, err)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Equal(t, model.MediaStatusFailed, task.MediaStatus)
	assert.Contains(t, task.FailReason, "media directory is unavailable")
}

func TestCacheImageResponseStoresBase64AndCreatesOneTaskPerImage(t *testing.T) {
	truncate(t)
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)
	t.Setenv("MEDIA_BASE_URL", "https://huajingapi.top")

	encoded := base64.StdEncoding.EncodeToString(testPNG)
	body := []byte(`{"created":456,"data":[{"b64_json":"` + encoded + `"},{"b64_json":"data:image/png;base64,` + encoded + `"}]}`)
	rewritten, err := CacheImageResponse(body, ImageCacheParams{
		UserID:    8,
		ChannelID: 10,
		Quota:     101,
		Group:     "vip",
		Action:    constant.TaskActionImageEdit,
		Prompt:    "change the sky",
		ModelName: "edit-model",
	})

	require.NoError(t, err)
	var response dto.ImageResponse
	require.NoError(t, common.Unmarshal(rewritten, &response))
	require.Len(t, response.Data, 2)
	assert.NotEqual(t, response.Data[0].Url, response.Data[1].Url)
	for _, item := range response.Data {
		assert.True(t, strings.HasPrefix(item.Url, "https://huajingapi.top/media/"))
		assert.Empty(t, item.B64Json)
		_, statErr := os.Stat(filepath.Join(mediaDir, filepath.Base(item.Url)))
		require.NoError(t, statErr)
	}

	var tasks []model.Task
	require.NoError(t, model.DB.Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	assert.Equal(t, constant.TaskActionImageEdit, tasks[0].Action)
	assert.Equal(t, constant.TaskActionImageEdit, tasks[1].Action)
	assert.Equal(t, 51, tasks[0].Quota)
	assert.Equal(t, 50, tasks[1].Quota)
	assert.Equal(t, float64(1), tasks[0].Properties.Extra["image_index"])
	assert.Equal(t, float64(2), tasks[1].Properties.Extra["image_index"])
}

func TestCacheImageResponseRejectsNonImageContentAndRecordsFailure(t *testing.T) {
	truncate(t)
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)

	previousFetch := fetchImageResult
	fetchImageResult = func(_ context.Context, _ string) (*http.Response, error) {
		body := []byte(`{"error":"invalid token"}`)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
		}, nil
	}
	t.Cleanup(func() { fetchImageResult = previousFetch })

	body := []byte(`{"created":789,"data":[{"url":"https://upstream.example/not-image"}]}`)
	_, err := CacheImageResponse(body, ImageCacheParams{
		UserID:    11,
		ChannelID: 12,
		Action:    constant.TaskActionImageGenerate,
		ModelName: "image-model",
	})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "upstream.example")
	var tasks []model.Task
	require.NoError(t, model.DB.Find(&tasks).Error)
	require.Len(t, tasks, 1)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), tasks[0].Status)
	assert.Equal(t, model.MediaStatusFailed, tasks[0].MediaStatus)
	assert.NotEmpty(t, tasks[0].FailReason)

	entries, readErr := os.ReadDir(mediaDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

func TestCacheImageResponseRollsBackEarlierImagesWhenLaterImageFails(t *testing.T) {
	truncate(t)
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)

	callCount := 0
	previousFetch := fetchImageResult
	fetchImageResult = func(_ context.Context, _ string) (*http.Response, error) {
		callCount++
		body := testPNG
		if callCount == 2 {
			body = []byte("not an image")
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
		}, nil
	}
	t.Cleanup(func() { fetchImageResult = previousFetch })

	body := []byte(`{"created":999,"data":[{"url":"https://upstream.example/one"},{"url":"https://upstream.example/two"}]}`)
	_, err := CacheImageResponse(body, ImageCacheParams{
		UserID:    12,
		ChannelID: 13,
		Quota:     20,
		Action:    constant.TaskActionImageGenerate,
		ModelName: "image-model",
	})

	require.Error(t, err)
	var tasks []model.Task
	require.NoError(t, model.DB.Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
		assert.Equal(t, model.MediaStatusFailed, task.MediaStatus)
		assert.Empty(t, task.MediaURL)
	}
	entries, readErr := os.ReadDir(mediaDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}
