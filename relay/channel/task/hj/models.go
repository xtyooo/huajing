package hj

type HjRunRequest struct {
	Model string         `json:"model,omitempty"`
	Graph map[string]any `json:"graph"`
}

type HjUpstreamBody struct {
	Graph map[string]any `json:"graph"`
}

type HjSubmitResponse struct {
	OK     bool   `json:"ok"`
	TaskID string `json:"taskId"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

type HjTaskInfo struct {
	TaskID    string `json:"taskId"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Error     string `json:"error"`
	LastError string `json:"lastError"`
	VideoURL  string `json:"videoUrl"`
}

type HjQueryResponse struct {
	OK   bool       `json:"ok"`
	Task HjTaskInfo `json:"task"`
}
