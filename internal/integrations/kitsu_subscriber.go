package integrations

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zishang520/engine.io/v2/events"
	"github.com/zishang520/socket.io-client-go/socket"
)

// KitsuSocketSubscriber connects to a Zou socket.io endpoint, listens for
// task:* events, and pushes them onto the listener's event channel.
type KitsuSocketSubscriber struct {
	token  string
	apiUrl string
}

// NewKitsuSocketSubscriber builds a subscriber bound to a service account
// token and the Kitsu API URL (the same URL passed to Authenticate).
func NewKitsuSocketSubscriber(token, apiUrl string) KitsuEventSubscriber {
	return &KitsuSocketSubscriber{token: token, apiUrl: apiUrl}
}

// Subscribe connects to Zou over socket.io and forwards task:* events on
// out until ctx is cancelled or the socket disconnects with an error.
// Returns the first transport-level error encountered.
func (s *KitsuSocketSubscriber) Subscribe(ctx context.Context, out chan<- KitsuEvent) error {
	rootURL, err := socketRootURL(s.apiUrl)
	if err != nil {
		return fmt.Errorf("invalid api_url for socket: %w", err)
	}

	opts := socket.DefaultOptions()
	opts.SetPath("/socket.io/")
	opts.SetAuth(map[string]any{"token": s.token})
	opts.SetExtraHeaders(http.Header{
		"Authorization": []string{"Bearer " + s.token},
		"Cookie":        []string{"access_token_cookie=" + s.token},
	})
	opts.SetReconnection(false)
	// Bound how long we wait for the initial socket.io handshake before
	// giving up so a wedged Zou doesn't leave the listener hanging.
	opts.SetTimeout(20 * time.Second)

	sock, err := socket.Connect(rootURL+"/events", opts)
	if err != nil {
		return fmt.Errorf("socket connect: %w", err)
	}

	done := make(chan error, 1)
	once := sync.Once{}
	finish := func(err error) { once.Do(func() { done <- err }) }

	sock.On("connect", func(args ...any) {
		log.Printf("kitsu_subscriber: connected to %s/events", rootURL)
	})
	sock.On("connect_error", func(args ...any) {
		if len(args) > 0 {
			finish(fmt.Errorf("connect_error: %v", args[0]))
			return
		}
		finish(errors.New("connect_error"))
	})
	sock.On("disconnect", func(args ...any) {
		reason := "disconnected"
		if len(args) > 0 {
			reason = fmt.Sprintf("%v", args[0])
		}
		finish(fmt.Errorf("disconnect: %s", reason))
	})

	forward := func(name string) events.Listener {
		return func(args ...any) {
			if len(args) == 0 {
				return
			}
			payload, ok := args[0].(map[string]any)
			if !ok {
				return
			}
			ev := KitsuEvent{
				Name:      name,
				TaskId:    stringField(payload, "task_id"),
				ProjectId: stringField(payload, "project_id"),
				PersonId:  stringField(payload, "person_id"),
			}
			if ev.TaskId == "" {
				return
			}
			select {
			case out <- ev:
			case <-ctx.Done():
			}
		}
	}

	// Register the task events we care about. New event names from future
	// Zou versions can be added here without touching the listener.
	for _, name := range []string{"task:new", "task:update", "task:assign", "task:unassign"} {
		sock.On(events.EventName(name), forward(name))
	}

	select {
	case <-ctx.Done():
		sock.Disconnect()
		return ctx.Err()
	case err := <-done:
		sock.Disconnect()
		return err
	}
}

// socketRootURL converts a Kitsu REST api_url like "https://host/api" into
// the matching socket.io root "https://host" that Zou's python-socketio
// listens on.
func socketRootURL(apiUrl string) (string, error) {
	if apiUrl == "" {
		return "", errors.New("empty api_url")
	}
	u, err := url.Parse(apiUrl)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// stringField fetches a string-valued key from a JSON-decoded map,
// returning "" when missing or wrongly typed.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
