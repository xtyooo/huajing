package xxuan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allowUploadFixtureURLs(t *testing.T) {
	t.Helper()
	settings := system_setting.GetFetchSetting()
	old := *settings
	settings.EnableSSRFProtection = false
	t.Cleanup(func() { *settings = old })
}

func TestUploadVideoSingleAndMultipartWire(t *testing.T) {
	allowUploadFixtureURLs(t)
	// net/http identifies the MP4 ftyp box; no extension-based MIME guessing.
	video := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{0, 1, 2, 3}, 12)...)
	for _, mode := range []string{"single", "multipart"} {
		t.Run(mode, func(t *testing.T) {
			const key = "fixture-platform-key"
			const ticket = "fixture-material-ticket"
			const partSize = 31
			var server *httptest.Server
			var uploaded []byte
			var completed bool
			var seenSource bool
			var receivedParts []completedUploadPart
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/source.mp4":
					seenSource = true
					assert.Equal(t, http.MethodGet, r.Method)
					assert.Empty(t, r.Header.Get("Authorization"))
					assert.Empty(t, r.Header.Get("X-Material-Ticket"))
					assert.Equal(t, "source-token", r.URL.Query().Get("sig"))
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = w.Write(video)
				case r.URL.Path == "/open/v1/uploads":
					assert.True(t, seenSource)
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "Bearer "+key, r.Header.Get("Authorization"))
					assert.Empty(t, r.Header.Get("X-Material-Ticket"))
					var request map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &request))
					assert.Equal(t, map[string]any{
						"file_name": "source.mp4", "size_bytes": float64(len(video)), "media_kind": "video", "mime_type": "video/mp4", "sha256": fmt.Sprintf("%x", sha256.Sum256(video)),
					}, request)
					session := uploadSession{ID: "upl_fixture", Mode: mode, Method: http.MethodPut, URL: server.URL + "/storage?sig=upload-token", Headers: map[string]string{"Content-Type": "video/mp4", "If-None-Match": "*", "X-Storage-Token": "storage-only"}, MaterialTicket: ticket, PartSize: partSize}
					if mode == "multipart" {
						for number := (len(video)-1)/partSize + 1; number >= 1; number-- {
							session.Parts = append(session.Parts, uploadPart{Number: number, URL: server.URL + "/part/" + strconv.Itoa(number) + "?sig=upload-token"})
						}
					}
					data, err := common.Marshal(session)
					require.NoError(t, err)
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write(data)
				case r.URL.Path == "/storage" || strings.HasPrefix(r.URL.Path, "/part/"):
					assert.Equal(t, http.MethodPut, r.Method)
					assert.Empty(t, r.Header.Get("Authorization"))
					assert.Empty(t, r.Header.Get("Cookie"))
					assert.Empty(t, r.Header.Get("X-Material-Ticket"))
					assert.Equal(t, "video/mp4", r.Header.Get("Content-Type"))
					assert.Equal(t, "*", r.Header.Get("If-None-Match"))
					assert.Equal(t, "storage-only", r.Header.Get("X-Storage-Token"))
					assert.Equal(t, "upload-token", r.URL.Query().Get("sig"))
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					assert.Equal(t, int64(len(body)), r.ContentLength)
					if mode == "multipart" {
						number, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/part/"))
						require.NoError(t, err)
						start := (number - 1) * partSize
						assert.Equal(t, video[start:min(start+partSize, len(video))], body)
						etag := fmt.Sprintf("\"part-%d-etag\"", number)
						receivedParts = append(receivedParts, completedUploadPart{Number: number, ETag: etag})
						w.Header().Set("ETag", etag)
					}
					uploaded = append(uploaded, body...)
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/open/v1/uploads/upl_fixture/complete":
					completed = true
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "Bearer "+key, r.Header.Get("Authorization"))
					assert.Equal(t, ticket, r.Header.Get("X-Material-Ticket"))
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					if mode == "single" {
						assert.JSONEq(t, `{}`, string(body))
					} else {
						expected, err := common.Marshal(map[string]any{"parts": receivedParts})
						require.NoError(t, err)
						assert.JSONEq(t, string(expected), string(body))
					}
					assert.NotContains(t, string(body), "media_metadata")
					_, _ = w.Write([]byte(`{"asset_id":"ast_verified","media_kind":"video","duration_ms":12000.25,"frame_rate_milli":null}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			result, err := uploadVideoWithClients(context.Background(), server.URL, key, server.URL+"/source.mp4?sig=source-token", server.Client(), server.Client())
			require.NoError(t, err)
			assert.Equal(t, uploadedVideo{AssetID: "ast_verified", DurationSeconds: 13}, result)
			assert.Equal(t, video, uploaded)
			assert.True(t, completed)
		})
	}
}

func TestUploadVideoRejectsUntrustedCompletionDuration(t *testing.T) {
	allowUploadFixtureURLs(t)
	for _, duration := range []string{"null", "0", "-1", "3600001", "1e100"} {
		t.Run(duration, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/source":
					w.Header().Set("Content-Type", "video/mp4")
					_, _ = w.Write([]byte{0, 1, 2, 3, 0, 1, 2, 3})
				case "/open/v1/uploads":
					body, err := common.Marshal(uploadSession{ID: "upl_test", Mode: "single", Method: "PUT", URL: server.URL + "/storage"})
					require.NoError(t, err)
					_, _ = w.Write(body)
				case "/storage":
					w.WriteHeader(http.StatusNoContent)
				case "/open/v1/uploads/upl_test/complete":
					_, _ = fmt.Fprintf(w, `{"asset_id":"ast_test","media_kind":"video","duration_ms":%s}`, duration)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			result, err := uploadVideoWithClients(context.Background(), server.URL, "fixture-key", server.URL+"/source", server.Client(), server.Client())
			require.ErrorContains(t, err, "trusted duration")
			assert.Empty(t, result.AssetID)
			assert.Zero(t, result.DurationSeconds)
		})
	}
}

func TestUploadVideoRejectsStorageRedirectAndMissingETag(t *testing.T) {
	allowUploadFixtureURLs(t)
	for _, scenario := range []string{"redirect", "missing-etag", "missing-part", "duplicate-part"} {
		t.Run(scenario, func(t *testing.T) {
			var server *httptest.Server
			storageCalls := 0
			completeCalls := 0
			redirectCalls := 0
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/source":
					w.Header().Set("Content-Type", "video/mp4")
					_, _ = w.Write([]byte{0, 1, 2, 3, 0, 1, 2, 3})
				case "/open/v1/uploads":
					session := uploadSession{ID: "upl_test", Mode: "multipart", PartSize: 4, Parts: []uploadPart{{Number: 1, URL: server.URL + "/storage"}, {Number: 2, URL: server.URL + "/storage"}}}
					if scenario == "missing-part" {
						session.Parts = session.Parts[:1]
					}
					if scenario == "duplicate-part" {
						session.Parts[1].Number = 1
					}
					body, err := common.Marshal(session)
					require.NoError(t, err)
					_, _ = w.Write(body)
				case "/storage":
					storageCalls++
					if scenario == "redirect" {
						w.Header().Set("Location", server.URL+"/leaked")
						w.WriteHeader(http.StatusTemporaryRedirect)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case "/leaked":
					redirectCalls++
				case "/open/v1/uploads/upl_test/complete":
					completeCalls++
				}
			}))
			defer server.Close()
			_, err := uploadVideoWithClients(context.Background(), server.URL, "fixture-key", server.URL+"/source", server.Client(), server.Client())
			require.Error(t, err)
			assert.Zero(t, completeCalls)
			assert.Zero(t, redirectCalls)
			if scenario == "missing-part" || scenario == "duplicate-part" {
				assert.Zero(t, storageCalls)
			} else {
				assert.Equal(t, 1, storageCalls)
			}
		})
	}
}

func TestUploadVideoBoundsUnknownLengthSourceBeforePlatformRequest(t *testing.T) {
	allowUploadFixtureURLs(t)
	oldLimit := constant.MaxMediaDownloadMB
	constant.MaxMediaDownloadMB = 1
	t.Cleanup(func() { constant.MaxMediaDownloadMB = oldLimit })
	apiCalls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/source" {
			apiCalls++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.(http.Flusher).Flush() // exercise actual byte counting, not Content-Length
		_, _ = w.Write(bytes.Repeat([]byte{0}, 1024*1024+1))
	}))
	defer server.Close()
	_, err := uploadVideoWithClients(context.Background(), server.URL, "fixture-key", server.URL+"/source", server.Client(), server.Client())
	require.ErrorContains(t, err, "gateway media download limit")
	assert.Zero(t, apiCalls)
}

func TestUploadVideoRejectsUnsafeURLWithoutNetwork(t *testing.T) {
	settings := system_setting.GetFetchSetting()
	old := *settings
	settings.EnableSSRFProtection = true
	settings.AllowPrivateIp = false
	t.Cleanup(func() { *settings = old })
	for _, source := range []string{"http://example.com/video.mp4", "https://user:secret@example.com/video.mp4", "file:///tmp/video.mp4", "https://127.0.0.1/video.mp4"} {
		t.Run(source, func(t *testing.T) {
			_, err := uploadVideoWithClients(context.Background(), "https://api.example.com", "fixture-key", source, &http.Client{}, &http.Client{})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret")
			assert.NotContains(t, err.Error(), source)
		})
	}
}
