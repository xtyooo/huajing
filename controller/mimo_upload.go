package controller

import (
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// MimoUpload 将 multipart 素材上传请求安全转发到选中的 Mimo 渠道。
func MimoUpload(c *gin.Context) {
	contentType := c.Request.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Content-Type must be multipart/form-data", "data": nil})
		return
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "failed to read request body", "data": nil})
		return
	}
	channel, err := getUploadChannel(c, constant.ChannelTypeMimo)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "no available MIMO channel", "data": nil})
		return
	}

	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "channel key unavailable", "data": nil})
		return
	}

	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	upstreamURL := baseURL + "/api/video/upload"

	bodyReader, err := storage.NewReader()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "internal error", "data": nil})
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstreamURL, bodyReader)
	if err != nil {
		_ = bodyReader.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "failed to build upstream request", "data": nil})
		return
	}
	// 为 HTTP/2 连接重置等透明重试提供独立请求体，避免复用共享游标。
	req.GetBody = storage.NewReader
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)
	req.ContentLength = storage.Size()

	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "proxy configuration error", "data": nil})
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "upstream request failed", "data": nil})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "failed to read upstream response", "data": nil})
		return
	}

	for k, v := range resp.Header {
		if k != "Content-Length" && k != "Transfer-Encoding" {
			c.Header(k, v[0])
		}
	}
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}
