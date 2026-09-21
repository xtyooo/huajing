package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/task/longxia6"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLongxia6MappedModelAdminBilling(t *testing.T) {
	const alias = "public-longxia-billing-test"
	const upstream = "LongXia-video-seedance2_0-standard-express"
	savedPrices := ratio_setting.ModelPrice2JSONString()
	savedGroups := ratio_setting.GroupRatio2JSONString()
	settings, ok := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	require.True(t, ok)
	savedSkip := settings.SkipSeconds
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroups))
		settings.SkipSeconds = savedSkip
	})
	prices, err := common.Marshal(map[string]float64{alias: 0.02, upstream: 9, upstream + "-PerSecond": 9})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"longxia-discount":0.5}`))

	for _, tc := range []struct {
		name      string
		upstream  string
		group     string
		skip      bool
		wantQuota int
	}{
		{"per-call duration does not multiply", upstream, "default", false, 10000},
		{"mapped per-second multiplies once", upstream + "-PerSecond", "default", false, 100000},
		{"per-second group discount", upstream + "-PerSecond", "longxia-discount", false, 50000},
		{"public alias skip seconds overrides upstream SKU", upstream + "-PerSecond", "default", true, 10000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.SkipSeconds = map[string]bool{alias: tc.skip}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"`+alias+`","prompt":"test","duration":10,"ratio":"16:9"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
			info := &relaycommon.RelayInfo{
				OriginModelName: alias,
				UserGroup:       "default",
				UsingGroup:      tc.group,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: tc.upstream},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "public-task"},
			}
			adaptor := &longxia6.TaskAdaptor{}
			adaptor.Init(info)
			require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))
			price, err := helper.ModelPriceHelperPerCall(ctx, info)
			require.NoError(t, err)
			require.True(t, price.UsePrice)
			assert.Equal(t, 0.02, price.ModelPrice, "pricing uses public alias, not upstream name")
			info.PriceData = price
			for name, ratio := range adaptor.EstimateBilling(ctx, info) {
				info.PriceData.AddOtherRatio(name, ratio)
			}
			applyTaskPriceRatios(info, info.PriceData.UsePrice && billing_setting.GetSkipSeconds(info.OriginModelName))
			assert.Equal(t, tc.wantQuota, info.PriceData.Quota)
			assert.False(t, info.PriceData.FreeModel)
			assert.Empty(t, adaptor.AdjustBillingOnSubmit(info, []byte(`{"id":"private-task","status":"queued"}`)), "submission must not add duration a second time")
		})
	}
}
