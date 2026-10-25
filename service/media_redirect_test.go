package service

import (
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mediaRedirectTransport func(*http.Request) (*http.Response, error)

func (f mediaRedirectTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMedia307RedirectCredentials(t *testing.T) {
	for _, destination := range []string{"https://provider.example.test/output", "https://cdn.provider.example.test/output", "https://cdn.example.test/output", "http://provider.example.test/output", "https://provider.example.test:8443/output"} {
		t.Run(destination, func(t *testing.T) {
			checked := false
			base := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { checked = true; return nil }}
			base.Transport = mediaRedirectTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/content" {
					require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
					return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{destination}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				want := ""
				if destination == "https://provider.example.test/output" {
					want = "Bearer secret"
				}
				require.Equal(t, want, r.Header.Get("Authorization"))
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("mp4")), Request: r}, nil
			})
			req, err := http.NewRequest("GET", "https://provider.example.test/content", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer secret")
			resp, err := SameOriginMediaClient(base).Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, 200, resp.StatusCode)
			require.True(t, checked)
		})
	}
}
