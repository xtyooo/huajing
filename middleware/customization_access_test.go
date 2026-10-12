package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCustomizationPermissionsRequireRoleScopeAndCurrentGrant(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = wasMaster })
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(model.DB))
	admin := createMiddlewarePATUser(t, "customization-admin", "fixture-legacy-admin")
	require.NoError(t, model.DB.Model(admin).Update("role", common.RoleAdminUser).Error)
	user := createMiddlewarePATUser(t, "customization-user", "fixture-legacy-user")
	for _, route := range []struct {
		method, path, scope string
		permission          authz.Permission
	}{
		{http.MethodGet, "/api/invitation_record/", "invitation:read", authz.InvitationRead},
		{http.MethodGet, "/api/models/pricing", "model_pricing:write", authz.ModelPricingWrite},
		{http.MethodPost, "/api/models/save", "model_pricing:write", authz.ModelPricingWrite},
	} {
		DeclareAccessTokenPermissionRoute(route.method, route.path, route.permission)
		valid, _ := createMiddlewareScopedToken(t, admin.Id, 0, route.scope)
		wrong, _ := createMiddlewareScopedToken(t, admin.Id, 0, "profile:read")
		ordinary, _ := createMiddlewareScopedToken(t, user.Id, 0, route.scope)
		router := gin.New()
		router.Handle(route.method, route.path, AdminAuth(), RequirePermission(route.permission), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		requestWith := func(token string) *httptest.ResponseRecorder {
			request := httptest.NewRequest(route.method, route.path, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			return recorder
		}
		require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{route.permission.Resource: {route.permission.Action: true}}))
		response := requestWith(valid)
		require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
		response = requestWith(wrong)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "ACCESS_TOKEN_SCOPE_DENIED")
		response = requestWith(ordinary)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{route.permission.Resource: {route.permission.Action: false}}))
		response = requestWith(valid)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), "ACCESS_TOKEN_SCOPE_DENIED")
	}
}
