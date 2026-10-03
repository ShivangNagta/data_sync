package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type FileEvent struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

func (e *SyncEngine) ListenEvents(ctx context.Context, httpURL string, onChange func()) {
	for {
		if ctx.Err() != nil {
			return
		}

		err := e.connectEvents(ctx, httpURL, onChange)
		if err != nil {
			fmt.Printf("events: %v, reconnecting in 5s\n", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (e *SyncEngine) connectEvents(ctx context.Context, httpURL string, onChange func()) error {
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL+"/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.client.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("events: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 6 || line[:5] != "data:" {
			continue
		}
		data := line[5:]
		if len(data) > 0 && data[0] == ' ' {
			data = data[1:]
		}
		if data == "connected" {
			continue
		}

		var event FileEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Type == "change" {
			onChange()
		}
	}

	return scanner.Err()
}
