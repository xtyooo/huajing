package controller

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/longxia6"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLongxia6ModelCatalogAndFetch(t *testing.T) {
	require.Equal(t, "longxia6", constant.GetChannelTypeName(constant.ChannelTypeLongxia6))
	require.Equal(t, "https://api8.longxiaai.store", constant.ChannelBaseURLs[constant.ChannelTypeLongxia6])
	require.Equal(t, longxia6.ModelList, channelId2Models[constant.ChannelTypeLongxia6])
	require.Len(t, longxia6.ModelList, 11)
	for _, suffix := range []string{"", "/v1", "/v1/"} {
		t.Run(suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/models", r.URL.Path)
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"provider-new-video"},{"id":"provider-new-video-PerSecond"}]}`))
			}))
			defer server.Close()
			base := server.URL + suffix
			ch := &model.Channel{Type: constant.ChannelTypeLongxia6, Key: "test-key", BaseURL: &base}
			ids, err := fetchChannelUpstreamModelIDs(ch)
			require.NoError(t, err)
			require.Equal(t, []string{"provider-new-video", "provider-new-video-PerSecond"}, ids)
		})
	}
}
