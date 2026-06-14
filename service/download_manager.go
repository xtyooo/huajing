package service

import (
	"fmt"
	"io"
	"math/rand"
	"mime"
	"net/http"
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

	const maxRetries = 3
	var resp *http.Response
	var err error
	for i := 0; i < maxRetries; i++ {
		resp, err = DoDownloadRequest(url, "download_task_result")
		if err != nil {
			common.SysLog(fmt.Sprintf("download task %s failed: %v", task.TaskID, err))
			task.FailReason = err.Error()
			if err := task.UpdateMediaStatus(model.MediaStatusFailed); err != nil {
				common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
			}
			return
		}
		if resp.StatusCode == http.StatusNotFound && i < maxRetries-1 {
			resp.Body.Close()
			common.SysLog(fmt.Sprintf("download task %s got 404, retrying (%d/%d)...", task.TaskID, i+1, maxRetries))
			time.Sleep(time.Duration(10+rand.Intn(6)) * time.Second)
			continue
		}
		break
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		common.SysLog(fmt.Sprintf("download task %s failed: 404 after %d retries", task.TaskID, maxRetries))
		task.FailReason = "404 Not Found after max retries"
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

	contentType := resp.Header.Get("Content-Type")
	common.SysLog(fmt.Sprintf("URL: %s, Content-Type: %s, StausCode: %s", url, contentType, resp.Status))
	var ext string
	if strings.ToLower(contentType) == "video/mp4" {
		ext = ".mp4"
	} else {
		ext = getExtFromContentType(contentType)
		if ext == "" {
			ext = getExtFromURL(url)
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

func getExtFromContentType(contentType string) string {
	if contentType == "" {
		return ""
	}
	if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func getExtFromURL(rawURL string) string {
	if idx := strings.Index(rawURL, "?"); idx != -1 {
		rawURL = rawURL[:idx]
	}
	if idx := strings.Index(rawURL, "#"); idx != -1 {
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
