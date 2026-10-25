package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXxuanDispatchedCreateNeverRetries(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusTemporaryRedirect, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("xxuan_task_submit_started", true)
			require.False(t, shouldRetryTaskRelay(c, 1, &taskdto.TaskError{StatusCode: status}, 5))
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.True(t, shouldRetryTaskRelay(c, 1, &taskdto.TaskError{StatusCode: http.StatusTooManyRequests}, 1), "other providers retain existing retry behavior")
}

func TestXxuanModelCatalogUsesOpaqueProductIDs(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/open/v1/catalog", r.URL.Path)
		assert.Equal(t, "Bearer selected-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"products":[{"product_id":"opaque-custom","display_name":"renamed","available":true,"unit_price_cents":99999},{"product_id":"future-id","operation_type":"generate"},{"product_id":"opaque-custom"},{"product_id":"disabled","available":false},{"product_id":"upscale-internal","operation_type":"upscale"}]}`))
	}))
	defer server.Close()
	for _, base := range []string{server.URL, server.URL + "/open/v1/", server.URL + "/v1", server.URL + "/open"} {
		channel := &model.Channel{Type: constant.ChannelTypeXxuan, Key: "selected-key", BaseURL: &base, Models: "retail-alias"}
		ids, err := fetchChannelUpstreamModelIDs(channel)
		require.NoError(t, err)
		assert.Equal(t, []string{"opaque-custom", "future-id"}, ids)
		assert.Equal(t, "retail-alias", channel.Models, "catalog must not mutate configured admission or prices")
	}
	assert.Equal(t, 4, calls)
	base := server.URL
	ids, err := fetchChannelUpstreamModelIDs(&model.Channel{Type: constant.ChannelTypeXxuanUpscale, BaseURL: &base})
	require.NoError(t, err)
	assert.Equal(t, []string{"xxuan-video-upscale"}, ids)
	assert.Equal(t, 4, calls, "upscale options are not a model listing")
}

func TestXxuanCatalogErrorsAndChannelTestDoNotCreateTasks(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"http error", `{"products":[]}`, 503},
		{"invalid JSON", `{`, 200},
		{"wrong envelope", `{"data":[]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/open/v1/catalog", r.URL.Path)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			base := server.URL
			_, err := fetchChannelUpstreamModelIDs(&model.Channel{Type: constant.ChannelTypeXxuan, Key: "selected-key", BaseURL: &base})
			require.Error(t, err)
		})
	}
	for _, channelType := range []int{constant.ChannelTypeXxuan, constant.ChannelTypeXxuanUpscale} {
		result := testChannel(context.Background(), &model.Channel{Type: channelType}, 1, "future-model", "", false)
		require.ErrorContains(t, result.localErr, "channel test is not supported")
	}
}
