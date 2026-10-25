package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	imageN := uint(1)
	if request.N != nil {
		imageN = *request.N
	}
	imageAction := constant.TaskActionImageGenerate
	if info.RelayMode == relayconstant.RelayModeImagesEdits {
		imageAction = constant.TaskActionImageEdit
	}

	var requestBody io.Reader

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.NewReplayableBodyReader(storage)
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err := common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}

			logger.LogDebug(c, "image request body: %s", jsonData)
			body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			defer closer.Close()
			jsonData = nil
			requestBody = body
		}
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	cacheImages := !info.IsStream
	imageCacheSession := service.ImageCacheSessionFromContext(c)
	cacheSessionManagedByController := imageCacheSession != nil
	if cacheImages && imageCacheSession == nil {
		imageCacheSession = service.NewImageCacheSession(service.ImageCacheParams{
			UserID:            info.UserId,
			ChannelID:         info.ChannelId,
			Quota:             info.PriceData.QuotaToPreConsume,
			Group:             info.UsingGroup,
			Action:            imageAction,
			Prompt:            request.Prompt,
			ModelName:         info.OriginModelName,
			UpstreamModelName: info.UpstreamModelName,
			CreatedAt:         info.StartTime.Unix(),
		})
	}
	var responseBuffer *imageResponseBuffer
	if cacheImages {
		if err := imageCacheSession.Begin(); err != nil {
			logger.LogError(c, fmt.Sprintf("create image task failed: %v", err))
			return types.NewError(fmt.Errorf("failed to create image task"), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if err := imageCacheSession.SetAttempt(info.ChannelId, info.UpstreamModelName); err != nil {
			logger.LogError(c, fmt.Sprintf("update image task attempt failed: %v", err))
			return types.NewError(fmt.Errorf("failed to update image task"), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if !cacheSessionManagedByController {
			defer imageCacheSession.Fail(fmt.Errorf("image response processing failed"))
		}
		responseBuffer = newImageResponseBuffer(c.Writer)
		c.Writer = responseBuffer
		defer func() { c.Writer = responseBuffer.ResponseWriter }()
	}
	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if responseBuffer != nil {
		c.Writer = responseBuffer.ResponseWriter
	}
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	var cachedBody []byte
	if responseBuffer != nil {
		cachedBody, err = imageCacheSession.CacheResponse(responseBuffer.body.Bytes())
		if err != nil {
			c.Writer.Header().Del("Content-Length")
			logger.LogError(c, fmt.Sprintf("cache image response failed: %v", err))
			return types.NewError(fmt.Errorf("failed to cache generated image"), types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	quality := request.Quality
	if quality == "" {
		quality = "standard"
	}

	var logContent []string

	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)
	if responseBuffer != nil {
		if err := responseBuffer.FlushTo(cachedBody); err != nil {
			logger.LogError(c, fmt.Sprintf("write cached image response failed: %v", err))
		}
	}
	return nil
}

type imageResponseBuffer struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
	wrote  bool
}

func newImageResponseBuffer(writer gin.ResponseWriter) *imageResponseBuffer {
	return &imageResponseBuffer{ResponseWriter: writer, status: http.StatusOK}
}

func (w *imageResponseBuffer) WriteHeader(code int) {
	if w.wrote || code <= 0 {
		return
	}
	w.status = code
	w.wrote = true
}

func (w *imageResponseBuffer) WriteHeaderNow() {
	if !w.wrote {
		w.WriteHeader(w.status)
	}
}

func (w *imageResponseBuffer) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	return w.body.Write(data)
}

func (w *imageResponseBuffer) WriteString(data string) (int, error) {
	w.WriteHeaderNow()
	return w.body.WriteString(data)
}

func (w *imageResponseBuffer) Status() int { return w.status }

func (w *imageResponseBuffer) Size() int {
	if !w.wrote {
		return -1
	}
	return w.body.Len()
}

func (w *imageResponseBuffer) Written() bool { return w.wrote }

func (w *imageResponseBuffer) Flush() { w.WriteHeaderNow() }

func (w *imageResponseBuffer) FlushTo(body []byte) error {
	w.ResponseWriter.Header().Del("Transfer-Encoding")
	w.ResponseWriter.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.ResponseWriter.WriteHeader(w.status)
	_, err := w.ResponseWriter.Write(body)
	return err
}
