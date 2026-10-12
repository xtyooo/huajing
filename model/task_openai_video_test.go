package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskToOpenAIVideoDoesNotExposeResultURL(t *testing.T) {
	task := &Task{
		TaskID:    "task_public",
		Status:    TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 10,
		UpdatedAt: 20,
		Properties: Properties{
			OriginModelName: "video-model",
		},
		PrivateData: TaskPrivateData{
			ResultURL: "https://upstream.example/video.mp4?signature=secret",
		},
	}

	video := task.ToOpenAIVideo()

	assert.Equal(t, "task_public", video.ID)
	assert.Equal(t, "video", video.Object)
	assert.Equal(t, "completed", video.Status)
	assert.Nil(t, video.Metadata)

	encoded, err := common.Marshal(video)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "upstream.example")
	assert.NotContains(t, string(encoded), "signature")
}

func TestTaskToOpenAIVideoRequiresStableNativeCache(t *testing.T) {
	for _, tc := range []struct {
		name         string
		task         Task
		wantStatus   string
		wantProgress int
	}{
		{name: "historical native cache not scheduled", task: Task{Platform: "81", MediaStatus: MediaStatusNotNeed, MediaURL: "https://local.example/stale.mp4"}, wantStatus: "in_progress", wantProgress: 99},
		{name: "native successful cache missing stable url", task: Task{Platform: "81", MediaStatus: MediaStatusSuccess}, wantStatus: "in_progress", wantProgress: 99},
		{name: "native ready", task: Task{Platform: "81", MediaStatus: MediaStatusSuccess, MediaURL: "https://local.example/ready.mp4"}, wantStatus: "completed", wantProgress: 100},
		{name: "kuai direct link", task: Task{Platform: "65", MediaStatus: MediaStatusNotNeed, MediaURL: "https://cdn.example/direct.mp4"}, wantStatus: "completed", wantProgress: 100},
		{name: "suno audio", task: Task{Platform: constant.TaskPlatformSuno}, wantStatus: "completed", wantProgress: 100},
		{name: "numeric suno audio", task: Task{Platform: "36"}, wantStatus: "completed", wantProgress: 100},
		{name: "image", task: Task{Platform: constant.TaskPlatformImage}, wantStatus: "completed", wantProgress: 100},
		{name: "sora image-size snapshot", task: Task{Platform: "55", PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{ImageSizePricing: true}}}, wantStatus: "completed", wantProgress: 100},
		{name: "doubao image action", task: Task{Platform: "54", Action: constant.TaskActionImageGenerate}, wantStatus: "completed", wantProgress: 100},
		{name: "snumom image action", task: Task{Platform: "81", Action: constant.TaskActionImageEdit}, wantStatus: "completed", wantProgress: 100},
		{name: "native image protocol", task: Task{Platform: "55", PrivateData: TaskPrivateData{Execution: &TaskExecutionSnapshot{RequestPath: "/v1/images/generations"}}}, wantStatus: "completed", wantProgress: 100},
		{name: "plugin", task: Task{Platform: "sora", PrivateData: TaskPrivateData{Execution: &TaskExecutionSnapshot{TaskPlugin: &TaskPluginSnapshot{Key: "sora"}}}}, wantStatus: "completed", wantProgress: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.task.TaskID = "fixture_native_cache"
			tc.task.Status = TaskStatusSuccess
			tc.task.Progress = "100%"
			tc.task.FinishTime = 123
			video := tc.task.ToOpenAIVideo()
			assert.Equal(t, tc.wantStatus, video.Status)
			encoded, err := common.Marshal(video)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, common.Unmarshal(encoded, &fields))
			assert.Equal(t, float64(tc.wantProgress), fields["progress"])
			if tc.wantStatus == "in_progress" {
				assert.Zero(t, video.CompletedAt)
				assert.Nil(t, video.Metadata)
				assert.NotContains(t, fields, "completed_at")
			}
		})
	}
}

func TestTaskToOpenAIVideoStatusAndCompletedAt(t *testing.T) {
	tests := []struct {
		name                string
		task                Task
		wantStatus          string
		wantCompletedAt     int64
		wantCompletedAtJSON bool
	}{
		{
			name:            "not start maps to queued",
			task:            Task{TaskID: "t1", Status: TaskStatusNotStart, CreatedAt: 10, UpdatedAt: 20},
			wantStatus:      "queued",
			wantCompletedAt: 0,
		},
		{
			name:            "in progress omits completed at",
			task:            Task{TaskID: "t2", Status: TaskStatusInProgress, CreatedAt: 10, UpdatedAt: 20, FinishTime: 30},
			wantStatus:      "in_progress",
			wantCompletedAt: 0,
		},
		{
			name:                "success uses finish time",
			task:                Task{TaskID: "t3", Status: TaskStatusSuccess, CreatedAt: 10, UpdatedAt: 20, FinishTime: 30},
			wantStatus:          "completed",
			wantCompletedAt:     30,
			wantCompletedAtJSON: true,
		},
		{
			name:                "success falls back to updated at",
			task:                Task{TaskID: "t4", Status: TaskStatusSuccess, CreatedAt: 10, UpdatedAt: 20},
			wantStatus:          "completed",
			wantCompletedAt:     20,
			wantCompletedAtJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			video := tt.task.ToOpenAIVideo()
			assert.Equal(t, tt.wantStatus, video.Status)
			assert.Equal(t, tt.wantCompletedAt, video.CompletedAt)

			encoded, err := common.Marshal(video)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, common.Unmarshal(encoded, &fields))
			if tt.wantCompletedAtJSON {
				require.Contains(t, fields, "completed_at")
				assert.Equal(t, float64(tt.wantCompletedAt), fields["completed_at"])
			} else {
				assert.NotContains(t, fields, "completed_at")
			}
		})
	}
}
