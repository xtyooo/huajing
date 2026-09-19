package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchYaochenModelsUsesVideoKeys(t *testing.T) {
	for _, suffix := range []string{"", "/", "/v1", "/v1/"} {
		t.Run("base"+suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/v1/models", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"text":{"text-model":{}},"image":{"image-model":{}},"video":{"future-model-z":{"display_name":"Not a model ID"},"actual-model-a":{"pricing_mode":"per_request","fixed_duration_seconds":30}," ":{}}}`))
			}))
			defer server.Close()
			baseURL := server.URL + suffix
			channel := &model.Channel{Type: constant.ChannelTypeYaochen, Key: "test-key", BaseURL: &baseURL}
			ids, err := fetchChannelUpstreamModelIDs(channel)
			require.NoError(t, err)
			assert.Equal(t, []string{"actual-model-a", "future-model-z"}, ids)
		})
	}
}

func TestFetchYaochenModelsPreservesHeaderOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Token test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "custom", r.Header.Get("X-Discovery"))
		_, _ = w.Write([]byte(`{"video":{"actual-id":{}}}`))
	}))
	defer server.Close()
	baseURL := server.URL
	override := `{"Authorization":"Token {api_key}","X-Discovery":"custom"}`
	channel := &model.Channel{Type: constant.ChannelTypeYaochen, Key: "test-key", BaseURL: &baseURL, HeaderOverride: &override}
	ids, err := fetchChannelUpstreamModelIDs(channel)
	require.NoError(t, err)
	assert.Equal(t, []string{"actual-id"}, ids)
}

func TestFetchYaochenModelsRejectsInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   string
		status int
		want   string
	}{
		{"malformed", `{"video":`, 200, "invalid Yaochen Models response"},
		{"missing video", `{"text":{}}`, 200, "video is required"},
		{"null video", `{"video":null}`, 200, "video is required"},
		{"array video", `{"video":[]}`, 200, "invalid Yaochen Models response"},
		{"bad entry", `{"video":{"invalid":"display name"}}`, 200, "invalid Yaochen Models response"},
		{"empty video", `{"video":{}}`, 200, "no valid video model IDs"},
		{"blank ID", `{"video":{" ":{}}}`, 200, "no valid video model IDs"},
		{"upstream error", `{"error":"secret"}`, 401, "status code: 401"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			baseURL := server.URL
			channel := &model.Channel{Type: constant.ChannelTypeYaochen, Key: "test-key", BaseURL: &baseURL}
			ids, err := fetchChannelUpstreamModelIDs(channel)
			require.ErrorContains(t, err, test.want)
			assert.Nil(t, ids)
		})
	}
}

func TestFetchOpenAIModelsIgnoresYaochenVideoObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"ordinary-model"}],"video":{"unrelated":{}}}`))
	}))
	defer server.Close()
	baseURL := server.URL
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "test-key", BaseURL: &baseURL}
	ids, err := fetchChannelUpstreamModelIDs(channel)
	require.NoError(t, err)
	assert.Equal(t, []string{"ordinary-model"}, ids)
}
