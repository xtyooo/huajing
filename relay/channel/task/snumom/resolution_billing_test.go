package snumom

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setResolutionBillingOption(t *testing.T, name, value string) {
	t.Helper()
	key := model.ResolutionPriceKey(name)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	old, existed := common.OptionMap[key]
	common.OptionMap[key] = value
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[key] = old
		} else {
			delete(common.OptionMap, key)
		}
	})
}

func TestResolutionBillingScreenshotPricesAndMappedModels(t *testing.T) {
	setResolutionBillingOption(t, "public-video", `{"enabled":true,"setting":{"480p":0.23,"720p":0.28,"1080p":0.33}}`)
	setResolutionBillingOption(t, "dreamina-seedance-2-0", `{"enabled":true,"setting":{"720p":9}}`)
	previous := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previous)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"discount":0.5}`))
	for _, tc := range []struct {
		name, fields, upstream, group, tier string
		price                               float64
		quota                               int
	}{
		{"480p", `"duration":10,"resolution":"480P"`, "future-renamed-model", "default", "480p", 0.23, 1150000},
		{"720p", `"seconds":"10","resolution":"720p"`, "dreamina-seedance-2-0", "default", "720p", 0.28, 1400000},
		{"1080p", `"duration":"10","size":"1920x1080"`, "grok-imagine-video-1.5", "default", "1080p", 0.33, 1650000},
		{"portrait pixels and discount", `"seconds":10,"size":"720X1280"`, "future-renamed-model", "discount", "720p", 0.28, 700000},
		{"extra canonical fields", `"extra":{"duration":10,"resolution":"720p"}`, "future-renamed-model", "default", "720p", 0.28, 1400000},
		{"native metadata", `"metadata":{"duration":"10","resolution":"720P","generate_audio":false}`, "doubao-seedance-new", "default", "720p", 0.28, 1400000},
		{"matching nested pricing attributes", `"duration":10,"resolution":"720p","metadata":{"parameters":{"seconds":"10","size":"1280x720"}}`, "future-renamed-model", "default", "720p", 0.28, 1400000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, info := testContext(t, `{"model":"public-video","prompt":"scene",`+tc.fields+`}`, tc.upstream)
			info.UserGroup, info.UsingGroup = "default", tc.group
			a := &TaskAdaptor{}
			a.Init(info)
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			price, err := helper.ModelPriceHelperPerCall(c, info)
			require.NoError(t, err)
			assert.Zero(t, price.Quota, "common helper leaves enabled resolution pricing to adaptor")
			info.PriceData = price
			assert.Empty(t, a.EstimateBilling(c, info), "absolute quota must not get another duration ratio")
			assert.Equal(t, tc.quota, info.PriceData.Quota)
			assert.Equal(t, tc.price, info.PriceData.ModelPrice, "upstream mapping must not pick the upstream price")
			assert.True(t, info.PriceData.UsePrice, "completed task must preserve precharged quota")
			assert.Empty(t, info.PriceData.OtherRatios())
			props, exists := c.Get(string(constant.ContextKeyTaskPropsExtra))
			require.True(t, exists)
			assert.Equal(t, map[string]interface{}{"resolution": tc.tier, "duration": 10}, props)
			assert.Empty(t, a.AdjustBillingOnSubmit(info, []byte(`{"status":"queued","duration":999}`)))
			assert.Nil(t, info.QuotaClamp)
		})
	}
}

func TestResolutionBillingRejectsAmbiguousOrUnpricedInputs(t *testing.T) {
	setResolutionBillingOption(t, "public-video", `{"enabled":true,"setting":{"720p":0.28}}`)
	for _, fields := range []string{
		`"resolution":"720p"`, `"duration":-1,"resolution":"720p"`, `"duration":0,"resolution":"720p"`,
		fmt.Sprintf(`"duration":%d,"resolution":"720p"`, relaycommon.MaxTaskDurationSeconds+1),
		`"duration":10`, `"duration":10,"resolution":"auto"`, `"duration":10,"resolution":"480p"`,
		`"duration":10,"resolution":"720p","size":"1920x1080"`,
		`"duration":10,"resolution":"720p","metadata":{"duration":20}`,
		`"duration":10,"resolution":"720p","metadata":{"parameters":{"seconds":20}}`,
		`"duration":10,"resolution":"720p","metadata":{"resolution":"1080p"}`,
		`"duration":10,"resolution":"720p","metadata":{"nested":{"size":"1920x1080"}}`,
		`"duration":10,"resolution":"720p","metadata":{"nested":{"seconds":null}}`,
		`"metadata":{"parameters":{"seconds":10,"resolution":"720p"}}`,
		`"duration":10,"size":"-720x1280"`, `"duration":10,"resolution":"720p","metadata":{"size":720}`,
	} {
		t.Run(fields, func(t *testing.T) {
			payload := `{"model":"public-video","prompt":"scene",` + fields + `}`
			var input map[string]any
			require.NoError(t, common.Unmarshal([]byte(payload), &input), "fixture must reach billing validation, not malformed JSON")
			c, _, info := testContext(t, payload, "future-model")
			a := &TaskAdaptor{}
			err := a.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, err)
			assert.Equal(t, 400, err.StatusCode)
			assert.True(t, err.LocalError)
			assert.NotEqual(t, "invalid_json", err.Code)
			assert.Nil(t, a.EstimateBilling(c, info), "failed validation must not retain billing state")
			_, bodyErr := a.BuildRequestBody(c, info)
			assert.Error(t, bodyErr)
		})
	}
}

func TestResolutionBillingPixelWireMatchesEstimate(t *testing.T) {
	setResolutionBillingOption(t, "public-video", `{"enabled":true,"setting":{"720p":0.28}}`)
	for _, tc := range []struct {
		name, fields, upstream, want string
	}{
		{"metadata-only pixel size", `"metadata":{"duration":10,"size":"1280x720"}`, "dreamina-seedance-2-0", `{"model":"dreamina-seedance-2-0","prompt":"scene","seconds":10,"size":"1280x720","metadata":{"duration":10,"size":"1280x720"}}`},
		{"metadata-only pixel resolution", `"metadata":{"seconds":"10","resolution":"720X1280"}`, "dreamina-seedance-2-0", `{"model":"dreamina-seedance-2-0","prompt":"scene","seconds":10,"resolution":"720p","metadata":{"seconds":"10","resolution":"720p"}}`},
		{"pixel resolution", `"duration":10,"resolution":"1280x720"`, "dreamina-seedance-2-0", `{"model":"dreamina-seedance-2-0","prompt":"scene","seconds":10,"resolution":"720p"}`},
		{"Grok metadata-only pixel size", `"metadata":{"duration":10,"size":"1280x720"}`, "grok-imagine-video-1.5", `{"model":"grok-imagine-video-1.5","prompt":"scene","seconds":"10","size":"1280x720","metadata":{"duration":10,"size":"1280x720"}}`},
		{"Doubao metadata-only pixel size", `"metadata":{"duration":10,"size":"1280x720"}`, "doubao-seedance-new", `{"model":"doubao-seedance-new","prompt":"scene","duration":10,"metadata":{"duration":10,"size":"1280x720","resolution":"720p","ratio":"16:9","content":[{"type":"text","text":"scene"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, info := testContext(t, `{"model":"public-video","prompt":"scene",`+tc.fields+`}`, tc.upstream)
			a := &TaskAdaptor{}
			a.Init(info)
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			assert.JSONEq(t, tc.want, bodyBytes(t, a, c, info))
			info.PriceData.GroupRatioInfo.GroupRatio = 1
			assert.Nil(t, a.EstimateBilling(c, info))
			assert.Equal(t, 1400000, info.PriceData.Quota)
		})
	}
}

