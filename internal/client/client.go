package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const basePath = "/api/alertmanager/grafana/api/v2"

// ErrNotFound is returned when a silence does not exist in the API.
var ErrNotFound = errors.New("silence not found")

// errAPI is the base error for non-successful API responses.
var errAPI = errors.New("API error")

// Client wraps net/http to interact with the Grafana Alertmanager silence API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	authHeader string
}

// New creates a new Client. The auth parameter is either a Bearer token or
// "user:pass" for Basic authentication (detected by the presence of a colon).
func New(baseURL, auth string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")

	authHeader := "Bearer " + auth
	if strings.Contains(auth, ":") {
		authHeader = "Basic " + basicAuth(auth)
	}

	return &Client{
		baseURL:    baseURL,
		httpClient: http.DefaultClient,
		authHeader: authHeader,
	}
}

func basicAuth(auth string) string {
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// PostSilences creates or updates a silence and returns its ID.
// To create a new silence, leave ID empty. To update, set ID to the existing silence's ID.
func (c *Client) PostSilences(
	ctx context.Context,
	silence PostableSilence,
) (string, error) {
	body, err := json.Marshal(silence)
	if err != nil {
		return "", fmt.Errorf("marshaling silence: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.baseURL+basePath+"/silences",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusAccepted {
		return "", readError(resp)
	}

	var result PostSilencesOKBody

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return result.SilenceID, nil
}

// GetSilence retrieves a silence by ID. Returns ErrNotFound if the silence does not exist.
func (c *Client) GetSilence(ctx context.Context, silenceID string) (*GettableSilence, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		c.baseURL+basePath+"/silence/"+silenceID, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var silence GettableSilence

	err = json.NewDecoder(resp.Body).Decode(&silence)
	if err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &silence, nil
}

// DeleteSilence expires a silence by ID. Returns nil if already expired (404).
func (c *Client) DeleteSilence(ctx context.Context, silenceID string) error {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodDelete,
		c.baseURL+basePath+"/silence/"+silenceID, nil,
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}

	return nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}

func readError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	return fmt.Errorf("%w: status %d: %s", errAPI, resp.StatusCode, string(body))
}
