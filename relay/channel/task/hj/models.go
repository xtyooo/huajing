package hj

type HjRunRequest struct {
	Model string         `json:"model,omitempty"`
	Graph map[string]any `json:"graph"`
}

type HjUpstreamBody struct {
	Graph map[string]any `json:"graph"`
}

type HjSubmitResponse struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	Message   string `json:"message,omitempty"`
}

type HjQueryResponse struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	Progress  string `json:"progress,omitempty"`
	ResultURL string `json:"result_url,omitempty"`
	ErrorMsg  string `json:"error_msg,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}
