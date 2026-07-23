package service

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const imageCacheSessionContextKey = "image_cache_session"

type ImageCacheParams struct {
	UserID            int
	ChannelID         int
	Quota             int
	Group             string
	Action            string
	Prompt            string
	ModelName         string
	UpstreamModelName string
	CreatedAt         int64
}

type ImageCacheSession struct {
	params   ImageCacheParams
	tasks    []*model.Task
	finished bool
}

type cachedImageTask struct {
	task     *model.Task
	filePath string
}

var fetchImageResult = func(ctx context.Context, rawURL string) (*http.Response, error) {
	return DoDownloadRequestWithHeadersContext(ctx, rawURL, nil, "cache_image_result")
}

func BeginImageCacheSession(params ImageCacheParams) (*ImageCacheSession, error) {
	task, err := newImageCacheTask(params, 0, 1)
	if err != nil {
		return nil, fmt.Errorf("create image task failed: %w", err)
	}
	return &ImageCacheSession{params: params, tasks: []*model.Task{task}}, nil
}

func AttachImageCacheSession(c *gin.Context, session *ImageCacheSession) {
	if c != nil && session != nil {
		c.Set(imageCacheSessionContextKey, session)
	}
}

func ImageCacheSessionFromContext(c *gin.Context) *ImageCacheSession {
	if c == nil {
		return nil
	}
	value, exists := c.Get(imageCacheSessionContextKey)
	if !exists {
		return nil
	}
	session, _ := value.(*ImageCacheSession)
	return session
}

func NewImageCacheSession(params ImageCacheParams) *ImageCacheSession {
	return &ImageCacheSession{params: params}
}

func (s *ImageCacheSession) Begin() error {
	if s == nil || s.finished {
		return fmt.Errorf("image cache session is unavailable")
	}
	if len(s.tasks) > 0 {
		return nil
	}
	task, err := newImageCacheTask(s.params, 0, 1)
	if err != nil {
		return fmt.Errorf("create image task failed: %w", err)
	}
	s.tasks = []*model.Task{task}
	return nil
}

func (s *ImageCacheSession) SetAttempt(channelID int, upstreamModelName string) error {
	if s == nil || s.finished {
		return fmt.Errorf("image cache session is unavailable")
	}
	s.params.ChannelID = channelID
	s.params.UpstreamModelName = upstreamModelName
	now := time.Now().Unix()
	for _, task := range s.tasks {
		if task == nil {
			continue
		}
		task.ChannelId = channelID
		task.UpdatedAt = now
		task.Properties.UpstreamModelName = upstreamModelName
		if err := task.Update(); err != nil {
			return fmt.Errorf("update image task attempt failed: %w", err)
		}
	}
	return nil
}

func (s *ImageCacheSession) CacheResponse(responseBody []byte) ([]byte, error) {
	if s == nil || s.finished {
		return nil, fmt.Errorf("image cache session is unavailable")
	}
	if err := s.Begin(); err != nil {
		return nil, err
	}
	rewritten, tasks, err := cacheImageResponse(responseBody, s.params, s.tasks)
	s.tasks = tasks
	s.finished = true
	return rewritten, err
}

func (s *ImageCacheSession) Fail(cause error) {
	if s == nil || s.finished {
		return
	}
	if cause == nil {
		cause = fmt.Errorf("image request failed")
	}
	failImageCacheTasks(s.tasks, cause)
	s.finished = true
}

func (s *ImageCacheSession) Finished() bool {
	return s == nil || s.finished
}

func CacheImageResponse(responseBody []byte, params ImageCacheParams) ([]byte, error) {
	session, err := BeginImageCacheSession(params)
	if err != nil {
		return nil, err
	}
	return session.CacheResponse(responseBody)
}

