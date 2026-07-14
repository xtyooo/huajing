package mimo

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMimoTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx
}

func TestValidateRequestRejectsDurationAboveBillingLimit(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ctx := newMimoTestContext(t, `{"prompt":"test","duration":3601}`)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	taskErr := adaptor.ValidateRequestAndSetAction(ctx, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_duration", taskErr.Code)
}

func TestValidateRequestKeepsValidDurationForBilling(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ctx := newMimoTestContext(t, `{"prompt":"test","duration":120}`)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))
	assert.Equal(t, map[string]float64{"duration": 120}, adaptor.EstimateBilling(ctx, info))
}
