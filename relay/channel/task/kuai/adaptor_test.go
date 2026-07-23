package kuai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultCompletedUsesMetadataURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"id":"task_upstream",
		"status":"completed",
		"progress":100,
		"metadata":{
			"url":"https://example.com/result.mp4",
			"total_tokens":40594,
			"completion_tokens":"123"
		}
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, "https://example.com/result.mp4", result.Url)
	assert.Equal(t, 40594, result.TotalTokens)
	assert.Equal(t, 123, result.CompletionTokens)
}

func TestParseTaskResultCompletedFallsBackToVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"id":"task_upstream",
		"status":"succeeded",
		"video_url":"https://example.com/video-url.mp4"
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "https://example.com/video-url.mp4", result.Url)
}

func TestConvertToOpenAIVideoKeepsDirectResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		CreatedAt:  1784011914,
		FinishTime: 1784012258,
		Properties: model.Properties{
			OriginModelName: "doubao-seedance-2-0-fast-260128",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://example.com/direct.mp4",
		},
		Data: []byte(`{
			"id":"task_upstream",
			"status":"completed",
			"metadata":{"url":"https://example.com/upstream.mp4","total_tokens":40594}
		}`),
	}

	data, err := adaptor.ConvertToOpenAIVideo(task)

	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, common.Unmarshal(data, &out))
	assert.Equal(t, "task_public", out["id"])
	assert.Equal(t, "completed", out["status"])
	metadata, ok := out["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/direct.mp4", metadata["url"])
	assert.Equal(t, float64(40594), metadata["total_tokens"])
}