func cacheImageResponse(responseBody []byte, params ImageCacheParams, tasks []*model.Task) ([]byte, []*model.Task, error) {
	fail := func(err error) ([]byte, []*model.Task, error) {
		failImageCacheTasks(tasks, err)
		return nil, tasks, err
	}
	if !gjson.ValidBytes(responseBody) {
		return fail(fmt.Errorf("decode image response failed"))
	}
	dataResult := gjson.GetBytes(responseBody, "data")
	if !dataResult.IsArray() {
		return fail(fmt.Errorf("image response data is not an array"))
	}
	items := dataResult.Array()
	if len(items) == 0 {
		return fail(fmt.Errorf("image response contains no results"))
	}

	rewritten := append([]byte(nil), responseBody...)
	completed := make([]cachedImageTask, 0, len(items))
	rollback := func(cause error) ([]byte, []*model.Task, error) {
		rollbackCachedImageTasks(completed, cause)
		failImageCacheTasks(tasks, cause)
		return nil, tasks, cause
	}

	for index, item := range items {
		if !item.IsObject() {
			return rollback(fmt.Errorf("image result %d is not an object", index+1))
		}
		if index >= len(tasks) {
			task, err := newImageCacheTask(params, index, len(items))
			if err != nil {
				return rollback(fmt.Errorf("create image task failed: %w", err))
			}
			tasks = append(tasks, task)
		}

		rawURL := imageResultString(item, "url")
		if rawURL == "" {
			rawURL = imageResultString(item, "image_url")
		}
		b64JSON := imageResultString(item, "b64_json")
		if b64JSON == "" {
			b64JSON = imageResultString(item, "b64_image")
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "data:") {
			if b64JSON == "" {
				b64JSON = rawURL
			}
			rawURL = ""
		}

		task := tasks[index]
		prepareImageCacheTask(task, params, rawURL, index, len(items))
		if err := task.Update(); err != nil {
			return rollback(fmt.Errorf("prepare image task failed: %w", err))
		}

		mediaURL, filePath, err := cacheImageResult(task.TaskID, rawURL, b64JSON)
		if err != nil {
			return rollback(fmt.Errorf("cache image result failed: %w", err))
		}
		rewritten, err = rewriteImageResponseItem(rewritten, index, rawURL, mediaURL)
		if err != nil {
			_ = os.Remove(filePath)
			return rollback(fmt.Errorf("rewrite image response failed: %w", err))
		}

		now := time.Now().Unix()
		task.MediaURL = mediaURL
		task.MediaStatus = model.MediaStatusSuccess
		task.MediaFinishTime = now
		task.FinishTime = now
		task.UpdatedAt = now
		task.Status = model.TaskStatusSuccess
		task.Progress = "100%"
		task.SetData(cachedImageTaskData(rewritten, index))
		if err := task.Update(); err != nil {
			_ = os.Remove(filePath)
			return rollback(fmt.Errorf("save cached image task failed: %w", err))
		}
		completed = append(completed, cachedImageTask{task: task, filePath: filePath})
	}

	return rewritten, tasks, nil
}

func newImageCacheTask(params ImageCacheParams, index, count int) (*model.Task, error) {
	now := time.Now().Unix()
	createdAt := params.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	task := &model.Task{
		CreatedAt:      createdAt,
		UpdatedAt:      now,
		TaskID:         model.GenerateTaskID(),
		Platform:       constant.TaskPlatformImage,
		UserId:         params.UserID,
		Group:          params.Group,
		ChannelId:      params.ChannelID,
		Quota:          imageTaskQuota(params.Quota, count, index),
		Action:         params.Action,
		Status:         model.TaskStatusInProgress,
		SubmitTime:     createdAt,
		StartTime:      createdAt,
		Progress:       "0%",
		Properties:     imageTaskProperties(params, index, count),
		MediaStatus:    model.MediaStatusDownloading,
		MediaStartTime: now,
	}
	if err := task.Insert(); err != nil {
		return nil, err
	}
	return task, nil
}

