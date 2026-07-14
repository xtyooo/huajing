package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskResponseBufferDoesNotCommitBeforeFlush(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	buffer := newTaskResponseBuffer(ctx.Writer)

	buffer.Header().Set("Content-Type", "application/json")
	buffer.WriteHeader(http.StatusAccepted)
	_, err := buffer.WriteString(`{"id":"task_123"}`)
	require.NoError(t, err)

	assert.Equal(t, 0, recorder.Body.Len())
	assert.Equal(t, http.StatusOK, recorder.Code)

	require.NoError(t, buffer.FlushTo(ctx.Writer))
	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.JSONEq(t, `{"id":"task_123"}`, recorder.Body.String())
}

func TestTaskResponseBufferCanBeDiscardedForErrorResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	buffer := newTaskResponseBuffer(ctx.Writer)

	_, err := buffer.WriteString(`{"id":"orphaned_task"}`)
	require.NoError(t, err)

	ctx.JSON(http.StatusInternalServerError, gin.H{"error": "insert failed"})

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "orphaned_task")
}
