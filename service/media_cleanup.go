package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

var (
	mediaCleanupOnce    sync.Once
	mediaCleanupRunning atomic.Bool
)

var mediaFileNameRegex = regexp.MustCompile(`^(\d{14})_(task_\w+)\.\w+$`)

type MediaCleanupResult struct {
	DeletedCount int   `json:"deleted_count"`
	FreedBytes   int64 `json:"freed_bytes"`
}

func StartMediaCleanupTask() {
	mediaCleanupOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			cfg := common.GetMediaCleanupConfig()
			common.SysLog(fmt.Sprintf("media cleanup task started: interval=%dm, age=%dm", cfg.CleanupInterval, cfg.CleanupAge))

			RunMediaCleanup()
			for {
				cfg := common.GetMediaCleanupConfig()
				interval := time.Duration(cfg.CleanupInterval) * time.Minute
				time.Sleep(interval)
				RunMediaCleanup()
			}
		})
	})
}

func RunMediaCleanup() (*MediaCleanupResult, error) {
	age := time.Duration(common.GetMediaCleanupConfig().CleanupAge) * time.Minute
	return RunMediaCleanupWithAge(age)
}

func RunMediaCleanupWithAge(age time.Duration) (*MediaCleanupResult, error) {
	if !mediaCleanupRunning.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("media cleanup already running")
	}
	defer mediaCleanupRunning.Store(false)

	mediaDir := common.GetMediaDir()
	if mediaDir == "" {
		return nil, fmt.Errorf("MEDIA_DIR not configured")
	}

	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		return nil, fmt.Errorf("read media dir failed: %v", err)
	}

	cutoff := time.Now().Add(-age)

	var toDelete []string
	var taskIDs []string
	var freedBytes int64

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		matches := mediaFileNameRegex.FindStringSubmatch(name)
		if len(matches) < 3 {
			continue
		}

		timeStr := matches[1]
		taskID := matches[2]

		fileTime, err := time.ParseInLocation("20060102150405", timeStr, time.Local)
		if err != nil {
			continue
		}

		if fileTime.Before(cutoff) {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			toDelete = append(toDelete, filepath.Join(mediaDir, name))
			taskIDs = append(taskIDs, taskID)
			freedBytes += info.Size()
		}
	}

	if len(toDelete) == 0 {
		common.SysLog("media cleanup: no files to clean")
		return &MediaCleanupResult{DeletedCount: 0, FreedBytes: 0}, nil
	}

	if err := model.CleanMediaByTaskIDs(taskIDs); err != nil {
		common.SysError(fmt.Sprintf("media cleanup: update database failed: %v", err))
	}

	deletedCount := 0
	for _, path := range toDelete {
		if err := os.Remove(path); err != nil {
			common.SysError(fmt.Sprintf("media cleanup: delete file %s failed: %v", path, err))
			continue
		}
		deletedCount++
	}

	common.SysLog(fmt.Sprintf("media cleanup: deleted %d files, freed %d bytes", deletedCount, freedBytes))
	return &MediaCleanupResult{DeletedCount: deletedCount, FreedBytes: freedBytes}, nil
}
