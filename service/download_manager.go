package service

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/tidwall/gjson"
)

const (
	downloadBatchSize         = 100
	downloadScanLimit         = downloadBatchSize * 5
	downloadPollInterval      = 15 * time.Second
	mediaCompensationInterval = time.Minute
	mediaDownloadRetries      = 5
	mediaStatusRetries        = 3
	mediaCompensationRetries  = 5
)

type downloadManager struct {
	wakeChan           chan struct{}
	sem                chan struct{}
	channelMu          sync.Mutex
	channelSem         map[int]chan struct{}
	lastCompensationAt time.Time
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
	if mediaURLs, err := model.ResetInvalidSoraMediaDownloads(); err != nil {
		common.SysLog(fmt.Sprintf("reset invalid Sora media downloads failed: %v", err))
	} else {
		removeInvalidCachedMediaFiles(common.GetMediaDir(), mediaURLs)
	}
	if err := cleanupStaleMediaDownloadTempFiles(common.GetMediaDir(), mediaDownloadTimeout()); err != nil {
		common.SysLog(fmt.Sprintf("cleanup stale media download files failed: %v", err))
	}
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
	staleAfter := mediaDownloadTimeout() + 2*downloadPollInterval
	if err := model.ResetStuckMediaTasksBefore(time.Now().Add(-staleAfter).Unix()); err != nil {
		common.SysLog(fmt.Sprintf("reset timed out media tasks failed: %v", err))
	}
	dm.scheduleFailedMediaCompensation()
	tasks := model.GetPendingMediaTasks(downloadScanLimit)
	for _, task := range tasks {
		task := task
		if !dm.tryAcquireGlobalSlot() {
			return
		}
		if !dm.tryAcquireChannelSlot(task.ChannelId) {
			dm.releaseGlobalSlot()
			continue
		}
		if !task.CompareAndSwapMediaStatus(model.MediaStatusPending, model.MediaStatusDownloading) {
			dm.releaseChannelSlot(task.ChannelId)
			dm.releaseGlobalSlot()
			continue
		}
		gopool.Go(func() {
			defer func() {
				dm.releaseChannelSlot(task.ChannelId)
				dm.releaseGlobalSlot()
			}()
			dm.downloadTaskResult(task)
		})
	}
}

func (dm *downloadManager) scheduleFailedMediaCompensation() {
	now := time.Now()
	if !dm.lastCompensationAt.IsZero() && now.Sub(dm.lastCompensationAt) < mediaCompensationInterval {
		return
	}
	dm.lastCompensationAt = now
	nowUnix := now.Unix()

	for _, task := range model.GetLegacyFailedMediaTasks(downloadScanLimit) {
		if task.EnableLegacyMediaCompensation(nowUnix) {
			common.SysLog(fmt.Sprintf("scheduled legacy media compensation for task %s", task.TaskID))
		}
	}

	for _, task := range model.GetRetryableFailedMediaTasks(nowUnix, mediaCompensationRetries, downloadScanLimit) {
		nextRetryAt := mediaCompensationNextRetryAt(task.MediaRetryCount+1, now)
		if task.QueueMediaCompensation(nowUnix, nextRetryAt, mediaCompensationRetries) {
			common.SysLog(fmt.Sprintf("queued media compensation for task %s (%d/%d)", task.TaskID, task.MediaRetryCount, mediaCompensationRetries))
		}
	}
}

