package manying

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newManyingContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func newManyingInfo(originModel, upstreamModel string) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		OriginModelName: originModel,
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	info.UpstreamModelName = upstreamModel
	return info
}

func TestManyingMapsStandardReferencesToShafuJSON(t *testing.T) {
	c, _ := newManyingContext(t, `{
		"model":"public-manying",
		"prompt":"@Image1 @Video1 @Audio1",
		"duration":"8",
		"aspect_ratio":"9:16",
		"resolution":"1080p",
		"image_url":"https://example.test/main.png?token=abc",
		"reference_image_urls":["https://example.test/ref.png"],
		"reference_video":"https://example.test/v.mp4",
		"audio_url":"https://example.test/a.mp3",
		"video_config":{"reference_mode":"auto"},
		"generate_audio":false
	}`)
	adaptor := &TaskAdaptor{}
	info := newManyingInfo("public-manying", "sdf-720p")
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, "sdf-720p", body["model"])
	assert.EqualValues(t, 8, body["duration"])
	assert.Equal(t, "9:16", body["aspect_ratio"])
	assert.Equal(t, "image", body["reference_mode"])
	assert.Equal(t, false, body["generate_audio"])
	assert.Equal(t, []any{"https://example.test/main.png?token=abc", "https://example.test/ref.png"}, body["images"])
	assert.Equal(t, []any{"https://example.test/v.mp4"}, body["reference_videos"])
	assert.Equal(t, []any{"https://example.test/a.mp3"}, body["reference_audios"])
	assert.NotContains(t, body, "resolution")
	assert.NotContains(t, body, "image_url")
	assert.NotContains(t, body, "video_config")
	assert.Equal(t, map[string]float64{"seconds": 8}, adaptor.EstimateBilling(c, info))
}

func TestManyingGenerateAudioDefaultsTrueAndRespectsExplicitChoice(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "omitted defaults true", body: `{"model":"sd-720p","prompt":"city"}`, want: true},
		{name: "explicit true", body: `{"model":"sd-720p","prompt":"city","generate_audio":true}`, want: true},
		{name: "explicit false", body: `{"model":"sd-720p","prompt":"city","generate_audio":false}`, want: false},
		{name: "camel case false", body: `{"model":"sd-720p","prompt":"city","generateAudio":false}`, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newManyingContext(t, test.body)
			adaptor := &TaskAdaptor{}
			info := newManyingInfo("sd-720p", "sd-720p")
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			reader, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, common.Unmarshal(data, &body))
			assert.Equal(t, test.want, body["generate_audio"])
		})
	}
}

func TestManyingMapsStandardFrameModes(t *testing.T) {
	tests := []struct {
		name   string
		mode   string
		images string
	}{
		{name: "start frame", mode: "start_frame", images: `"image_url":"https://example.test/start.png"`},
		{name: "start end", mode: "start_end", images: `"reference_image_urls":["https://example.test/start.png","https://example.test/end.png"]`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `{"model":"sd-480p","prompt":"transition","duration":5,"aspect_ratio":"16:9",` + test.images + `,"video_config":{"reference_mode":"` + test.mode + `"}}`
			c, _ := newManyingContext(t, body)
			adaptor := &TaskAdaptor{}
			info := newManyingInfo("sd-480p", "sd-480p")
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			reader, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			var upstream map[string]any
			require.NoError(t, common.Unmarshal(data, &upstream))
			assert.Equal(t, "frame", upstream["reference_mode"])
		})
	}
}

