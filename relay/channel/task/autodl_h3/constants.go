package autodl_h3

const (
	ChannelName         = "autodl_h3"
	TextWorkflowID      = "minimax_h3_lightx2v_no_pic"
	ReferenceWorkflowID = "minimax_h3_image_audio_to_video_v2_15s"
	FirstLastWorkflowID = "minimax_h3_b99_002"
	workflowPath        = "/api/v1/comfyui/comfyui_workflow"
)

var ModelList = []string{
	TextWorkflowID,
	ReferenceWorkflowID,
	FirstLastWorkflowID,
}
