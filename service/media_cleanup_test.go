package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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

func TestMediaCleanupIntervalUsesCurrentConfig(t *testing.T) {
	previous := common.GetMediaCleanupConfig()
	common.SetMediaCleanupConfig(common.MediaCleanupConfig{CleanupInterval: 7, CleanupAge: 180})
	t.Cleanup(func() { common.SetMediaCleanupConfig(previous) })

	assert.Equal(t, 7*time.Minute, mediaCleanupInterval())
}
