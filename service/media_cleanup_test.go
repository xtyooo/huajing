package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaCleanupRestoresFilesWhenDatabaseUpdateFails(t *testing.T) {
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)
	fileName := time.Now().Add(-2*time.Hour).Format("20060102150405") + "_task_cleanup_test.mp4"
	filePath := filepath.Join(mediaDir, fileName)
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	previousClean := cleanMediaByTaskIDs
	cleanMediaByTaskIDs = func([]string) error { return errors.New("database unavailable") }
	t.Cleanup(func() { cleanMediaByTaskIDs = previousClean })

	result, err := RunMediaCleanupWithAge(time.Hour)

	require.Error(t, err)
	assert.Nil(t, result)
	content, readErr := os.ReadFile(filePath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("video"), content)
}

func TestMediaCleanupKeepsTaskIdentityForUniqueInlineFiles(t *testing.T) {
	truncate(t)
	mediaDir := t.TempDir()
	t.Setenv(common.MediaDirEnv, mediaDir)
	for _, suffix := range []string{"", ".abcdef0123456789"} {
		taskID := "task_cleanup_inline"
		if suffix == "" {
			taskID = "task_cleanup_legacy"
		}
		fileName := time.Now().Add(-2*time.Hour).Format("20060102150405") + "_" + taskID + suffix + ".mp4"
		filePath := filepath.Join(mediaDir, fileName)
		require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o600))
		task := &model.Task{TaskID: taskID, Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusSuccess, MediaURL: "https://local.example.test/media/" + fileName}
		require.NoError(t, model.DB.Create(task).Error)
	}
	result, err := RunMediaCleanupWithAge(time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 2, result.DeletedCount)
	for _, taskID := range []string{"task_cleanup_inline", "task_cleanup_legacy"} {
		var loaded model.Task
		require.NoError(t, model.DB.Where("task_id = ?", taskID).First(&loaded).Error)
		assert.Equal(t, model.MediaStatusCleaned, loaded.MediaStatus)
		assert.Empty(t, loaded.MediaURL)
	}
}

func TestMediaCleanupIntervalUsesCurrentConfig(t *testing.T) {
	previous := common.GetMediaCleanupConfig()
	common.SetMediaCleanupConfig(common.MediaCleanupConfig{CleanupInterval: 7, CleanupAge: 180})
	t.Cleanup(func() { common.SetMediaCleanupConfig(previous) })

	assert.Equal(t, 7*time.Minute, mediaCleanupInterval())
}
