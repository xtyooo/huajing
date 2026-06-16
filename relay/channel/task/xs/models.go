package xs

type XsVideoRequest struct {
	Model          string        `json:"model"`
	Mode           string        `json:"mode"`
	Prompt         string        `json:"prompt"`
	Resolution     string        `json:"resolution,omitempty"`
	Ratio          string        `json:"ratio,omitempty"`
	Duration       int           `json:"duration,omitempty"`
	ImageUrls      []interface{} `json:"image_urls,omitempty"`
	VideoUrls      []interface{} `json:"video_urls,omitempty"`
	VideoDurations []int         `json:"video_durations,omitempty"`
	AudioUrls      []interface{} `json:"audio_urls,omitempty"`
	RefImages      []interface{} `json:"refImages,omitempty"`
}

type XsSubmitResponse struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Object    string `json:"object"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Seconds   int    `json:"seconds"`
	CreatedAt int64  `json:"created_at"`
}

type XsQueryResponse struct {
	TaskID     string      `json:"task_id"`
	Model      string      `json:"model"`
	Mode       string      `json:"mode"`
	Status     string      `json:"status"`
	Progress   int         `json:"progress"`
	Ratio      string      `json:"ratio"`
	Duration   int         `json:"duration"`
	Resolution string      `json:"resolution"`
	ResultURL  string      `json:"result_url"`
	FailReason interface{} `json:"fail_reason"`
	ErrorMsg   string      `json:"error_msg,omitempty"`
	CreatedAt  int64       `json:"created_at"`
	UpdatedAt  int64       `json:"updated_at"`
}
