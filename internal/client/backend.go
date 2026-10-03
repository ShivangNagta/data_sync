package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type SyncBackend interface {
	GetSyncPlan(context.Context, []*FileState) ([]*SyncAction, error)
	Upload(context.Context, *UploadFileMeta, []byte) (*UploadFileResponse, error)
	Download(context.Context, *SyncAction) (io.ReadCloser, error)
	Delete(context.Context, string, string) error
	EventsURL() string
}

type HTTPBackend struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (b *HTTPBackend) GetSyncPlan(ctx context.Context, files []*FileState) ([]*SyncAction, error) {
	var response struct {
		Actions []httpAction `json:"actions"`
	}
	if files == nil {
		files = []*FileState{}
	}
	if err := b.doJSON(ctx, http.MethodPost, "/sync/plan", struct {
		LocalFiles []*FileState `json:"local_files"`
	}{files}, &response); err != nil {
		return nil, err
	}
	actions := make([]*SyncAction, 0, len(response.Actions))
	for _, action := range response.Actions {
		if action.Action == "conflict" {
			return nil, fmt.Errorf("conflict: server has %s", action.Hash)
		}
		actions = append(actions, action.proto())
	}
	return actions, nil
}

func (b *HTTPBackend) Upload(ctx context.Context, meta *UploadFileMeta, content []byte) (*UploadFileResponse, error) {
	if err := b.doBytes(ctx, http.MethodPut, "/files/"+meta.Hash, content, nil); err != nil {
		return nil, err
	}
	var response UploadFileResponse
	err := b.doJSON(ctx, http.MethodPost, "/sync/commit", httpCommit{
		Operation:    "upload",
		Path:         meta.Path,
		Hash:         meta.Hash,
		Size:         meta.Size,
		LastSeenHash: meta.LastSeenHash,
	}, &response)
	return &response, err
}

func (b *HTTPBackend) Download(ctx context.Context, action *SyncAction) (io.ReadCloser, error) {
	req, err := b.request(ctx, http.MethodGet, "/files/"+action.Hash, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, responseError(resp)
	}
	return resp.Body, nil
}

func (b *HTTPBackend) Delete(ctx context.Context, path, lastSeenHash string) error {
	var response UploadFileResponse
	err := b.doJSON(ctx, http.MethodPost, "/sync/commit", httpCommit{
		Operation:    "delete",
		Path:         path,
		LastSeenHash: lastSeenHash,
	}, &response)
	if err != nil {
		return err
	}
	if response.Conflict {
		return fmt.Errorf("conflict: server has %s", response.CurrentHash)
	}
	return nil
}

func (b *HTTPBackend) EventsURL() string { return strings.TrimRight(b.BaseURL, "/") + "/v2" }

type httpAction struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Hash   string `json:"hash"`
	Size   int64  `json:"size"`
}

func (a httpAction) proto() *SyncAction {
	action := map[string]ActionType{
		"upload": ActionUpload, "download": ActionDownload,
		"delete": ActionDelete,
	}[a.Action]
	return &SyncAction{Path: a.Path, Action: action, Hash: a.Hash, Size: a.Size}
}

type httpCommit struct {
	Operation    string `json:"operation"`
	Path         string `json:"path"`
	Hash         string `json:"hash"`
	Size         int64  `json:"size"`
	LastSeenHash string `json:"last_seen_hash"`
}

func (b *HTTPBackend) doJSON(ctx context.Context, method, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return b.doBytes(ctx, method, path, data, func(resp *http.Response) error {
		return json.NewDecoder(resp.Body).Decode(out)
	})
}

func (b *HTTPBackend) doBytes(ctx context.Context, method, path string, body []byte, decode func(*http.Response) error) error {
	req, err := b.request(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	if decode != nil {
		return decode(resp)
	}
	return nil
}

func (b *HTTPBackend) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(b.BaseURL, "/")+"/v2"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return req, nil
}

func (b *HTTPBackend) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return http.DefaultClient
}

func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("http status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
}
