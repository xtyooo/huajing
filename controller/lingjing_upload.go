package controller

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// LingjingUpload passes a multipart reference-image upload through to the
// Lingjing upstream (/api/open/v1/uploads) and returns its {path,url} response
// as-is. The returned path is then used in reference_images when creating a
// video generation task.
func LingjingUpload(c *gin.Context) {
	contentType := c.Request.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Content-Type must be multipart/form-data"})
		return
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "failed to read request body"})
		return
	}
	channel, err := getUploadChannel(c, constant.ChannelTypeLingjing)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "no available Lingjing channel"})
		return
	}

	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "channel key unavailable"})
		return
	}

	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	upstreamURL := baseURL + "/api/open/v1/uploads"

	bodyReader, err := storage.NewReader()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "internal error"})
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstreamURL, bodyReader)
	if err != nil {
		_ = bodyReader.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to build upstream request"})
		return
	}
	// 为 HTTP/2 连接重置等透明重试提供独立请求体，避免复用共享游标。
	req.GetBody = storage.NewReader
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)
	req.ContentLength = storage.Size()

	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "proxy configuration error"})
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "upstream request failed"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to read upstream response"})
		return
	}

	for k, v := range resp.Header {
		if k != "Content-Length" && k != "Transfer-Encoding" {
			c.Header(k, v[0])
		}
	}
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

func getUploadChannel(c *gin.Context, channelType int) (*model.Channel, error) {
	specificChannelID := 0
	if rawID, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		if id, ok := rawID.(string); ok {
			specificChannelID, _ = strconv.Atoi(id)
		}
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	return model.GetUploadChannel(channelType, group, specificChannelID)
}