func TestResolutionBillingInvalidSettings(t *testing.T) {
	for _, value := range []string{`{`, `{"enabled":true,"setting":{}}`, `{"enabled":true,"setting":{"720p":0}}`, `{"enabled":true,"setting":{"720p":-0.1}}`} {
		t.Run(value, func(t *testing.T) {
			setResolutionBillingOption(t, "public-video", value)
			c, _, info := testContext(t, `{"model":"public-video","prompt":"scene","duration":10,"resolution":"720p"}`, "future-model")
			err := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, err)
			assert.Equal(t, "resolution_price_invalid", err.Code)
		})
	}
}

func TestResolutionBillingStateResetAndQuotaSaturation(t *testing.T) {
	setResolutionBillingOption(t, "public-video", `{"enabled":true,"setting":{"720p":100000000}}`)
	c, _, info := testContext(t, fmt.Sprintf(`{"model":"public-video","prompt":"scene","duration":%d,"resolution":"720p"}`, relaycommon.MaxTaskDurationSeconds), "future-model")
	info.UserGroup, info.UsingGroup = "default", "default"
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	assert.Nil(t, a.EstimateBilling(c, info))
	assert.Equal(t, common.MaxQuota, info.PriceData.Quota)
	require.NotNil(t, info.QuotaClamp)
	assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
	setResolutionBillingOption(t, "public-video", `{"enabled":false}`)
	c, _, info = testContext(t, `{"model":"public-video","prompt":"scene","duration":-1}`, "future-model")
	require.Nil(t, a.ValidateRequestAndSetAction(c, info), "base pricing must not require duration/tier")
	info.PriceData.Quota = 123
	assert.Nil(t, a.EstimateBilling(c, info))
	assert.Equal(t, 123, info.PriceData.Quota, "reused adaptor must not reuse old pricing")
	c, _, info = testContext(t, `{"model":"public-video","prompt":"scene"}`, "future-model")
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.JSONEq(t, `{"model":"future-model","prompt":"scene"}`, bodyBytes(t, a, c, info))
}
