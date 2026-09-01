package controller

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsSuccessfulVideoProxyStatus 验证视频代理会透传所有有效的 2xx 媒体响应，避免把 206 MP4 误报为上游失败。
func TestIsSuccessfulVideoProxyStatus(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{name: "OK", statusCode: http.StatusOK, want: true},
		{name: "Partial Content", statusCode: http.StatusPartialContent, want: true},
		{name: "Other 2xx", statusCode: http.StatusIMUsed, want: true},
		{name: "Redirect", statusCode: http.StatusMultipleChoices, want: false},
		{name: "Unauthorized", statusCode: http.StatusUnauthorized, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, isSuccessfulVideoProxyStatus(testCase.statusCode))
		})
	}
}