func (dm *downloadManager) tryAcquireGlobalSlot() bool {
	select {
	case dm.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (dm *downloadManager) releaseGlobalSlot() {
	<-dm.sem
}

func (dm *downloadManager) tryAcquireChannelSlot(channelID int) bool {
	sem := dm.getChannelSemaphore(channelID)
	select {
	case sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (dm *downloadManager) releaseChannelSlot(channelID int) {
	<-dm.getChannelSemaphore(channelID)
}

func (dm *downloadManager) getChannelSemaphore(channelID int) chan struct{} {
	dm.channelMu.Lock()
	defer dm.channelMu.Unlock()
	if dm.channelSem == nil {
		dm.channelSem = make(map[int]chan struct{})
	}
	if sem, ok := dm.channelSem[channelID]; ok {
		return sem
	}
	sem := make(chan struct{}, mediaDownloadChannelConcurrency())
	dm.channelSem[channelID] = sem
	return sem
}

func (dm *downloadManager) downloadTaskResult(task *model.Task) {
	common.SysLog("********** download task result starting...: " + task.TaskID)
	defer func() { common.SysLog("********** download task result done...: " + task.TaskID) }()

	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("download task %s panic: %v", task.TaskID, r))
			if err := markMediaDownloadFailed(task, fmt.Errorf("panic: %v", r)); err != nil {
				common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
			}
		}
	}()

	target, targetErr := resolveMediaDownloadTarget(task)
	if targetErr != nil {
		if err := markMediaDownloadFailed(task, targetErr); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}
	if target.URL == "" {
		if err := markMediaDownloadFailed(task, fmt.Errorf("result URL is empty")); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	mediaDir := common.GetMediaDir()
	if mediaDir == "" {
		task.FailReason = "MEDIA_DIR not set"
		common.SysLog(fmt.Sprintf("download task %s failed: %s", task.TaskID, task.FailReason))
		if err := markMediaDownloadFailed(task, fmt.Errorf("MEDIA_DIR not set")); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	var resp *http.Response
	var err error
	attempts := mediaDownloadRetries
	if len(target.Headers) > attempts {
		attempts = len(target.Headers)
	}
	downloadCtx, cancelDownload := context.WithTimeout(context.Background(), mediaDownloadTimeout())
	defer cancelDownload()
	for i := 0; i < attempts; i++ {
		dlHeaders := target.Headers[i%len(target.Headers)]
		resp, err = DoDownloadRequestWithHeadersContext(downloadCtx, target.URL, dlHeaders, "download_task_result")
		if err == nil && resp == nil {
			err = fmt.Errorf("download failed: empty response")
		}
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if i < attempts-1 {
				common.SysLog(fmt.Sprintf("download task %s request failed, retrying (%d/%d): %v", task.TaskID, i+1, attempts, err))
				if !waitForMediaDownloadRetry(downloadCtx, i) {
					err = downloadCtx.Err()
					break
				}
				continue
			}
			break
		}
		retryAuth := (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && len(target.Headers) > 1
		if (isRetryableMediaDownloadStatus(resp.StatusCode) || retryAuth) && i < attempts-1 {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			common.SysLog(fmt.Sprintf("download task %s got status %d, retrying (%d/%d)...", task.TaskID, resp.StatusCode, i+1, attempts))
			if !waitForMediaDownloadRetry(downloadCtx, i) {
				err = downloadCtx.Err()
				break
			}
			continue
		}
		if isUnexpectedMediaContentType(resp.Header.Get("Content-Type")) && len(target.Headers) > 1 && i < attempts-1 {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			common.SysLog(fmt.Sprintf("download task %s got non-media content, trying another channel key (%d/%d)...", task.TaskID, i+1, attempts))
			continue
		}
		break
	}
	if err != nil {
		common.SysLog(fmt.Sprintf("download task %s failed after %d attempts: %v", task.TaskID, attempts, err))
		if err := markMediaDownloadFailed(task, err); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	if err := validateMediaDownloadResponse(resp); err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		common.SysLog(fmt.Sprintf("download task %s failed: %v", task.TaskID, err))
		if err := markMediaDownloadFailed(task, err); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	common.SysLog(fmt.Sprintf("URL: %s, Content-Type: %s, StatusCode: %s", target.URL, contentType, resp.Status))
	ext := resolveMediaFileExtension(task.Platform, target.URL, contentType)

	timePrefix := time.Now().Format("20060102150405")
	fileName := timePrefix + "_" + task.TaskID + ext
	filePath := filepath.Join(mediaDir, fileName)
	if err := streamMediaDownloadToFile(resp.Body, filePath); err != nil {
		common.SysLog(fmt.Sprintf("write file for task %s failed: %v", task.TaskID, err))
		if err := markMediaDownloadFailed(task, err); err != nil {
			common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		}
		return
	}

	baseURL := common.GetEnvOrDefaultString("MEDIA_BASE_URL", "https://huajingapi.top")
	task.MediaURL = baseURL + "/media/" + fileName
	task.FailReason = ""
	task.MediaNextRetryAt = 0
	if err := updateMediaStatusWithRetry(task, model.MediaStatusSuccess); err != nil {
		_ = os.Remove(filePath)
		common.SysLog(fmt.Sprintf("update task %s media status failed: %v", task.TaskID, err))
		return
	}
	common.SysLog(fmt.Sprintf("downloaded task %s result to %s", task.TaskID, filePath))
}

func markMediaDownloadFailed(task *model.Task, cause error) error {
	task.FailReason = cause.Error()
	if task.MediaRetryCount >= mediaCompensationRetries {
		task.MediaNextRetryAt = 0
		task.FailReason += "; media retry limit exhausted"
	} else {
		task.MediaNextRetryAt = mediaCompensationNextRetryAt(task.MediaRetryCount, time.Now())
	}
	return updateMediaStatusWithRetry(task, model.MediaStatusFailed)
}

func mediaCompensationNextRetryAt(retryCount int, now time.Time) int64 {
	delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount >= len(delays) {
		return 0
	}
	return now.Add(delays[retryCount]).Unix()
}

func mediaDownloadTimeout() time.Duration {
	seconds := constant.MediaDownloadTimeoutSeconds
	if seconds <= 0 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func mediaDownloadChannelConcurrency() int {
	concurrency := common.GetEnvOrDefault("MEDIA_DOWNLOAD_CHANNEL_CONCURRENCY", downloadBatchSize)
	if concurrency < 1 {
		return 1
	}
	if concurrency > downloadBatchSize {
		return downloadBatchSize
	}
	return concurrency
}

func waitForMediaDownloadRetry(ctx context.Context, retryIndex int) bool {
	timer := time.NewTimer(time.Duration(retryIndex+1) * 2 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func isRetryableMediaDownloadStatus(status int) bool {
	return status == http.StatusForbidden || status == http.StatusNotFound || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func streamMediaDownloadToFile(reader io.Reader, filePath string) error {
	tempFile, err := os.CreateTemp(filepath.Dir(filePath), ".media-download-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	maxFileSize := int64(mediaDownloadLimitMB()) * 1024 * 1024
	written, err := io.Copy(tempFile, io.LimitReader(reader, maxFileSize+1))
	if err != nil {
		_ = tempFile.Close()
		return err
	}
	if written > maxFileSize {
		_ = tempFile.Close()
		return fmt.Errorf("file size exceeds maximum allowed size: %dMB", mediaDownloadLimitMB())
	}
	if written == 0 {
		_ = tempFile.Close()
		return fmt.Errorf("download failed: empty response body")
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, filePath)
}

func validateMediaDownloadResponse(resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("download failed: empty response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download failed with status %s", resp.Status)
	}
	if resp.StatusCode == http.StatusNoContent || resp.ContentLength == 0 {
		return fmt.Errorf("download failed: empty response body")
	}
	if resp.Body == nil {
		return fmt.Errorf("download failed: empty response body")
	}
	if isUnexpectedMediaContentType(resp.Header.Get("Content-Type")) {
		contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		return fmt.Errorf("download failed: unexpected content type %s", strings.ToLower(contentType))
	}

	maxFileSize := int64(mediaDownloadLimitMB()) * 1024 * 1024
	if resp.ContentLength > maxFileSize {
		return fmt.Errorf("file size exceeds maximum allowed size: %dMB", mediaDownloadLimitMB())
	}
	return nil
}

func isUnexpectedMediaContentType(value string) bool {
	contentType, _, _ := mime.ParseMediaType(value)
	contentType = strings.ToLower(contentType)
	return contentType == "application/json" || strings.HasSuffix(contentType, "+json") || contentType == "text/html" || contentType == "text/plain"
}

type mediaDownloadTarget struct {
	URL     string
	Headers []map[string]string
}

func resolveMediaDownloadTarget(task *model.Task) (mediaDownloadTarget, error) {
	if task == nil {
		return mediaDownloadTarget{}, fmt.Errorf("task is nil")
	}
	if resultURL := strings.TrimSpace(task.PrivateData.ResultURL); resultURL != "" && !isTaskProxyResultURL(resultURL, task.TaskID) {
		headers, err := mediaDownloadHeadersForResultURL(task, resultURL)
		if err != nil {
			return mediaDownloadTarget{}, err
		}
		return mediaDownloadTarget{URL: resultURL, Headers: headers}, nil
	}
	if resultURL := mediaResultURLFromTaskData(task.Data); resultURL != "" && !isTaskProxyResultURL(resultURL, task.TaskID) {
		headers, err := mediaDownloadHeadersForResultURL(task, resultURL)
		if err != nil {
			return mediaDownloadTarget{}, err
		}
		return mediaDownloadTarget{URL: resultURL, Headers: headers}, nil
	}
	if usesOpenAIVideoContentEndpoint(task.Platform) {
		channel, err := model.CacheGetChannel(task.ChannelId)
		if err != nil {
			return mediaDownloadTarget{}, err
		}
		baseURL := channel.GetBaseURL()
		if baseURL == "" {
			baseURL = constant.ChannelBaseURLs[channel.Type]
		}
		if baseURL == "" {
			return mediaDownloadTarget{}, fmt.Errorf("channel base URL is empty")
		}
		return mediaDownloadTarget{
			URL:     strings.TrimRight(baseURL, "/") + "/v1/videos/" + url.PathEscape(task.GetUpstreamTaskID()) + "/content",
			Headers: mediaDownloadAuthHeaders(task, channel),
		}, nil
	}
	resultURL := strings.TrimSpace(task.PrivateData.ResultURL)
	headers, err := mediaDownloadHeadersForResultURL(task, resultURL)
	if err != nil {
		return mediaDownloadTarget{}, err
	}
	return mediaDownloadTarget{URL: resultURL, Headers: headers}, nil
}

func mediaResultURLFromTaskData(data []byte) string {
	for _, key := range []string{"metadata.url", "video_url", "result_url", "url", "image_url"} {
		value := strings.TrimSpace(gjson.GetBytes(data, key).String())
		if value != "" {
			return value
		}
	}
	return ""
}

func isTaskProxyResultURL(rawURL string, taskID string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Path == "/v1/videos/"+taskID+"/content"
}

func mediaDownloadAuthHeaders(task *model.Task, channel *model.Channel) []map[string]string {
	keys := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if task != nil {
		add(task.PrivateData.Key)
	}
	if channel != nil {
		for i, key := range channel.GetKeys() {
			if status, ok := channel.ChannelInfo.MultiKeyStatusList[i]; ok && status != common.ChannelStatusEnabled {
				continue
			}
			add(key)
		}
	}
	headers := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		headers = append(headers, map[string]string{"Authorization": "Bearer " + key})
	}
	if len(headers) == 0 {
		return []map[string]string{nil}
	}
	return headers
}

// usesOpenAIVideoContentEndpoint 判断任务是否需要通过上游鉴权的 content 接口获取视频。
// 安和的任务查询结果可能只有嵌套产物信息，此时必须使用已选渠道密钥访问上游 content 接口。
func usesOpenAIVideoContentEndpoint(platform constant.TaskPlatform) bool {
	return platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)) ||
		platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAI)) ||
		platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAnhe)) ||
		platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeShafu))
}

