package manju

import (
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdministratorModelNamesPreserveMaterials(t *testing.T) {
	for _, modelName := range []string{"wan3.0-video-prime", "renamed-model", "future-i2v", "wan3.0-t2v"} {
		for _, mode := range []string{"auto", "start_frame"} {
			t.Run(modelName+"/"+mode, func(t *testing.T) {
				c, _ := newManjuTestContext(t, `{"model":"wan3-1055","prompt":"p","duration":5,"ratio":"16:9","resolution":"720p","image_urls":["https://example.test/i.png","https://example.test/i.png"],"audio_urls":"https://example.test/a.mp3","video_urls":"https://example.test/v.mp4","video_config":{"reference_mode":"`+mode+`"}}`)
				adaptor := &TaskAdaptor{}
				info := newManjuRelayInfo(modelName)
				require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
				reader, err := adaptor.BuildRequestBody(c, info)
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				var body upstreamRequest
				require.NoError(t, common.Unmarshal(data, &body))
				assert.Equal(t, modelName, body.Model)
				role := "reference_image"
				if mode == "start_frame" {
					role = "first_frame"
				}
				assert.Equal(t, []mediaItem{{Type: role, URL: "https://example.test/i.png"}, {Type: role, URL: "https://example.test/i.png"}, {Type: "reference_video", URL: "https://example.test/v.mp4"}, {Type: "audio", URL: "https://example.test/a.mp3"}}, body.Media)
			})
		}
	}
}

func TestModelCapabilitiesAreValidatedUpstream(t *testing.T) {
	for _, modelName := range []string{"wan3.0-video-prime", "wan3.0-i2v", "wan3.0-r2v"} {
		c, _ := newManjuTestContext(t, `{"model":"wan3-1055-wen","prompt":"p","duration":1,"ratio":"2:1","resolution":"720p"}`)
		adaptor := &TaskAdaptor{}
		info := newManjuRelayInfo(modelName)
		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
		assert.Empty(t, adaptor.body.Media)
		assert.Equal(t, float64(1), adaptor.EstimateBilling(c, info)["seconds"])
	}
	request := map[string]any{"model": "alias", "prompt": strings.Repeat("字", 10001), "duration": 31, "ratio": "2:1", "resolution": "720p"}
	for _, field := range []string{"images", "videos", "audios"} {
		request[field] = strings.Split(strings.TrimSuffix(strings.Repeat("https://example.test/ref,", 11), ","), ",")
	}
	data, err := common.Marshal(request)
	require.NoError(t, err)
	c, _ := newManjuTestContext(t, string(data))
	adaptor := &TaskAdaptor{}
	info := newManjuRelayInfo("renamed-model")
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Len(t, adaptor.body.Media, 33)
	assert.Equal(t, float64(31), adaptor.EstimateBilling(c, info)["seconds"], "new durations must still be billed")

	c, _ = newManjuTestContext(t, `{"model":"alias","prompt":"p","duration":5,"ratio":"16:9","resolution":"720p","audio_urls":"https://example.test/a.mp3"}`)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, []mediaItem{{Type: "audio", URL: "https://example.test/a.mp3"}}, adaptor.body.Media)
}

func TestExtendedDurationResolutionPriceStillCharges(t *testing.T) {
	setManjuResolutionPricing(t, "alias", `{"enabled":true,"setting":{"720p":0.1}}`)
	c, _ := newManjuTestContext(t, `{"model":"alias","prompt":"p","duration":31,"ratio":"16:9","resolution":"720p"}`)
	info := newManjuRelayInfo("wan3.0-video-prime")
	info.OriginModelName = "alias"
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, a.EstimateBilling(c, info))
	assert.Equal(t, 1550000, info.PriceData.Quota)
}
