package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeMediaProxySelectedKeyStaysOnProviderOrigin(t *testing.T) {
	const signedQuery = "signature=a%2Bb&expires=123"
	for _, channelType := range []int{constant.ChannelTypeSnumom, constant.ChannelTypeManju, constant.ChannelTypeDiaomao, constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, target := range []string{"origin", "cdn", "redirect"} {
			t.Run(strconv.Itoa(channelType)+"/"+target, func(t *testing.T) {
				selectedKey := "fixture-selected-media-key"
				if channelType == constant.ChannelTypeVertexAi {
					selectedKey = `{"client_email":"fixture@example.invalid","private_key":"fixture-not-used-for-signed-result"}`
				}
				task := setupGenericTaskTest(t)
				allowPrivateTaskMediaTest(t)
				previousMemoryCache := common.MemoryCacheEnabled
				common.MemoryCacheEnabled = false
				t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })
				calls := 0
				cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					assert.Empty(t, r.Header.Get("Authorization"))
					assert.Empty(t, r.Header.Get("x-goog-api-key"))
					assert.Equal(t, signedQuery, r.URL.RawQuery)
					assert.Equal(t, "bytes=0-2", r.Header.Get("Range"))
					w.Header().Set("Content-Type", "video/mp4")
					w.Header().Set("Content-Range", "bytes 0-2/3")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte("mp4"))
				}))
				t.Cleanup(cdn.Close)
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if channelType == constant.ChannelTypeGemini {
						assert.Equal(t, selectedKey, r.Header.Get("x-goog-api-key"))
						assert.Empty(t, r.Header.Get("Authorization"))
					} else if channelType == constant.ChannelTypeVertexAi {
						assert.Empty(t, r.Header.Get("Authorization"))
						assert.Empty(t, r.Header.Get("x-goog-api-key"))
					} else {
						assert.Equal(t, "Bearer "+selectedKey, r.Header.Get("Authorization"))
						assert.Empty(t, r.Header.Get("x-goog-api-key"))
					}
					assert.Equal(t, signedQuery, r.URL.RawQuery, "never add a credential to a signed result URL")
					assert.Equal(t, "bytes=0-2", r.Header.Get("Range"))
					if target == "redirect" {
						w.Header().Set("Location", cdn.URL+"/content?"+signedQuery)
						w.WriteHeader(http.StatusTemporaryRedirect)
						return
					}
					w.Header().Set("Content-Type", "video/mp4")
					w.Header().Set("Content-Range", "bytes 0-2/3")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte("mp4"))
				}))
				t.Cleanup(origin.Close)
				resultURL := origin.URL + "/content?" + signedQuery
				if target == "cdn" {
					resultURL = cdn.URL + "/content?" + signedQuery
				}
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", task.ChannelId).Updates(map[string]any{
					"type": channelType, "base_url": origin.URL, "key": "fixture-other-channel-key",
				}).Error)
				task.Platform = constant.TaskPlatform(strconv.Itoa(channelType))
				task.MediaStatus = model.MediaStatusSuccess
				task.MediaURL = "https://local.example/media/fixture.mp4"
				task.PrivateData = model.TaskPrivateData{Key: selectedKey, UpstreamTaskID: "fixture-private-task", ResultURL: resultURL}
				if channelType == constant.ChannelTypeGemini {
					data, err := common.Marshal(map[string]any{"response": map[string]any{
						"generateVideoResponse": map[string]any{"generatedSamples": []any{map[string]any{"video": map[string]any{"uri": resultURL}}}},
					}})
					require.NoError(t, err)
					task.Data = data
				}
				require.NoError(t, model.DB.Save(task).Error)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Set("id", task.UserId)
				c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
				c.Request.Header.Set("Range", "bytes=0-2")
				VideoProxy(c)
				assert.Equal(t, http.StatusPartialContent, recorder.Code, recorder.Body.String())
				assert.Equal(t, "mp4", recorder.Body.String())
				assert.Equal(t, "bytes 0-2/3", recorder.Header().Get("Content-Range"))
				assert.NotContains(t, recorder.Body.String(), selectedKey)
				wantCalls := 1
				if target == "redirect" {
					wantCalls = 2
				}
				assert.Equal(t, wantCalls, calls)
			})
		}
	}
}