func prepareImageCacheTask(task *model.Task, params ImageCacheParams, rawURL string, index, count int) {
	now := time.Now().Unix()
	task.UpdatedAt = now
	task.UserId = params.UserID
	task.Group = params.Group
	task.ChannelId = params.ChannelID
	task.Quota = imageTaskQuota(params.Quota, count, index)
	task.Action = params.Action
	task.Status = model.TaskStatusInProgress
	task.Progress = "0%"
	task.FailReason = ""
	task.FinishTime = 0
	task.Properties = imageTaskProperties(params, index, count)
	task.PrivateData.ResultURL = strings.TrimSpace(rawURL)
	task.Data = nil
	task.MediaURL = ""
	task.MediaStatus = model.MediaStatusDownloading
	task.MediaStartTime = now
	task.MediaFinishTime = 0
}

func imageTaskProperties(params ImageCacheParams, index, count int) model.Properties {
	return model.Properties{
		Input:             params.Prompt,
		OriginModelName:   params.ModelName,
		UpstreamModelName: params.UpstreamModelName,
		Extra: map[string]interface{}{
			"image_index": index + 1,
			"image_count": count,
		},
	}
}

func imageResultString(item gjson.Result, field string) string {
	value := item.Get(field)
	if value.Type != gjson.String {
		return ""
	}
	return strings.TrimSpace(value.String())
}

func rewriteImageResponseItem(responseBody []byte, index int, upstreamURL, mediaURL string) ([]byte, error) {
	itemPath := fmt.Sprintf("data.%d", index)
	for _, field := range []string{"b64_json", "b64_image"} {
		path := itemPath + "." + field
		if gjson.GetBytes(responseBody, path).Exists() {
			var err error
			responseBody, err = sjson.DeleteBytes(responseBody, path)
			if err != nil {
				return nil, err
			}
		}
	}
	for _, field := range []string{"url", "image_url"} {
		path := itemPath + "." + field
		if field != "url" && !gjson.GetBytes(responseBody, path).Exists() {
			continue
		}
		var err error
		responseBody, err = sjson.SetBytes(responseBody, path, mediaURL)
		if err != nil {
			return nil, err
		}
	}
	if upstreamURL == "" {
		return responseBody, nil
	}
	return replaceImageMetadataURL(responseBody, upstreamURL, mediaURL)
}

func replaceImageMetadataURL(responseBody []byte, upstreamURL, mediaURL string) ([]byte, error) {
	metadata := gjson.GetBytes(responseBody, "metadata")
	if !metadata.Exists() || metadata.Raw == "" {
		return responseBody, nil
	}
	var value interface{}
	if err := common.Unmarshal([]byte(metadata.Raw), &value); err != nil {
		return nil, err
	}
	if !replaceImageURLValue(value, upstreamURL, mediaURL) {
		return responseBody, nil
	}
	rewrittenMetadata, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	return sjson.SetRawBytes(responseBody, "metadata", rewrittenMetadata)
}

func replaceImageURLValue(value interface{}, upstreamURL, mediaURL string) bool {
	replaced := false
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			if text, ok := child.(string); ok && text == upstreamURL {
				typed[key] = mediaURL
				replaced = true
				continue
			}
			if replaceImageURLValue(child, upstreamURL, mediaURL) {
				replaced = true
			}
		}
	case []interface{}:
		for index, child := range typed {
			if text, ok := child.(string); ok && text == upstreamURL {
				typed[index] = mediaURL
				replaced = true
				continue
			}
			if replaceImageURLValue(child, upstreamURL, mediaURL) {
				replaced = true
			}
		}
	}
	return replaced
}

func cachedImageTaskData(responseBody []byte, index int) map[string]interface{} {
	data := map[string]interface{}{}
	if created := gjson.GetBytes(responseBody, "created"); created.Exists() {
		var value interface{}
		if common.Unmarshal([]byte(created.Raw), &value) == nil {
			data["created"] = value
		}
	}
	item := gjson.GetBytes(responseBody, fmt.Sprintf("data.%d", index))
	var itemValue interface{}
	if common.Unmarshal([]byte(item.Raw), &itemValue) == nil {
		data["data"] = []interface{}{itemValue}
	}
	return data
}

