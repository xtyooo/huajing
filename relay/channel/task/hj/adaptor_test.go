package hj

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestHJDefaultBaseURLDoesNotDuplicateGatewayPrefix(t *testing.T) {
	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeHJ, ChannelBaseUrl: constant.GetChannelBaseURL(constant.ChannelTypeHJ)}}
	a.Init(info)
	endpoint, err := a.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "http://64.81.114.92:8080/api/gateway/v1/run", endpoint)
}

func TestHJPrefixedBaseURLPollUsesPrivateIDAndSelectedHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/gateway/v1/tasks/private-id", r.URL.Path)
		require.Equal(t, "selected-fixture", r.Header.Get("X-Seedance-Key"))
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	a := &TaskAdaptor{}
	resp, err := a.FetchTask(server.URL+BasePath, "selected-fixture", map[string]any{"task_id": "private-id"}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
}
