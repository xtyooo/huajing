package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func proxyNativeVideo(c *gin.Context, task *model.Task) {
	taskID := task.TaskID
	if task.MediaStatus == model.MediaStatusPending || task.MediaStatus == model.MediaStatusDownloading {
		videoProxyError(c, http.StatusConflict, "media_cache_pending", "Media cache is still processing")
		return
	}
	if task.MediaStatus == model.MediaStatusFailed || task.MediaStatus == model.MediaStatusCleaned {
		videoProxyError(c, http.StatusGone, "media_unavailable", "Media is unavailable")
		return
	}
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(channel.Type)
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	var videoURL string
	proxy := channel.GetSetting().Proxy
	client := service.GetSSRFProtectedHTTPClient()
	if proxy != "" {
		// 渠道代理路径的连接由代理侧建立，无法做拨号时逐 IP 校验，
		// 因此后面对 videoURL 保留请求前的一次性 SSRF 校验。
		client, err = service.GetHttpClientWithProxy(proxy)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, c.Request.Method, "", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	switch channel.Type {
	case constant.ChannelTypeGemini:
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Missing stored API key for Gemini task %s", taskID))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "API key not stored for task")
			return
		}
		videoURL, err = getGeminiVideoURL(channel, task, apiKey)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Gemini video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Gemini video URL")
			return
		}
		result, resultErr := url.Parse(videoURL)
		base, baseErr := url.Parse(baseURL)
		if resultErr == nil && baseErr == nil && sameTaskMediaOrigin(result, base) {
			req.Header.Set("x-goog-api-key", apiKey)
		}
	case constant.ChannelTypeVertexAi:
		videoURL, err = getVertexVideoURL(channel, task)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Vertex video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Vertex video URL")
			return
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora, constant.ChannelTypeShafu, constant.ChannelTypeManying, constant.ChannelTypeWanchen, constant.ChannelTypeYaochen:
		// 安和、shafu、manying 与 OpenAI/Sora 一样通过上游 content 接口输出视频，必须携带任务提交时选中的渠道密钥。
		if channel.Type == constant.ChannelTypeWanchen || channel.Type == constant.ChannelTypeYaochen {
			baseURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
		}
		videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, url.PathEscape(task.GetUpstreamTaskID()))
		apiKey, keyErr := taskChannelKey(channel, task)
		if keyErr != nil {
			videoProxyError(c, http.StatusBadGateway, "server_error", "No available channel key")
			return
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case constant.ChannelTypeLingjing:
		// Lingjing serves the video via an authenticated download endpoint
		// (no public URL). Use the key selected when the task was submitted.
		videoURL = fmt.Sprintf("%s/api/open/v1/videos/%s/download", baseURL, task.GetUpstreamTaskID())
		apiKey := strings.TrimSpace(task.PrivateData.Key)
		if apiKey == "" {
			apiKey, _, err = channel.GetNextEnabledKey()
			if err != nil {
				videoProxyError(c, http.StatusBadGateway, "server_error", "No available channel key")
				return
			}
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	default:
		// Video URL is stored in PrivateData.ResultURL (fallback to FailReason for old data)
		videoURL = task.GetResultURL()
		headers, headerErr := service.MediaDownloadHeadersForResultURL(task, videoURL)
		if headerErr != nil {
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve media authentication")
			return
		}
		if len(headers) > 0 {
			for name, value := range headers[0] {
				req.Header.Set(name, value)
			}
		}
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if strings.HasPrefix(videoURL, "data:") {
		if err := writeVideoDataURL(c, videoURL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}

	var validateErr error
	if proxy == "" {
		validateErr = service.ValidateSSRFProtectedFetchURL(videoURL)
	} else {
		fetchSetting := system_setting.GetFetchSetting()
		validateErr = common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain)
	}
	if validateErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL blocked for task %s: %v", taskID, validateErr))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", validateErr))
		return
	}

	for _, header := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if value := c.GetHeader(header); value != "" {
			req.Header.Set(header, value)
		}
	}
	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse URL %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	client = service.SameOriginMediaClient(client)
	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if !isSuccessfulVideoProxyStatus(resp.StatusCode) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for %s", resp.StatusCode, videoURL))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	c.Writer.Header().Set("Cache-Control", "private, no-store")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
	}
}

func taskChannelKey(channel *model.Channel, task *model.Task) (string, error) {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key, nil
		}
	}
	if channel == nil {
		return "", fmt.Errorf("channel is nil")
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
