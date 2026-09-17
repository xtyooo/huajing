package taskcommon

import (
	"bytes"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestMediaURLListMultipartSingleAndRepeatedFields(t *testing.T) {
	for _, count := range []int{1, 2} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for _, key := range []string{"images", "reference_videos", "audio_urls"} {
			for i := 0; i < count; i++ {
				require.NoError(t, writer.WriteField(key, "https://example.test/media"))
			}
		}
		require.NoError(t, writer.Close())
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/videos", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		var request struct {
			Images MediaURLList `json:"images"`
			Videos MediaURLList `json:"reference_videos"`
			Audios MediaURLList `json:"audio_urls"`
		}
		require.NoError(t, common.UnmarshalBodyReusable(c, &request))
		require.Len(t, request.Images, count)
		require.Len(t, request.Videos, count)
		require.Len(t, request.Audios, count)
		require.Equal(t, "https://example.test/media", request.Audios[0])
	}
}

func TestMediaURLListJSON(t *testing.T) {
	for _, tc := range []struct {
		input   string
		want    MediaURLList
		invalid bool
	}{
		{`"https://example.test/a?sig=x,y"`, MediaURLList{"https://example.test/a?sig=x,y"}, false},
		{`["https://example.test/a","https://example.test/b"]`, MediaURLList{"https://example.test/a", "https://example.test/b"}, false},
		{`[]`, MediaURLList{}, false},
		{`null`, nil, false},
		{`""`, nil, false},
		{`"  "`, nil, false},
		{`123`, nil, true},
		{`true`, nil, true},
		{`{"url":"https://example.test/a"}`, nil, true},
		{`["https://example.test/a",1]`, nil, true},
		{`[null]`, nil, true},
		{`[["https://example.test/a"]]`, nil, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := MediaURLList{"old"}
			err := common.Unmarshal([]byte(tc.input), &got)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			encoded, err := common.Marshal(got)
			require.NoError(t, err)
			var upstream []string
			require.NoError(t, common.Unmarshal(encoded, &upstream))
			require.Equal(t, []string(tc.want), upstream)
		})
	}
}
