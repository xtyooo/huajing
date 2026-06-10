package service

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	downloadBatchSize    = 50
	downloadPollInterval = 15 * time.Second
)

type downloadManager struct {
	wakeChan chan struct{}
	sem      chan struct{}
}

func (dm *downloadManager) WakeUp() {
	select {
	case dm.wakeChan <- struct{}{}:
	default:
	}
}

func (dm *downloadManager) Start() {
	common.SysLog("********** download manager started **********")
	model.ResetStuckMediaTasks()
	dm.pollingLoop()
}

func (dm *downloadManager) pollingLoop() {
	dm.processPending()
	ticker := time.NewTicker(downloadPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-dm.wakeChan:
		}
		dm.processPending()
	}
}

func (dm *downloadManager) processPending() {
	common.SysLog("********** download manager processing pending **********")
	defer common.SysLog("********** download manager processing completed **********")
	tasks := model.GetPendingMediaTasks(downloadBatchSize)
	for _, task := range tasks {
		task := task
		if !task.CompareAndSwapMediaStatus(model.MediaStatusPending, model.MediaStatusDownloading) {
			continue
		}
		dm.sem <- struct{}{}
		gopool.Go(func() {
			defer func() { <-dm.sem }()
			dm.downloadTaskResult(task)
		})
	}
}

func (dm *downloadManager) downloadTaskResult(task *model.Task) {
	common.SysLog("********** download task result starting...: " + task.TaskID)
	defer func() { common.SysLog("********** download task result done...: " + task.TaskID) }()

	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("download task %s panic: %v", task.TaskID, r))
			task.FailReason = fmt.Sprintf("panic: %v", r)
			if err := task.UpdateMediaStatus(model.MediaStatusFailed); err != nil {
				common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
			}
		}
	}()

	url := task.PrivateData.ResultURL
	if url == "" {
		task.FailReason = "result URL is empty"
		if err := task.UpdateMediaStatus(model.MediaStatusFailed); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	mediaDir := common.GetMediaDir()
	if mediaDir == "" {
		task.FailReason = "MEDIA_DIR not set"
		common.SysLog(fmt.Sprintf("download task %s failed: %s", task.TaskID, task.FailReason))
		if err := task.UpdateMediaStatus(model.MediaStatusFailed); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	common.SysLog("********** download task result starting...: " + task.TaskID + ", URL: " + url)
	resp, err := DoDownloadRequest(url, "download_task_result")
	if err != nil {
		common.SysLog(fmt.Sprintf("download task %s failed: %v", task.TaskID, err))
		task.FailReason = err.Error()
		if err := task.UpdateMediaStatus(model.MediaStatusFailed); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("read body for task %s failed: %v", task.TaskID, err))
		task.FailReason = err.Error()
		if err := task.UpdateMediaStatus(model.MediaStatusFailed); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	ext := getExtFromURL(url)
	if ext == "" {
		if exts, _ := mime.ExtensionsByType(resp.Header.Get("Content-Type")); len(exts) > 0 {
			ext = exts[0]
		}
	}
	timePrefix := time.Now().Format("20060102150405")
	fileName := timePrefix + "_" + task.TaskID + ext
	filePath := filepath.Join(mediaDir, fileName)
	if err := os.WriteFile(filePath, body, 0644); err != nil {
		common.SysLog(fmt.Sprintf("write file for task %s failed: %v", task.TaskID, err))
		task.FailReason = err.Error()
		if err := task.UpdateMediaStatus(model.MediaStatusFailed); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	baseURL := common.GetEnvOrDefaultString("MEDIA_BASE_URL", "https://huajingapi.top")
	task.MediaURL = baseURL + "/media/" + fileName
	if err := task.UpdateMediaStatus(model.MediaStatusSuccess); err != nil {
		common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		return
	}
	common.SysLog(fmt.Sprintf("downloaded task %s result to %s", task.TaskID, filePath))
}

func getExtFromURL(rawURL string) string {
	if idx := strings.Index(rawURL, "?"); idx != -1 {
		rawURL = rawURL[:idx]
	}
	ext := strings.ToLower(filepath.Ext(rawURL))
	if ext != "" {
		return ext
	}
	return ""
}

var DownloadManager = &downloadManager{
	wakeChan: make(chan struct{}, 1),
	sem:      make(chan struct{}, downloadBatchSize),
}
