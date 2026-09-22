package autodl_h3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenamedWorkflowPreservesExplicitSchemaAndBilling(t *testing.T) {
	for _, tc := range []struct{ workflow, body string }{
		{TextWorkflowID, `{"model":"public","prompt":"scene","duration":60,"resolution":"1080p"}`},
		{ReferenceWorkflowID, `{"model":"public","prompt":"scene","seed":0,"images":["https://example.com/a.png"],"audios":"https://example.com/a.mp3","duration":60}`},
		{FirstLastWorkflowID, `{"model":"public","prompt":"scene","first_frame":"https://example.com/a.png","last_frame":"https://example.com/b.png","seed":123,"duration":60}`},
		{FirstLastWorkflowID, `{"model":"public","prompt":"scene","reference_mode":"start_end","images":["https://example.com/a.png","https://example.com/b.png"]}`},
		{ReferenceWorkflowID, `{"model":"public","prompt":"scene","seed":0}`},
	} {
		t.Run(tc.workflow+tc.body, func(t *testing.T) {
			var previous map[string]any
			for _, workflow := range []string{tc.workflow, "admin-renamed/workflow"} {
				c, _ := newAutoDLH3TestContext(t, tc.body)
				info := testRelayInfo()
				info.UpstreamModelName = workflow
				a := &TaskAdaptor{baseURL: "https://autodl.example"}
				require.Nil(t, a.ValidateRequestAndSetAction(c, info))
				body := decodeRequestBody(t, a, c, info)
				if previous != nil {
					assert.Equal(t, previous, body)
				}
				previous = body
				assert.Equal(t, map[string]float64{"seconds": body["duration"].(float64)}, a.EstimateBilling(c, info))
				endpoint, err := a.BuildRequestURL(info)
				require.NoError(t, err)
				if strings.Contains(workflow, "/") {
					assert.Contains(t, endpoint, "admin-renamed%2Fworkflow")
				}
			}
		})
	}
}

func TestConfiguredWorkflowsDelegateReferenceCountsAndPromptLength(t *testing.T) {
	images := []string{}
	audios := []string{}
	for n := 0; n < 12; n++ {
		images = append(images, fmt.Sprintf("https://example.com/%d.png", n))
		audios = append(audios, fmt.Sprintf("https://example.com/%d.mp3", n))
	}
	data, err := common.Marshal(map[string]any{"model": "public", "prompt": strings.Repeat("p", 10001), "images": images, "audios": audios})
	require.NoError(t, err)
	for _, workflow := range []string{TextWorkflowID, ReferenceWorkflowID, "new-provider-workflow"} {
		c, _ := newAutoDLH3TestContext(t, string(data))
		info := testRelayInfo()
		info.UpstreamModelName = workflow
		a := &TaskAdaptor{}
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		body := decodeRequestBody(t, a, c, info)
		assert.Equal(t, images[11], body["ref_image_11"])
		assert.Equal(t, audios[11], body["ref_audio_11"])
		assert.Equal(t, strings.Repeat("p", 10001), body["prompt"])
	}
}