func removeInvalidCachedMediaFiles(mediaDir string, mediaURLs []string) {
	for _, mediaURL := range mediaURLs {
		parsed, err := url.Parse(mediaURL)
		if err != nil {
			continue
		}
		name := filepath.Base(parsed.Path)
		if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		_ = os.Remove(filepath.Join(mediaDir, name))
	}
}

func updateMediaStatusWithRetry(task *model.Task, status int) error {
	var err error
	for attempt := 0; attempt < mediaStatusRetries; attempt++ {
		if task.Platform == constant.TaskPlatformImage && (status == model.MediaStatusSuccess || status == model.MediaStatusFailed) {
			now := time.Now().Unix()
			task.UpdatedAt = now
			task.MediaStatus = status
			task.MediaFinishTime = now
			task.FinishTime = now
			task.Progress = "100%"
			if status == model.MediaStatusSuccess {
				task.Status = model.TaskStatusSuccess
			} else {
				task.Status = model.TaskStatusFailure
			}
			err = task.Update()
		} else {
			err = task.UpdateMediaStatus(status)
		}
		if err == nil {
			return nil
		}
		if attempt < mediaStatusRetries-1 {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}
	return err
}

func cleanupStaleMediaDownloadTempFiles(mediaDir string, olderThan time.Duration) error {
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".media-download-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(mediaDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func mediaDownloadLimitMB() int {
	if constant.MaxMediaDownloadMB > 0 {
		return constant.MaxMediaDownloadMB
	}
	return 512
}

func getExtFromContentType(contentType string) string {
	if contentType == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func resolveMediaFileExtension(platform constant.TaskPlatform, rawURL string, contentType string) string {
	if urlExt := getKnownExtFromURL(rawURL); urlExt != "" {
		// When the URL already carries a known media extension (e.g. .png, .mp4),
		// trust it — it is the most reliable signal for image/video results.
		return urlExt
	}
	contentTypeExt := getExtFromContentType(contentType)
	if usesOpenAIVideoContentEndpoint(platform) {
		if knownMediaExts[strings.ToLower(contentTypeExt)] {
			return contentTypeExt
		}
		return ".mp4"
	}
	if platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeHJ)) || platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeMimo)) {
		return ".mp4"
	}
	if contentTypeExt != "" {
		return contentTypeExt
	}
	return getExtFromURL(rawURL)
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

