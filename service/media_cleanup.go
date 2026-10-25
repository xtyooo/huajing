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
	cleanMediaByTaskIDs = model.CleanMediaByTaskIDs
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

			_, _ = RunMediaCleanup()
			timer := time.NewTimer(mediaCleanupInterval())
			defer timer.Stop()
			for {
				select {
				case <-timer.C:
					_, _ = RunMediaCleanup()
					timer.Reset(mediaCleanupInterval())
				case <-common.MediaCleanupConfigChanged():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(mediaCleanupInterval())
				}
			}
		})
	})
}

func mediaCleanupInterval() time.Duration {
	minutes := common.GetMediaCleanupConfig().CleanupInterval
	if minutes < 1 {
		minutes = 180
	}
	return time.Duration(minutes) * time.Minute
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

	type mediaCleanupCandidate struct {
		path   string
		taskID string
		size   int64
	}
	var candidates []mediaCleanupCandidate

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
			candidates = append(candidates, mediaCleanupCandidate{
				path:   filepath.Join(mediaDir, name),
				taskID: taskID,
				size:   info.Size(),
			})
		}
	}

	if len(candidates) == 0 {
		common.SysLog("media cleanup: no files to clean")
		return &MediaCleanupResult{DeletedCount: 0, FreedBytes: 0}, nil
	}

	stagingDir, err := os.MkdirTemp(mediaDir, ".cleanup-")
	if err != nil {
		return nil, fmt.Errorf("create media cleanup staging dir failed: %v", err)
	}
	defer os.Remove(stagingDir)

	type stagedMediaFile struct {
		mediaCleanupCandidate
		stagedPath string
	}
	var stagedFiles []stagedMediaFile
	rollback := func() error {
		var rollbackErr error
		for i := len(stagedFiles) - 1; i >= 0; i-- {
			file := stagedFiles[i]
			if err := os.Rename(file.stagedPath, file.path); err != nil {
				rollbackErr = fmt.Errorf("restore media file %s failed: %v", file.path, err)
				common.SysError(rollbackErr.Error())
			}
		}
		return rollbackErr
	}

	for _, candidate := range candidates {
		stagedPath := filepath.Join(stagingDir, filepath.Base(candidate.path))
		if err := os.Rename(candidate.path, stagedPath); err != nil {
			rollbackErr := rollback()
			if rollbackErr != nil {
				return nil, fmt.Errorf("stage media file %s failed: %v; %v", candidate.path, err, rollbackErr)
			}
			return nil, fmt.Errorf("stage media file %s failed: %v", candidate.path, err)
		}
		stagedFiles = append(stagedFiles, stagedMediaFile{
			mediaCleanupCandidate: candidate,
			stagedPath:            stagedPath,
		})
	}

	taskIDs := make([]string, 0, len(stagedFiles))
	for _, file := range stagedFiles {
		taskIDs = append(taskIDs, file.taskID)
	}
	if err := cleanMediaByTaskIDs(taskIDs); err != nil {
		rollbackErr := rollback()
		if rollbackErr != nil {
			return nil, fmt.Errorf("update media cleanup database failed: %v; %v", err, rollbackErr)
		}
		return nil, fmt.Errorf("update media cleanup database failed: %v", err)
	}

	deletedCount := 0
	var freedBytes int64
	for _, file := range stagedFiles {
		if err := os.Remove(file.stagedPath); err != nil {
			common.SysError(fmt.Sprintf("media cleanup: delete staged file %s failed: %v", file.stagedPath, err))
			continue
		}
		deletedCount++
		freedBytes += file.size
	}

	common.SysLog(fmt.Sprintf("media cleanup: deleted %d files, freed %d bytes", deletedCount, freedBytes))
	return &MediaCleanupResult{DeletedCount: deletedCount, FreedBytes: freedBytes}, nil
}
