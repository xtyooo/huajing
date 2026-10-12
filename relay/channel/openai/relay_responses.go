package openai

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if info != nil && info.PriceData.UsePrice {
		// 固定价图片请求使用上游实际成功数量覆盖请求 n；该兼容信息不参与通用工具附加费计数。
		if imageCallCount := setResponsesImageGenerationContext(c, &responsesResponse); imageCallCount > 0 {
			info.UpdateImageCount(imageCallCount)
		}
	}
	info.ObserveResponseModel(responsesResponse.Model)
	responseBody = rewriteSGLangResponsesCreatedAt(info, responseBody, "created_at", responsesResponse.CreatedAt)

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := &dto.Usage{}
	service.ApplyResponsesUsage(usage, responsesResponse.Usage)
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}
	info.ApplyVendorToolUsage(responseBody)

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	for i := range responsesResponse.Output {
		imageCounter.Observe(&responsesResponse.Output[i], &i)
	}
	imageCounter.Commit(info)

	return usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, common.LogText("invalid response or response body"))
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var completedImageCalls int64
	var finalImageCalls int64
	var finalResponseSeen bool
	var completedImageQuality, completedImageSize string
	accumulator := service.NewResponsesUsageAccumulator(info)

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal stream response: %s", err.Error()))
			sr.Error(err)
			return
		}
		if streamResponse.Response != nil {
			data = string(rewriteSGLangResponsesCreatedAt(info, []byte(data), "response.created_at", streamResponse.Response.CreatedAt))
		}
		sendResponsesStreamData(c, streamResponse, data)
		accumulator.Observe(&streamResponse, common.StringToByteSlice(data))
		if info.PriceData.UsePrice {
			if streamResponse.Response != nil && (streamResponse.Type == "response.completed" || streamResponse.Type == "response.done" || streamResponse.Type == "response.incomplete") {
				finalResponseSeen = true
				finalImageCalls = streamResponse.Response.ImageGenerationCallCount()
				setResponsesImageGenerationContext(c, streamResponse.Response)
			}
			if streamResponse.Type == dto.ResponsesOutputTypeItemDone && streamResponse.Item != nil && streamResponse.Item.IsSuccessfulImageGenerationCall() {
				completedImageCalls++
				if completedImageQuality == "" {
					completedImageQuality = streamResponse.Item.Quality
				}
				if completedImageSize == "" {
					completedImageSize = streamResponse.Item.Size
				}
			}
		}
	})

	if info != nil && info.PriceData.UsePrice {
		if finalResponseSeen {
			info.UpdateImageCount(finalImageCalls)
		} else if info.StreamStatus != nil {
			upstreamFinished := info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone ||
				info.StreamStatus.EndReason == relaycommon.StreamEndReasonEOF
			requestedN := 1.0
			if n, ok := info.PriceData.OtherRatios()["n"]; ok {
				requestedN = n
			}
			if upstreamFinished || float64(completedImageCalls) > requestedN {
				info.UpdateImageCount(completedImageCalls)
			}
		}
		if !finalResponseSeen && completedImageCalls > 0 {
			c.Set("image_generation_call", true)
			c.Set("image_generation_call_count", int(completedImageCalls))
			c.Set("image_generation_call_quality", completedImageQuality)
			c.Set("image_generation_call_size", completedImageSize)
		}
	}

	common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, info.StreamStatus)
	info.StreamStatus.RequireTerminal()
	return accumulator.Finish(), nil
}

func rewriteSGLangResponsesCreatedAt(info *relaycommon.RelayInfo, payload []byte, path string, createdAt dto.IntValue) []byte {
	if info.GetChannelType() != constant.ChannelTypeSGLang {
		return payload
	}
	if !gjson.GetBytes(payload, path).Exists() {
		return payload
	}
	patched, err := sjson.SetBytes(payload, path, int(createdAt))
	if err != nil {
		return payload
	}
	return patched
}

func setResponsesImageGenerationContext(c *gin.Context, response *dto.OpenAIResponsesResponse) int64 {
	if c == nil || response == nil {
		return 0
	}
	count := response.ImageGenerationCallCount()
	if count <= 0 {
		return 0
	}
	c.Set("image_generation_call", true)
	c.Set("image_generation_call_count", int(count))
	c.Set("image_generation_call_quality", response.GetQuality())
	c.Set("image_generation_call_size", response.GetSize())
	return count
}