// knownMediaExts is a whitelist of media extensions that, when present in the
// result URL, should be used directly instead of relying on Content-Type.
var knownMediaExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
	".gif":  true,
	".bmp":  true,
	".mp4":  true,
	".mov":  true,
	".webm": true,
	".m4v":  true,
}

// getKnownExtFromURL returns the URL's extension only when it is a recognized
// media type; otherwise it returns "" so the caller falls back to Content-Type
// or platform-specific defaults. This avoids mistaking arbitrary path segments
// (e.g. "/v1") for a file extension.
func getKnownExtFromURL(rawURL string) string {
	ext := getExtFromURL(rawURL)
	if knownMediaExts[ext] {
		return ext
	}
	return ""
}

// isAuthDownloadPlatform returns true for platforms whose result download
// endpoint requires Bearer authentication (e.g. Lingjing). For these platforms
// DownloadManager attaches the channel key as an Authorization header when
// fetching the result file.
func isAuthDownloadPlatform(platform constant.TaskPlatform) bool {
	return platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeLingjing))
}

// mediaDownloadHeadersForResultURL 仅向需要鉴权且与渠道同源的结果地址附加渠道密钥，避免泄露给第三方 CDN。
func mediaDownloadHeadersForResultURL(task *model.Task, resultURL string) ([]map[string]string, error) {
	requiresSameOriginAuth := task.Platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeDiaomao)) ||
		task.Platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeManju)) ||
		usesOpenAIVideoContentEndpoint(task.Platform)
	if !isAuthDownloadPlatform(task.Platform) && !requiresSameOriginAuth {
		return []map[string]string{nil}, nil
	}
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		if requiresSameOriginAuth {
			// 历史任务可能已删除渠道，但第三方 CDN 结果仍可公开下载，不能因此阻断缓存。
			return []map[string]string{nil}, nil
		}
		return nil, err
	}
	if requiresSameOriginAuth {
		if !isResultURLFromChannelOrigin(resultURL, channel) {
			return []map[string]string{nil}, nil
		}
	}
	return mediaDownloadAuthHeaders(task, channel), nil
}

// isResultURLFromChannelOrigin 判断结果地址是否仍由当前渠道提供，只有同源地址才能安全携带渠道密钥。
func isResultURLFromChannelOrigin(resultURL string, channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[channel.Type]
	}
	result, resultErr := url.Parse(strings.TrimSpace(resultURL))
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	return resultErr == nil && baseErr == nil &&
		strings.EqualFold(result.Scheme, base.Scheme) && strings.EqualFold(result.Host, base.Host)
}

func taskDownloadKey(task *model.Task) (string, error) {
	if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
		return key, nil
	}
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		return "", err
	}
	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		return "", keyErr
	}
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("channel key is empty")
	}
	return key, nil
}

var DownloadManager = &downloadManager{
	wakeChan:   make(chan struct{}, 1),
	sem:        make(chan struct{}, downloadBatchSize),
	channelSem: make(map[int]chan struct{}),
}
