package vidu

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestReferenceVideoPreservesAdministratorModel(t *testing.T) {
	for _, name := range []string{"viduq2-pro", "viduq2-turbo", "future-vidu", "renamed-viduq2-model"} {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "p", Images: []string{"https://example.test/ref.png"}, Duration: 5})
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: name}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionReferenceGenerate}}
			reader, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
			require.NoError(t, err)
			var body requestPayload
			require.NoError(t, common.DecodeJson(reader, &body))
			require.Equal(t, name, body.Model)
			require.Equal(t, []string{"https://example.test/ref.png"}, body.Images)
		})
	}
}
