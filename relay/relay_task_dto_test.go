package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
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