func TestManyingRejectsInvalidFrameAndReferenceCounts(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "start end needs two images", body: `{"model":"sd-720p","prompt":"p","image_url":"https://example.test/a.png","video_config":{"reference_mode":"start_end"}}`, code: "invalid_reference_count"},
		{name: "frame rejects video", body: `{"model":"sd-720p","prompt":"p","image_url":"https://example.test/a.png","reference_video":"https://example.test/v.mp4","video_config":{"reference_mode":"start_frame"}}`, code: "unsupported_reference_media"},
		{name: "too many image references", body: `{"model":"sd-720p","prompt":"p","reference_image_urls":["1","2","3","4","5","6","7","8","9","10"]}`, code: "invalid_reference_count"},
		{name: "unsupported mapped model", body: `{"model":"alias","prompt":"p"}`, code: "unsupported_model"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			modelName := "sd-720p"
			if test.name == "unsupported mapped model" {
				modelName = "other"
			}
			c, _ := newManyingContext(t, test.body)
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newManyingInfo(test.body, modelName))
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestManyingResolutionPricingUsesMappedFixedResolution(t *testing.T) {
	setManyingResolutionPricing(t, "public-manying", `{"enabled":true,"setting":{"1080p":0.22}}`)
	c, _ := newManyingContext(t, `{"model":"public-manying","prompt":"city","duration":4,"resolution":"480p"}`)
	c.Set("group", "default")
	info := newManyingInfo("public-manying", "sd-1080p")
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	priceData, err := relayhelper.ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)
	info.PriceData = priceData

	ratios := adaptor.EstimateBilling(c, info)

	assert.Nil(t, ratios)
	assert.True(t, info.PriceData.UsePrice)
	assert.Equal(t, 0.22, info.PriceData.ModelPrice)
	assert.Equal(t, 440000, info.PriceData.Quota)
	props, ok := c.Get(string(constant.ContextKeyTaskPropsExtra))
	require.True(t, ok)
	assert.Equal(t, map[string]interface{}{"resolution": "1080P", "duration": 4}, props)
}

func TestManyingResolutionPricingRejectsMissingMappedModelTier(t *testing.T) {
	setManyingResolutionPricing(t, "public-manying", `{"enabled":true,"setting":{"720p":0.13}}`)
	c, _ := newManyingContext(t, `{"model":"public-manying","prompt":"city","duration":4}`)
	info := newManyingInfo("public-manying", "sd-1080p")

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "resolution_price_invalid", taskErr.Code)
}

func TestManyingDelegatesMultipartToShafu(t *testing.T) {
	tests := []struct {
		name          string
		fieldName     string
		generateAudio string
		want          string
	}{
		{name: "omitted defaults true", want: "true"},
		{name: "explicit false", fieldName: "generate_audio", generateAudio: "false", want: "false"},
		{name: "camel case false", fieldName: "generateAudio", generateAudio: "false", want: "false"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload bytes.Buffer
			writer := multipart.NewWriter(&payload)
			require.NoError(t, writer.WriteField("model", "sd-720p"))
			require.NoError(t, writer.WriteField("prompt", "coast"))
			require.NoError(t, writer.WriteField("duration", "6"))
			require.NoError(t, writer.WriteField("aspect_ratio", "4:3"))
			if test.generateAudio != "" {
				require.NoError(t, writer.WriteField(test.fieldName, test.generateAudio))
			}
			require.NoError(t, writer.Close())
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &payload)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			adaptor := &TaskAdaptor{}
			info := newManyingInfo("sd-720p", "sd-720p")

			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			reader, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
			require.NoError(t, err)
			form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(32 << 20)
			require.NoError(t, err)
			t.Cleanup(func() { _ = form.RemoveAll() })
			assert.Equal(t, "sd-720p", form.Value["model"][0])
			require.NotEmpty(t, form.Value["generate_audio"])
			assert.Equal(t, test.want, form.Value["generate_audio"][0])
			assert.Equal(t, map[string]float64{"seconds": 6}, adaptor.EstimateBilling(c, info))
		})
	}
}

func setManyingResolutionPricing(t *testing.T, modelName, value string) {
	t.Helper()
	key := model.ResolutionPriceKey(modelName)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previous, existed := common.OptionMap[key]
	common.OptionMap[key] = value
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[key] = previous
		} else {
			delete(common.OptionMap, key)
		}
	})
}

func TestManyingCatalogAndInheritedResultParsing(t *testing.T) {
	adaptor := &TaskAdaptor{}
	assert.Equal(t, []string{"sd-480p", "sd-720p", "sd-1080p", "sdf-480p", "sdf-720p"}, adaptor.GetModelList())
	assert.Equal(t, "manying", adaptor.GetChannelName())
	result, err := adaptor.ParseTaskResult([]byte(`{"status":"completed","metadata":{"result_url":"https://cdn.example.test/video.mp4"}}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "https://cdn.example.test/video.mp4", result.Url)
	assert.False(t, adaptor.SupportsImageSizePricing())
	assert.NotEmpty(t, strings.TrimSpace(adaptor.GetChannelName()))
}
