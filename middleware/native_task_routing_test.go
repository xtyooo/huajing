package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConfiguredNativeVideoWinsSameNamePluginAndRetainsTokenLimits(t *testing.T) {
	require.NoError(t, appI18n.Init())
	setupTaskPluginRouteDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	const key = "native-overlap-fixture"
	_, err := jsplugin.DefaultRegistry.Register(taskProtocolPluginSource(key, "1.0.0", `["operator-future-video"]`, "/v1/videos", `return {action:"text_to_video"};`), jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(key) })
	mapping := `{"operator-future-video":"unknown-provider-video"}`
	channel := &model.Channel{Id: 991, Type: constant.ChannelTypeSnumom, Status: common.ChannelStatusEnabled, Key: "fixture-selected", Models: "operator-future-video", Group: "default", ModelMapping: &mapping}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "operator-future-video", Group: "default", Enabled: true}).Error)
	model.InitChannelCache()
	for _, allowed := range []bool{true, false} {
		router := gin.New()
		router.POST("/v1/videos", func(c *gin.Context) {
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
			common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"operator-future-video": allowed})
		}, PinTaskPluginEndpoint(), PrepareTaskPluginEndpoint(), Distribute(), func(c *gin.Context) {
			_, pinned := c.Get(jsplugin.ContextKeyPinnedPlugin)
			require.False(t, pinned)
			require.Equal(t, constant.ChannelTypeSnumom, c.GetInt("channel_type"))
			require.Equal(t, "operator-future-video", c.GetString("original_model"))
			c.Status(http.StatusNoContent)
		})
		request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"operator-future-video","prompt":"scene","seconds":"5"}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if allowed {
			require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
		} else {
			require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
		}
	}
}
