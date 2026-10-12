package channel

import (
	"bytes"
	"fmt"
	"net/http"

	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// LegacyTaskBridge keeps native provider protocols behind the upstream host
// interface. Native estimates already use absolute quota, never plugin USD.
type LegacyTaskBridge struct{ LegacyTaskAdaptor }

func NewLegacyTaskBridge(native LegacyTaskAdaptor) *LegacyTaskBridge {
	return &LegacyTaskBridge{native}
}

type LegacyTaskClientResponse struct {
	Body       []byte
	Headers    http.Header
	StatusCode int
}

func (b *LegacyTaskBridge) ParseResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*TaskSubmitResponse, *taskdto.TaskError) {
	original := c.Writer
	capture := &legacyTaskResponseWriter{ResponseWriter: original, headers: original.Header().Clone(), status: http.StatusOK}
	c.Writer = capture
	defer func() { c.Writer = original }()
	id, data, taskErr := b.LegacyTaskAdaptor.DoResponse(c, resp, info)
	if taskErr != nil {
		// A successful upstream status may already have created paid work.
		// A malformed accepted response cannot authorize another submission.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			taskErr.NoRetry = true
		}
		return nil, taskErr
	}
	return &TaskSubmitResponse{UpstreamTaskID: id, TaskData: data, ClientResponse: &LegacyTaskClientResponse{Body: capture.body.Bytes(), Headers: capture.headers, StatusCode: capture.status}}, nil
}

func (b *LegacyTaskBridge) FetchTask(baseURL, key string, task *model.Task, proxy string) (*http.Response, error) {
	return b.LegacyTaskAdaptor.FetchTask(baseURL, key, map[string]any{"task_id": task.GetUpstreamTaskID(), "action": task.Action, "model": task.Properties.UpstreamModelName}, proxy)
}

func (b *LegacyTaskBridge) ParseTaskResult(_ *model.Task, _ *http.Response, body []byte) (*relaycommon.TaskInfo, error) {
	return b.LegacyTaskAdaptor.ParseTaskResult(body)
}

func (b *LegacyTaskBridge) SupportsImageSizePricing() bool {
	provider, ok := b.LegacyTaskAdaptor.(TaskImageSizePricingAdaptor)
	return ok && provider.SupportsImageSizePricing()
}

func (b *LegacyTaskBridge) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	provider, ok := b.LegacyTaskAdaptor.(OpenAIVideoConverter)
	if !ok {
		return nil, fmt.Errorf("native platform %s has no video converter", task.Platform)
	}
	return provider.ConvertToOpenAIVideo(task)
}

// No method may flush the legacy success response across the durable barrier.
type legacyTaskResponseWriter struct {
	gin.ResponseWriter
	body    bytes.Buffer
	headers http.Header
	status  int
	wrote   bool
}

func (w *legacyTaskResponseWriter) Header() http.Header { return w.headers }
func (w *legacyTaskResponseWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
	}
}
func (w *legacyTaskResponseWriter) WriteHeaderNow() { w.wrote = true }
func (w *legacyTaskResponseWriter) Write(body []byte) (int, error) {
	w.wrote = true
	return w.body.Write(body)
}
func (w *legacyTaskResponseWriter) WriteString(body string) (int, error) {
	w.wrote = true
	return w.body.WriteString(body)
}
func (w *legacyTaskResponseWriter) Status() int { return w.status }
func (w *legacyTaskResponseWriter) Size() int {
	if !w.wrote {
		return -1
	}
	return w.body.Len()
}
func (w *legacyTaskResponseWriter) Written() bool { return w.wrote }
func (w *legacyTaskResponseWriter) Flush()        { w.wrote = true }

func (*LegacyTaskBridge) IsNativeTaskAdaptor() bool { return true }
