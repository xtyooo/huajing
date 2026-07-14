package service

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMediaDownloadResponseRejectsNonSuccessResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
	}

	err := validateMediaDownloadResponse(resp)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestValidateMediaDownloadResponseRejectsNilResponse(t *testing.T) {
	err := validateMediaDownloadResponse(nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestValidateMediaDownloadResponseRejectsNilBody(t *testing.T) {
	err := validateMediaDownloadResponse(&http.Response{StatusCode: http.StatusOK})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response body")
}

func TestValidateMediaDownloadResponseRejectsOversizedFile(t *testing.T) {
	oldLimit := constant.MaxMediaDownloadMB
	constant.MaxMediaDownloadMB = 1
	t.Cleanup(func() { constant.MaxMediaDownloadMB = oldLimit })

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		ContentLength: 1024*1024 + 1,
		Body:          http.NoBody,
	}

	err := validateMediaDownloadResponse(resp)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1MB")
}

func TestValidateMediaDownloadResponseAcceptsFileWithinLimit(t *testing.T) {
	oldLimit := constant.MaxMediaDownloadMB
	constant.MaxMediaDownloadMB = 1
	t.Cleanup(func() { constant.MaxMediaDownloadMB = oldLimit })

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		ContentLength: int64(len("video data")),
		Body:          http.NoBody,
	}

	err := validateMediaDownloadResponse(resp)

	require.NoError(t, err)
}

func TestValidateMediaDownloadResponseRejectsNoContent(t *testing.T) {
	err := validateMediaDownloadResponse(&http.Response{
		StatusCode:    http.StatusNoContent,
		Status:        "204 No Content",
		ContentLength: 0,
		Body:          http.NoBody,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response body")
}

func TestValidateMediaDownloadResponseRejectsJSONError(t *testing.T) {
	err := validateMediaDownloadResponse(&http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		ContentLength: 124,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(`{"error":{"message":"Invalid token"}}`)),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected content type application/json")
}

func TestUnexpectedMediaContentTypeRecognizesProviderAuthError(t *testing.T) {
	assert.True(t, isUnexpectedMediaContentType("application/json; charset=utf-8"))
	assert.True(t, isUnexpectedMediaContentType("text/html"))
	assert.False(t, isUnexpectedMediaContentType("video/mp4"))
	assert.False(t, isUnexpectedMediaContentType("application/octet-stream"))
}

func TestResolveSoraMediaDownloadTargetUsesUpstreamURLAndTaskKey(t *testing.T) {
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })

	baseURL := "https://sora.example.test"
	channel := &model.Channel{
		Id:      990055,
		Type:    constant.ChannelTypeSora,
		Key:     "fallback-key",
		BaseURL: &baseURL,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	t.Cleanup(func() { model.DB.Delete(channel) })
	task := &model.Task{
		ChannelId: channel.Id,
		Platform:  constant.TaskPlatform("55"),
		TaskID:    "task_public",
		PrivateData: model.TaskPrivateData{
			Key:            "selected-key",
			UpstreamTaskID: "video_upstream_123",
			ResultURL:      "https://local.example/v1/videos/task_public/content",
		},
	}

	target, err := resolveMediaDownloadTarget(task)

	require.NoError(t, err)
	assert.Equal(t, "https://sora.example.test/v1/videos/video_upstream_123/content", target.URL)
	require.NotEmpty(t, target.Headers)
	assert.Equal(t, "Bearer selected-key", target.Headers[0]["Authorization"])
}

func TestTaskDownloadKeyPrefersKeyStoredWithTask(t *testing.T) {
	task := &model.Task{PrivateData: model.TaskPrivateData{Key: "selected-key"}}

	key, err := taskDownloadKey(task)

	require.NoError(t, err)
	assert.Equal(t, "selected-key", key)
}

func TestRetryableMediaDownloadStatus(t *testing.T) {
	assert.True(t, isRetryableMediaDownloadStatus(http.StatusNotFound))
	assert.True(t, isRetryableMediaDownloadStatus(http.StatusTooManyRequests))
	assert.True(t, isRetryableMediaDownloadStatus(http.StatusBadGateway))
	assert.False(t, isRetryableMediaDownloadStatus(http.StatusUnauthorized))
}

func TestStreamMediaDownloadToFile(t *testing.T) {
	oldLimit := constant.MaxMediaDownloadMB
	constant.MaxMediaDownloadMB = 1
	t.Cleanup(func() { constant.MaxMediaDownloadMB = oldLimit })

	dir := t.TempDir()
	path := filepath.Join(dir, "video.mp4")
	require.NoError(t, streamMediaDownloadToFile(bytes.NewBufferString("video data"), path))

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("video data"), body)
}

func TestStreamMediaDownloadRemovesOversizedPartialFile(t *testing.T) {
	oldLimit := constant.MaxMediaDownloadMB
	constant.MaxMediaDownloadMB = 1
	t.Cleanup(func() { constant.MaxMediaDownloadMB = oldLimit })

	path := filepath.Join(t.TempDir(), "video.mp4")
	err := streamMediaDownloadToFile(strings.NewReader(strings.Repeat("x", 1024*1024+1)), path)

	require.Error(t, err)
	assert.NoFileExists(t, path)
}

func TestStreamMediaDownloadRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	err := streamMediaDownloadToFile(bytes.NewReader(nil), path)

	require.Error(t, err)
	assert.NoFileExists(t, path)
}

func TestCleanupStaleMediaDownloadTempFiles(t *testing.T) {
	dir := t.TempDir()
	stalePath := filepath.Join(dir, ".media-download-stale")
	freshPath := filepath.Join(dir, ".media-download-fresh")
	regularPath := filepath.Join(dir, "video.mp4")
	for _, path := range []string{stalePath, freshPath, regularPath} {
		require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
	}
	staleTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stalePath, staleTime, staleTime))

	require.NoError(t, cleanupStaleMediaDownloadTempFiles(dir, time.Hour))

	assert.NoFileExists(t, stalePath)
	assert.FileExists(t, freshPath)
	assert.FileExists(t, regularPath)
}
