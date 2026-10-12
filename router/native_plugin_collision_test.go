package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeCompatibilityRoutesKeepFactoryPluginsActive(t *testing.T) {
	for _, test := range []struct{ key, path string }{
		{"sunoapi", "/suno/fetch/:id"},
		{"kling", "/kling/v1/videos/text2video/:task_id"},
		{"jimeng", "/jimeng/"},
	} {
		t.Run(test.key, func(t *testing.T) {
			method := http.MethodGet
			if test.key == "jimeng" {
				method = http.MethodPost
			}
			outer := gin.New()
			outer.Handle(method, test.path, func(c *gin.Context) { c.String(http.StatusOK, "native") })
			plugin := compileRouterPlugin(t, test.key, "1.0.0", `[{method:"`+method+`",path:"`+test.path+`",type:"dynamic"}]`)
			registry := jsplugin.NewRegistry()
			require.NoError(t, registry.ReplaceOverrides([]*jsplugin.LoadedPlugin{plugin}))
			builder := newPluginGenerationBuilder(outer.Routes(), nil, testPluginRouteHandlers(func(c *gin.Context, _ *jsplugin.RoutingGeneration, _ jsplugin.RouteBinding) {
				c.String(http.StatusOK, "plugin")
			}))
			require.NoError(t, registry.SetGenerationPreparer(builder.prepare))
			_, active := registry.Generation().Get(test.key)
			require.True(t, active)
			require.Empty(t, registry.RoutingErrors()[test.key])
			foreign := compileRouterPlugin(t, "foreign-host-claim", "1.0.0", `[{method:"`+method+`",path:"`+test.path+`",type:"dynamic"}]`)
			require.ErrorContains(t, builder.validatePlugin(foreign, nil), "intersects static route")
		})
	}
}
