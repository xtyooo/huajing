package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type asyncImageBillingStub struct{}

func (asyncImageBillingStub) Settle(int) error         { return nil }
func (asyncImageBillingStub) Refund(*gin.Context)      {}
func (asyncImageBillingStub) NeedsRefund() bool        { return false }
func (asyncImageBillingStub) GetPreConsumedQuota() int { return 0 }
func (asyncImageBillingStub) Reserve(int) error        { return nil }

func setAsyncImagePricingForTest(t *testing.T, modelName string) {
	t.Helper()
	key := model.ImageSizePriceKey(modelName)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousValue, existed := common.OptionMap[key]
	common.OptionMap[key] = `{"enabled":true,"setting":{"1k":0.01,"2k":0.02,"4k":0.04}}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[key] = previousValue
		} else {
			delete(common.OptionMap, key)
		}
	})
}

func TestRelayTaskSubmitRejectsInvalidImageResolutionBeforeUpstreamAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const modelName = "async-image-invalid-resolution"
	setAsyncImagePricingForTest(t, modelName)

	var upstreamCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"`+modelName+`","prompt":"draw","resolution":"720p"}`))
	request.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request
	ctx.Set("platform", strconv.Itoa(constant.ChannelTypeSora))
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeSora)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, server.URL)
	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "default",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	result, taskErr := RelayTaskSubmit(ctx, info)

	require.Nil(t, result)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.True(t, taskErr.LocalError)
	assert.Zero(t, upstreamCalls.Load())
	assert.Nil(t, info.Billing)
}

func TestRelayTaskSubmitImageSizePricingDoesNotMergeVideoEstimateRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const modelName = "async-image-no-video-ratios"
	setAsyncImagePricingForTest(t, modelName)
	savedGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"async-image-free":0}`))

	var upstreamCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-task","object":"image","status":"processing"}`))
	}))
	t.Cleanup(server.Close)

	request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"`+modelName+`","prompt":"draw","resolution":"2K","duration":15,"size":"1792x1024"}`))
	request.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request
	ctx.Set("platform", strconv.Itoa(constant.ChannelTypeSora))
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeSora)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, server.URL)
	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "async-image-free",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		Billing:         asyncImageBillingStub{},
	}

	result, taskErr := RelayTaskSubmit(ctx, info)

	require.Nil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, int32(1), upstreamCalls.Load())
	assert.True(t, info.PriceData.ImageSizePricing)
	assert.Equal(t, "2k", info.PriceData.ImageSizeTier)
	assert.Equal(t, 0.02, info.PriceData.ModelPrice)
	assert.Zero(t, result.Quota)
	assert.Nil(t, info.PriceData.OtherRatios(), "Sora duration and size estimates must not enter image-size billing")
}

func TestTaskImageSizePricingCapabilityIsScopedToStandardSoraAdaptor(t *testing.T) {
	_, soraSupportsImagePricing := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora))).(channel.TaskImageSizePricingAdaptor)
	_, openAISupportsImagePricing := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAI))).(channel.TaskImageSizePricingAdaptor)
	_, anheSupportsImagePricing := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAnhe))).(channel.TaskImageSizePricingAdaptor)

	assert.True(t, soraSupportsImagePricing)
	assert.True(t, openAISupportsImagePricing)
	assert.False(t, anheSupportsImagePricing)
}
