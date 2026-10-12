package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

func newNativeTaskBridge(native channel.LegacyTaskAdaptor) channel.TaskAdaptor {
	bridge := channel.NewLegacyTaskBridge(native)
	if native.GetChannelName() == "suno" {
		return &nativeSunoBridge{bridge}
	}
	return bridge
}

type nativeSunoBridge struct{ *channel.LegacyTaskBridge }

func (*nativeSunoBridge) FetchMode() string { return "batch" }

func (b *nativeSunoBridge) FetchBatchTasks(baseURL, key string, tasks []*model.Task, proxy string) (*http.Response, error) {
	groups := make(map[string][]string)
	keys := make([]string, 0)
	for _, task := range tasks {
		selectedKey := task.PrivateData.Key
		if selectedKey == "" {
			selectedKey = key
		}
		if _, exists := groups[selectedKey]; !exists {
			keys = append(keys, selectedKey)
		}
		groups[selectedKey] = append(groups[selectedKey], task.GetUpstreamTaskID())
	}
	items := make([]dto.SunoDataResponse, 0, len(tasks))
	for _, selectedKey := range keys {
		resp, err := b.LegacyTaskAdaptor.FetchTask(baseURL, selectedKey, map[string]any{"ids": groups[selectedKey]}, proxy)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return resp, nil
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		var result dto.TaskResponse[[]dto.SunoDataResponse]
		if err := common.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		if !result.IsSuccess() {
			return nil, fmt.Errorf("native suno query failed: %s", result.Code)
		}
		items = append(items, result.Data...)
	}
	body, err := common.Marshal(dto.TaskResponse[[]dto.SunoDataResponse]{Code: dto.TaskSuccessCode, Data: items})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func (*nativeSunoBridge) ParseBatchResult(_ []*model.Task, _ *http.Response, body []byte) (map[string]*service.BatchTaskResult, error) {
	var response dto.TaskResponse[[]dto.SunoDataResponse]
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if !response.IsSuccess() {
		return nil, fmt.Errorf("native suno query failed: %s", response.Code)
	}
	results := make(map[string]*service.BatchTaskResult, len(response.Data))
	for _, task := range response.Data {
		results[task.TaskID] = &service.BatchTaskResult{
			TaskInfo: relaycommon.TaskInfo{TaskID: task.TaskID, Status: task.Status, Reason: task.FailReason},
			Action:   task.Action, SubmitTime: task.SubmitTime, StartTime: task.StartTime, FinishTime: task.FinishTime, Data: task.Data,
		}
	}
	return results, nil
}
