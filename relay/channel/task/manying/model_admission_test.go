package manying

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdministratorModelAdmissionAndPricing(t *testing.T) {
	saved := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(saved)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"public-custom":0.12}`))
	for _, multipartBody := range []bool{false, true} {
		for _, tc := range []struct {
			name, resolution, size string
			configured, wantError  bool
		}{
			{name: "base price without resolution"},
			{name: "explicit resolution", resolution: "720p", configured: true},
			{name: "pixel size", size: "1280x720", configured: true},
			{name: "ambiguous tier", configured: true, wantError: true},
		} {
			t.Run(tc.name+map[bool]string{true: "/multipart", false: "/json"}[multipartBody], func(t *testing.T) {
				if tc.configured {
					setManyingResolutionPricing(t, "public-custom", `{"enabled":true,"setting":{"720p":0.1}}`)
				}
				fields := map[string]string{"model": "public-custom", "prompt": "test", "duration": "6", "aspect_ratio": "16:9", "resolution": tc.resolution, "size": tc.size}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				if multipartBody {
					var payload bytes.Buffer
					writer := multipart.NewWriter(&payload)
					for key, value := range fields {
						require.NoError(t, writer.WriteField(key, value))
					}
					require.NoError(t, writer.Close())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &payload)
					c.Request.Header.Set("Content-Type", writer.FormDataContentType())
				} else {
					data, err := common.Marshal(fields)
					require.NoError(t, err)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(data))
					c.Request.Header.Set("Content-Type", "application/json")
				}
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				info := newManyingInfo("public-custom", "administrator-new-model")
				a := &TaskAdaptor{}
				err := a.ValidateRequestAndSetAction(c, info)
				if tc.wantError {
					require.NotNil(t, err)
					assert.Equal(t, "resolution_price_invalid", err.Code)
					return
				}
				require.Nil(t, err)
				price, priceErr := relayhelper.ModelPriceHelperPerCall(c, info)
				require.NoError(t, priceErr)
				info.PriceData = price
				ratios := a.EstimateBilling(c, info)
				if tc.configured {
					assert.Empty(t, ratios)
					assert.Equal(t, 300000, info.PriceData.Quota)
				} else {
					assert.Equal(t, 60000, price.Quota)
					assert.Equal(t, map[string]float64{"seconds": 6}, ratios)
				}
				body, buildErr := a.BuildRequestBody(c, info)
				require.NoError(t, buildErr)
				data, readErr := io.ReadAll(body)
				require.NoError(t, readErr)
				if multipartBody {
					_, params, parseErr := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
					require.NoError(t, parseErr)
					form, parseErr := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1024)
					require.NoError(t, parseErr)
					defer form.RemoveAll()
					assert.Equal(t, []string{"administrator-new-model"}, form.Value["model"])
				} else {
					var output map[string]any
					require.NoError(t, common.Unmarshal(data, &output))
					assert.Equal(t, "administrator-new-model", output["model"])
				}
			})
		}
	}
}