func rollbackCachedImageTasks(tasks []cachedImageTask, cause error) {
	for _, cached := range tasks {
		_ = os.Remove(cached.filePath)
		cached.task.MediaURL = ""
		cached.task.Data = nil
		markImageCacheTaskFailed(cached.task, cause)
	}
}

func failImageCacheTasks(tasks []*model.Task, cause error) {
	for _, task := range tasks {
		if task == nil {
			continue
		}
		task.MediaURL = ""
		task.Data = nil
		markImageCacheTaskFailed(task, cause)
	}
}

func imageTaskQuota(total, count, index int) int {
	if total <= 0 || count <= 0 {
		return 0
	}
	quota := total / count
	if index < total%count {
		quota++
	}
	return quota
}

func markImageCacheTaskFailed(task *model.Task, cause error) {
	now := time.Now().Unix()
	task.UpdatedAt = now
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FinishTime = now
	task.MediaStatus = model.MediaStatusFailed
	task.MediaFinishTime = now
	task.FailReason = cause.Error()
	if err := task.Update(); err != nil {
		common.SysError(fmt.Sprintf("update image task %s failure state failed: %v", task.TaskID, err))
	}
}

func cacheImageResult(taskID, rawURL, b64JSON string) (string, string, error) {
	mediaDir := common.GetMediaDir()
	info, err := os.Stat(mediaDir)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("media directory is unavailable")
	}

	var reader io.Reader
	var closeBody func()
	if rawURL = strings.TrimSpace(rawURL); rawURL != "" {
		downloadCtx, cancel := context.WithTimeout(context.Background(), mediaDownloadTimeout())
		defer cancel()
		resp, err := fetchImageResult(downloadCtx, rawURL)
		if err != nil {
			return "", "", fmt.Errorf("download image failed")
		}
		if err := validateMediaDownloadResponse(resp); err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return "", "", fmt.Errorf("invalid image response: %w", err)
		}
		reader = resp.Body
		closeBody = func() { _ = resp.Body.Close() }
	} else if b64JSON = strings.TrimSpace(b64JSON); b64JSON != "" {
		encoded, err := imageBase64Payload(b64JSON)
		if err != nil {
			return "", "", err
		}
		maxBytes := int64(mediaDownloadLimitMB()) * 1024 * 1024
		if int64(base64.StdEncoding.DecodedLen(len(encoded))) > maxBytes {
			return "", "", fmt.Errorf("image exceeds maximum allowed size")
		}
		reader = base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	} else {
		return "", "", fmt.Errorf("image result contains neither url nor base64 data")
	}
	if closeBody != nil {
		defer closeBody()
	}

	buffered := bufio.NewReader(reader)
	prefix, err := buffered.Peek(512)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return "", "", fmt.Errorf("read image header failed")
	}
	if len(prefix) == 0 {
		return "", "", fmt.Errorf("image result is empty")
	}
	ext, ok := imageExtension(http.DetectContentType(prefix))
	if !ok {
		return "", "", fmt.Errorf("response is not a supported image")
	}

	fileName := time.Now().Format("20060102150405") + "_" + taskID + ext
	filePath := filepath.Join(mediaDir, fileName)
	if err := streamMediaDownloadToFile(buffered, filePath); err != nil {
		return "", "", err
	}
	baseURL := strings.TrimRight(common.GetEnvOrDefaultString("MEDIA_BASE_URL", "https://huajingapi.top"), "/")
	return baseURL + "/media/" + fileName, filePath, nil
}

func imageBase64Payload(value string) (string, error) {
	if !strings.HasPrefix(value, "data:") {
		return value, nil
	}
	comma := strings.IndexByte(value, ',')
	if comma < 0 || !strings.Contains(strings.ToLower(value[:comma]), ";base64") {
		return "", fmt.Errorf("invalid base64 image data")
	}
	return value[comma+1:], nil
}

func imageExtension(contentType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/bmp":
		return ".bmp", true
	default:
		return "", false
	}
}
