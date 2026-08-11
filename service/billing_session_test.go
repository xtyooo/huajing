package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewBillingSessionMasksRequiredQuota 验证钱包余额不足时仅隐藏所需额度，同时保留余额、状态码和业务错误码。
func TestNewBillingSessionMasksRequiredQuota(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	const (
		userID           = 91001
		userQuota        = 918
		preConsumedQuota = 3520
	)
	seedUser(t, userID, userQuota)
	relayInfo := &relaycommon.RelayInfo{
		UserId: userID,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}

	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)

	require.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())

	// 同时检查三个真实输出形态，防止后续转换流程重新带出精确所需额度。
	taskErr := TaskErrorFromAPIError(apiErr)
	require.NotNil(t, taskErr)
	messages := []string{
		apiErr.Error(),
		apiErr.ToOpenAIError().Message,
		taskErr.Message,
	}
	for _, message := range messages {
		assert.Contains(t, message, "用户剩余额度: "+logger.FormatQuota(userQuota))
		assert.Contains(t, message, "需要预扣费额度: ****")
		assert.NotContains(t, message, logger.FormatQuota(preConsumedQuota))
	}
}
