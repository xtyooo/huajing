package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDiscardedImmediateTaskCannotBeRetrievedOrProxied(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.PrivateData.ResultDiscarded = true
	task.PrivateData.ResultURL = "https://private-upstream.example.test/inline-only"
	require.NoError(t, model.DB.Save(task).Error)
	for _, route := range []struct {
		path, url string
		handler   gin.HandlerFunc
	}{
		{"/v1/tasks/:key", "/v1/tasks/" + task.TaskID, GetTask},
		{"/v1/tasks/:key/artifacts", "/v1/tasks/" + task.TaskID + "/artifacts", GetTaskArtifacts},
		{"/api/task/:task_id/artifacts", "/api/task/" + task.TaskID + "/artifacts", GetDashboardTaskArtifacts},
		{"/v1/videos/:task_id/content", "/v1/videos/" + task.TaskID + "/content", VideoProxy},
	} {
		router := gin.New()
		router.GET(route.path, func(c *gin.Context) { c.Set("id", task.UserId) }, route.handler)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route.url, nil))
		require.Equal(t, http.StatusNotFound, recorder.Code, route.url)
		require.NotContains(t, recorder.Body.String(), "private-upstream")
	}
}
