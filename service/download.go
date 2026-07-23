package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// WorkerRequest Worker请求的数据结构
type WorkerRequest struct {
	URL     string            `json:"url"`
	Key     string            `json:"key"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// DoWorkerRequest 通过Worker发送请求
func DoWorkerRequest(req *WorkerRequest) (*http.Response, error) {
	return DoWorkerRequestWithContext(context.Background(), req)
}

func DoWorkerRequestWithContext(ctx context.Context, req *WorkerRequest) (*http.Response, error) {
	if !system_setting.EnableWorker() {
		return nil, fmt.Errorf("worker not enabled")
	}
	if !system_setting.WorkerAllowHttpImageRequestEnabled && !strings.HasPrefix(req.URL, "https") {
		return nil, fmt.Errorf("only support https url")
	}

	// SSRF防护：验证请求URL
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(req.URL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}

	workerUrl := system_setting.WorkerUrl
	if !strings.HasSuffix(workerUrl, "/") {
		workerUrl += "/"
	}

	// 序列化worker请求数据
	workerPayload, err := common.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker payload: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, workerUrl, bytes.NewBuffer(workerPayload))
	if err != nil {
		return nil, fmt.Errorf("new worker request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return GetHttpClient().Do(httpReq)
}

func DoDownloadRequest(originUrl string, reason ...string) (resp *http.Response, err error) {
	return DoDownloadRequestWithHeaders(originUrl, nil, reason...)
}

// DoDownloadRequestWithHeaders downloads a file from originUrl, optionally
// including custom HTTP headers (e.g. Authorization for authenticated
// endpoints such as Lingjing /download).
func DoDownloadRequestWithHeaders(originUrl string, headers map[string]string, reason ...string) (resp *http.Response, err error) {
	return DoDownloadRequestWithHeadersContext(context.Background(), originUrl, headers, reason...)
}

func DoDownloadRequestWithHeadersContext(ctx context.Context, originUrl string, headers map[string]string, reason ...string) (resp *http.Response, err error) {
	if system_setting.EnableWorker() {
		common.SysLog(fmt.Sprintf("downloading file from worker: %s, reason: %s", common.MaskSensitiveInfo(originUrl), strings.Join(reason, ", ")))
		req := &WorkerRequest{
			URL:     originUrl,
			Key:     system_setting.WorkerValidKey,
			Headers: headers,
		}
		return DoWorkerRequestWithContext(ctx, req)
	}
	if err := ValidateSSRFProtectedFetchURL(originUrl); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}

	common.SysLog(fmt.Sprintf("downloading from origin: %s, reason: %s", common.MaskSensitiveInfo(originUrl), strings.Join(reason, ", ")))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, originUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return GetSSRFProtectedHTTPClient().Do(req)
}
