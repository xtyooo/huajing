package controller

import (
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
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
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "internal error"})
		return
	}

	channel, err := model.GetLingjingChannel()
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

	req, err := http.NewRequest("POST", upstreamURL, common.ReaderOnly(storage))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to build upstream request"})
		return
	}
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
