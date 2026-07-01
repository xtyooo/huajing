package mimo

type MimoSubmitReq struct {
	Prompt      string          `json:"prompt"`
	Duration    int             `json:"duration,omitempty"`
	AspectRatio string          `json:"aspectRatio,omitempty"`
	ModelName   string          `json:"modelName,omitempty"`
	Images      []MimoImageItem `json:"images,omitempty"`
}

type MimoImageItem struct {
	ImageUri string `json:"imageUri,omitempty"`
	ImageUrl string `json:"imageUrl,omitempty"`
}

type MimoSubmitResp struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data MimoSubmitData `json:"data"`
}

type MimoSubmitData struct {
	ID string `json:"id"`
}

type MimoBatchReq struct {
	TaskIDs []string `json:"taskIds"`
}

type MimoBatchResp struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data []MimoTaskItem `json:"data"`
}

type MimoTaskItem struct {
	TaskID   string        `json:"taskId"`
	Status   int           `json:"status"`
	VideoURL string        `json:"videoUrl"`
	CoverURL string        `json:"coverUrl"`
	SubTasks []MimoSubTask `json:"subTasks,omitempty"`
}

type MimoSubTask struct {
	Status    int            `json:"status"`
	VideoInfo *MimoVideoInfo `json:"videoInfo,omitempty"`
}

type MimoVideoInfo struct {
	VideoURL  string  `json:"videoUrl"`
	CoverURL  string  `json:"coverUrl"`
	Duration  float64 `json:"duration"`
}
