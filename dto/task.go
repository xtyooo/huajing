package dto

type TaskError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	StatusCode int    `json:"-"`
	LocalError bool   `json:"-"`
	Error      error  `json:"-"`
}

type TaskData interface {
	SunoDataResponse | []SunoDataResponse | string | any
}

const TaskSuccessCode = "success"

type TaskResponse[T TaskData] struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func (t *TaskResponse[T]) IsSuccess() bool {
	return t.Code == TaskSuccessCode
}

type TaskDto struct {
	ID              int64  `json:"id"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	TaskID          string `json:"task_id"`
	Platform        string `json:"platform"`
	UserId          int    `json:"user_id"`
	Group           string `json:"group"`
	ChannelId       int    `json:"channel_id"`
	Quota           int    `json:"quota"`
	Action          string `json:"action"`
	Status          string `json:"status"`
	FailReason      string `json:"fail_reason"`
	SubmitTime      int64  `json:"submit_time"`
	StartTime       int64  `json:"start_time"`
	FinishTime      int64  `json:"finish_time"`
	Progress        string `json:"progress"`
	Properties      any    `json:"properties"`
	Username        string `json:"username,omitempty"`
	MediaURL        string `json:"media_url,omitempty"`
	VideoURL        string `json:"video_url,omitempty"`
	ResultURL       string `json:"result_url,omitempty"`
	MediaStatus     int    `json:"media_status,omitempty"`
	MediaStatusDesc string `json:"media_status_desc,omitempty"`
	MediaStartTime  int64  `json:"media_start_time,omitempty"`
	MediaFinishTime int64  `json:"media_finish_time,omitempty"`
}

type FetchReq struct {
	IDs []string `json:"ids"`
}
