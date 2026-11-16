package put

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/runware/runware-cli/internal/buildinfo"
)

const (
	maxAnswerBytes = 64 << 10
	maxAckBytes    = 1024
)

// HTTPTransport sends parts over HTTPS with the part token as a bearer token and never follows a redirect.
type HTTPTransport struct {
	client *http.Client
}

// NewHTTPTransport returns a transport on client, or on an HTTP/1.1 client that keeps a connection for each part in flight when client is nil.
func NewHTTPTransport(client *http.Client) *HTTPTransport {
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				// HTTP/2 would carry the parallel parts on one connection.
				ForceAttemptHTTP2:   false,
				MaxIdleConnsPerHost: maxParallelism,
				TLSHandshakeTimeout: 15 * time.Second,
				// The endpoint answers once storage holds the part; a stalled answer is retried.
				ResponseHeaderTimeout: 2 * time.Minute,
				IdleConnTimeout:       90 * time.Second,
			},
		}
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPTransport{client: &c}
}

type ackBody struct {
	PartNumber int    `json:"partNumber"`
	Ack        string `json:"ack"`
}

// PutPart sends the part with the token in the Authorization header; a refusal is a *ProblemError.
func (t *HTTPTransport) PutPart(ctx context.Context, target Target, body io.Reader, length int64) (Secret, error) {
	u, err := url.Parse(target.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("%w: part %d has no https upload address", ErrProtocol, target.PartNumber)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), body)
	if err != nil {
		return "", fmt.Errorf("%w: part %d request is malformed", ErrProtocol, target.PartNumber)
	}
	req.ContentLength = length
	req.Header.Set("Authorization", "Bearer "+string(target.Token))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	resp, err := t.client.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return "", fmt.Errorf("send part %d: %w", target.PartNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	if err != nil {
		return "", fmt.Errorf("read the answer for part %d: %w", target.PartNumber, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", parseProblem(resp.StatusCode, answer)
	}
	var a ackBody
	if json.Unmarshal(answer, &a) != nil || a.PartNumber != target.PartNumber || a.Ack == "" || len(a.Ack) > maxAckBytes {
		return "", fmt.Errorf("%w: part %d was not acknowledged as sent", ErrProtocol, target.PartNumber)
	}
	return Secret(a.Ack), nil
}
