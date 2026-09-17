package zhou_sd

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise the actual request DTO and reusable HTTP decoder for every media
// list alias, so adding a strict slice field cannot silently regress clients.
func TestMediaListFieldsAcceptSingleURLAndArray(t *testing.T) {
	typ := reflect.TypeOf(standardVideoRequest{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Slice || field.Type.Elem().Kind() != reflect.String {
			continue
		}
		key := strings.Split(field.Tag.Get("json"), ",")[0]
		t.Run(key, func(t *testing.T) {
			for _, value := range []string{`"https://example.test/media?sig=a,b%2Fc"`, `["https://example.test/media?sig=a,b%2Fc"]`} {
				body := fmt.Sprintf("{%q:%s}", key, value)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				var request standardVideoRequest
				require.NoError(t, common.UnmarshalBodyReusable(c, &request))
				got := reflect.ValueOf(request).Field(i)
				require.Equal(t, 1, got.Len())
				require.Equal(t, "https://example.test/media?sig=a,b%2Fc", got.Index(0).String())
			}
		})
	}
}
