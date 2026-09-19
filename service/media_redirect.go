package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// SameOriginMediaClient preserves redirect validation but removes credentials
// whenever a media redirect changes origin, including subdomains and ports.
// Clone the client so shared transport clients are never mutated.
func SameOriginMediaClient(base *http.Client) *http.Client {
	client := *base
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if len(via) > 0 && (!strings.EqualFold(req.URL.Scheme, via[0].URL.Scheme) || !strings.EqualFold(req.URL.Host, via[0].URL.Host)) {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
			req.Header.Del("Proxy-Authorization")
		}
		if base.CheckRedirect != nil {
			return base.CheckRedirect(req, via)
		}
		return nil
	}
	return &client
}

// downloadChannelMedia performs authenticated content downloads locally so the
// redirect credential policy cannot be delegated to an unverified worker.
func downloadChannelMedia(ctx context.Context, rawURL string, headers map[string]string) (*http.Response, error) {
	if err := ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return SameOriginMediaClient(GetSSRFProtectedHTTPClient()).Do(req)
}
