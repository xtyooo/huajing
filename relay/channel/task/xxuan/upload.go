package xxuan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

type uploadedVideo struct {
	AssetID         string
	DurationSeconds int
}

type uploadSession struct {
	ID             string            `json:"upload_id"`
	Mode           string            `json:"upload_mode"`
	URL            string            `json:"upload_url"`
	Method         string            `json:"upload_method"`
	Headers        map[string]string `json:"upload_headers"`
	PartSize       int64             `json:"part_size_bytes"`
	Parts          []uploadPart      `json:"parts"`
	MaxBytes       int64             `json:"max_bytes"`
	MaterialTicket string            `json:"material_ticket"`
}

type uploadPart struct {
	Number int    `json:"part_number"`
	URL    string `json:"upload_url"`
}

type completedUploadPart struct {
	Number int    `json:"part_number"`
	ETag   string `json:"etag"`
}

// uploadVideo obtains duration from the provider's completion receipt, never
// from client-supplied metadata. Source and storage transfers deliberately use
// the SSRF-protected client; the operator's channel proxy applies to API calls.
func uploadVideo(ctx context.Context, baseURL, key, proxy, sourceURL string) (uploadedVideo, error) {
	apiClient, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return uploadedVideo{}, fmt.Errorf("invalid video upload API proxy")
	}
	return uploadVideoWithClients(ctx, baseURL, key, sourceURL, apiClient, service.GetSSRFProtectedHTTPClient())
}

func uploadVideoWithClients(ctx context.Context, baseURL, key, sourceURL string, apiClient, mediaClient *http.Client) (uploadedVideo, error) {
	if apiClient == nil || mediaClient == nil {
		return uploadedVideo{}, fmt.Errorf("video upload HTTP client is unavailable")
	}
	if err := validateUploadMediaURL(sourceURL); err != nil {
		return uploadedVideo{}, err
	}
	// Do not let a platform redirect carry its key or material ticket elsewhere.
	api := *apiClient
	api.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	api.Jar = nil
	media := *mediaClient
	media.Jar = nil
	media.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("video source exceeded three redirects")
		}
		return validateUploadMediaURL(req.URL.String())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return uploadedVideo{}, fmt.Errorf("invalid video source request")
	}
	response, err := media.Do(request)
	if err != nil {
		return uploadedVideo{}, fmt.Errorf("video source download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return uploadedVideo{}, fmt.Errorf("video source returned HTTP %d", response.StatusCode)
	}
	limitMB := constant.MaxMediaDownloadMB
	if limitMB <= 0 {
		limitMB = 512
	}
	if int64(limitMB) >= math.MaxInt64/(1024*1024) {
		return uploadedVideo{}, fmt.Errorf("invalid media download size limit")
	}
	limit := int64(limitMB) * 1024 * 1024
	if response.ContentLength > limit {
		return uploadedVideo{}, fmt.Errorf("video source exceeds gateway media download limit")
	}
	file, err := os.CreateTemp("", "new-api-xxuan-video-*")
	if err != nil {
		return uploadedVideo{}, fmt.Errorf("cannot prepare video upload file")
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, limit+1))
	if err != nil {
		return uploadedVideo{}, fmt.Errorf("video source could not be read completely")
	}
	if size == 0 || size > limit {
		return uploadedVideo{}, fmt.Errorf("video source is empty or exceeds gateway media download limit")
	}
	if response.ContentLength >= 0 && response.ContentLength != size {
		return uploadedVideo{}, fmt.Errorf("video source length does not match its response")
	}
	var prefix [512]byte
	n, err := file.ReadAt(prefix[:], 0)
	if err != nil && err != io.EOF {
		return uploadedVideo{}, fmt.Errorf("cannot inspect video source")
	}
	mediaType := http.DetectContentType(prefix[:n])
	// Several valid video containers have no net/http sniffer. For those,
	// retain the declared media type and let upload completion verify it.
	if mediaType == "application/octet-stream" {
		declaredType, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if parseErr == nil {
			mediaType = declaredType
		}
	}
	if !strings.HasPrefix(mediaType, "video/") {
		return uploadedVideo{}, fmt.Errorf("video source must have a video MIME type")
	}
	source, _ := url.Parse(sourceURL)
	fileName := path.Base(source.Path)
	if fileName == "." || fileName == "/" || fileName == "" || strings.Contains(fileName, "\\") || utf8.RuneCountInString(fileName) > 160 || strings.ContainsFunc(fileName, unicode.IsControl) {
		fileName = "source-video"
	}
	initBody := struct {
		FileName  string `json:"file_name"`
		Size      int64  `json:"size_bytes"`
		MediaKind string `json:"media_kind"`
		MIME      string `json:"mime_type"`
		SHA256    string `json:"sha256"`
	}{fileName, size, "video", mediaType, fmt.Sprintf("%x", hash.Sum(nil))}
	var session uploadSession
	if err := uploadJSON(ctx, &api, providerURL(baseURL, "/uploads"), key, "", initBody, &session); err != nil {
		return uploadedVideo{}, err
	}
	if session.ID == "" || strings.ContainsAny(session.ID, "/\\?#") || session.ID == "." || session.ID == ".." {
		return uploadedVideo{}, fmt.Errorf("upstream returned an invalid upload session")
	}
	if session.MaxBytes > 0 && size > session.MaxBytes {
		return uploadedVideo{}, fmt.Errorf("video exceeds upstream upload size limit")
	}
	storage := *mediaClient
	storage.Jar = nil
	storage.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	completeBody := struct {
		Parts []completedUploadPart `json:"parts,omitempty"`
	}{}
	switch session.Mode {
	case "single":
		if session.Method != http.MethodPut {
			return uploadedVideo{}, fmt.Errorf("unsupported upstream upload method")
		}
		if _, err := putUploadPart(ctx, &storage, session.URL, session.Headers, key, io.NewSectionReader(file, 0, size), size); err != nil {
			return uploadedVideo{}, err
		}
	case "multipart":
		if session.PartSize <= 0 {
			return uploadedVideo{}, fmt.Errorf("upstream returned an invalid upload part size")
		}
		partCount := (size-1)/session.PartSize + 1
		if int64(len(session.Parts)) != partCount {
			return uploadedVideo{}, fmt.Errorf("upstream upload parts do not cover the source")
		}
		ordered := make([]uploadPart, len(session.Parts))
		for _, part := range session.Parts {
			if part.Number < 1 || int64(part.Number) > partCount || ordered[part.Number-1].Number != 0 {
				return uploadedVideo{}, fmt.Errorf("upstream returned invalid upload part numbers")
			}
			ordered[part.Number-1] = part
		}
		for index, part := range ordered {
			offset := int64(index) * session.PartSize
			partSize := min(session.PartSize, size-offset)
			etag, err := putUploadPart(ctx, &storage, part.URL, session.Headers, key, io.NewSectionReader(file, offset, partSize), partSize)
			if err != nil {
				return uploadedVideo{}, err
			}
			if etag == "" {
				return uploadedVideo{}, fmt.Errorf("upstream storage did not return a multipart ETag")
			}
			completeBody.Parts = append(completeBody.Parts, completedUploadPart{Number: part.Number, ETag: etag})
		}
	default:
		return uploadedVideo{}, fmt.Errorf("unsupported upstream upload mode")
	}
	var receipt struct {
		AssetID    string   `json:"asset_id"`
		MediaKind  string   `json:"media_kind"`
		DurationMS *float64 `json:"duration_ms"`
	}
	completeURL := providerURL(baseURL, "/uploads/"+url.PathEscape(session.ID)+"/complete")
	if err := uploadJSON(ctx, &api, completeURL, key, session.MaterialTicket, completeBody, &receipt); err != nil {
		return uploadedVideo{}, err
	}
	if strings.TrimSpace(receipt.AssetID) == "" || receipt.MediaKind != "video" || receipt.DurationMS == nil ||
		math.IsNaN(*receipt.DurationMS) || math.IsInf(*receipt.DurationMS, 0) || *receipt.DurationMS <= 0 ||
		*receipt.DurationMS > float64(relaycommon.MaxTaskDurationSeconds)*1000 {
		return uploadedVideo{}, fmt.Errorf("upstream upload lacks a valid video asset and trusted duration")
	}
	return uploadedVideo{AssetID: receipt.AssetID, DurationSeconds: int(math.Ceil(*receipt.DurationMS / 1000))}, nil
}

func validateUploadMediaURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(raw) > 4096 {
		return fmt.Errorf("video media URL must be HTTPS without credentials")
	}
	if err := service.ValidateSSRFProtectedFetchURL(raw); err != nil {
		return fmt.Errorf("video media URL is blocked by fetch policy")
	}
	return nil
}

func uploadJSON(ctx context.Context, client *http.Client, endpoint, key, ticket string, payload, result any) error {
	data, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cannot encode video upload request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid video upload endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if ticket != "" {
		req.Header.Set("X-Material-Ticket", ticket)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("video upload API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("video upload API returned HTTP %d", resp.StatusCode)
	}
	const responseLimit = 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil || len(body) > responseLimit {
		return fmt.Errorf("video upload API response is incomplete or too large")
	}
	if err := common.Unmarshal(body, result); err != nil {
		return fmt.Errorf("video upload API returned invalid JSON")
	}
	return nil
}

func putUploadPart(ctx context.Context, client *http.Client, target string, headers map[string]string, platformKey string, data io.Reader, size int64) (string, error) {
	if err := validateUploadMediaURL(target); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, data)
	if err != nil {
		return "", fmt.Errorf("invalid upstream storage upload request")
	}
	req.ContentLength = size
	for name, value := range headers {
		// Never reflect the platform credential into a storage request, even if
		// an invalid provider response accidentally included it as an upload header.
		if (platformKey != "" && (value == platformKey || value == "Bearer "+platformKey)) || strings.EqualFold(name, "X-Material-Ticket") {
			return "", fmt.Errorf("upstream storage headers contain platform credentials")
		}
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("video storage upload failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("video storage upload returned HTTP %d", resp.StatusCode)
	}
	return resp.Header.Get("ETag"), nil
}
