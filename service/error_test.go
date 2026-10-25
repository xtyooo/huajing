package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

// TestTaskErrorWrapperMasksUpstreamRequiredQuota 验证任务接口包装上游 403 时不会向下游透传实际所需额度。
func TestTaskErrorWrapperMasksUpstreamRequiredQuota(t *testing.T) {
	upstreamBody := `{"code":"insufficient_user_quota","message":"预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ¥3.520000","data":null}`
	expectedBody := `{"code":"insufficient_user_quota","message":"预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ****","data":null}`

	taskErr := TaskErrorWrapper(fmt.Errorf("%s", upstreamBody), "fail_to_fetch_task", http.StatusForbidden)

	require.NotNil(t, taskErr)
	require.Equal(t, "fail_to_fetch_task", taskErr.Code)
	require.Equal(t, http.StatusForbidden, taskErr.StatusCode)
	require.Equal(t, expectedBody, taskErr.Message)
	require.Equal(t, upstreamBody, taskErr.Error.Error(), "内部错误应保留原文供服务端排查")

	responseBody, err := common.Marshal(taskErr)
	require.NoError(t, err)
	require.Contains(t, string(responseBody), "需要预扣费额度: ****")
	require.NotContains(t, string(responseBody), "¥3.520000")
}

// TestTaskErrorFromAPIErrorMasksRequiredQuota 验证本地 API 错误转换为任务响应时仍会执行输出边界脱敏。
func TestTaskErrorFromAPIErrorMasksRequiredQuota(t *testing.T) {
	rawMessage := "预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ¥3.520000"
	apiErr := types.NewErrorWithStatusCode(
		fmt.Errorf("%s", rawMessage),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
	)

	taskErr := TaskErrorFromAPIError(apiErr)

	require.NotNil(t, taskErr)
	require.Equal(t, "预扣费额度失败, 用户剩余额度: ¥0.918000, 需要预扣费额度: ****", taskErr.Message)
	require.Equal(t, rawMessage, taskErr.Error.Error(), "内部错误应保留原文供服务端排查")
}

// TestRelayErrorHandlerMasksUpstreamRequiredQuota 验证普通 OpenAI 与 Claude 输出都会隐藏上游所需额度。
func TestRelayErrorHandlerMasksUpstreamRequiredQuota(t *testing.T) {
	rawMessage := "预扣费额度失败, 用户剩余额度: ¥0.015200, 需要预扣费额度: ¥0.060000 (request id: req-1)"
	expectedMessage := "预扣费额度失败, 用户剩余额度: ¥0.015200, 需要预扣费额度: **** (request id: req-1)"
	body := `{"error":{"message":"` + rawMessage + `","type":"new_api_error","param":"","code":"insufficient_user_quota"}}`
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	apiErr := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Equal(t, rawMessage, apiErr.Error(), "内部错误应保留原文供服务端排查")
	require.Equal(t, expectedMessage, apiErr.ToOpenAIError().Message)
	require.Equal(t, expectedMessage, apiErr.ToClaudeError().Message)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}
