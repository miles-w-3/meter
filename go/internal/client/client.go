// Package client supplies the transport and state shared by the desktop and terminal clients.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"pi-workspace.local/workspace/internal/protocol"
)

type Client struct {
	api          *protocol.ClientWithResponses
	longAPI      *protocol.ClientWithResponses
	dialer       websocket.Dialer
	endpointFile string
}

func New(data, transport string) (*Client, error) {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	c := &Client{dialer: websocket.Dialer{HandshakeTimeout: 4 * time.Second}}
	httpTransport := &http.Transport{DialContext: dialer.DialContext}
	var roundTripper http.RoundTripper = httpTransport
	switch transport {
	case "", "unix":
		dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", filepath.Join(data, "workspace.sock"))
		}
		httpTransport.DialContext = dial
		c.dialer.NetDialContext = dial
	case "tcp":
		c.endpointFile = filepath.Join(data, "endpoint.json")
		roundTripper = localTCP{file: c.endpointFile, transport: httpTransport}
	default:
		return nil, fmt.Errorf("PI_TRANSPORT must be unix or tcp")
	}
	api, err := protocol.NewClientWithResponses("http://workspace", protocol.WithHTTPClient(&http.Client{
		Transport: roundTripper, Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}))
	if err != nil {
		return nil, err
	}
	c.api = api
	c.longAPI, err = protocol.NewClientWithResponses("http://workspace", protocol.WithHTTPClient(&http.Client{
		Transport: roundTripper, Timeout: 40 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}))
	if err != nil {
		return nil, err
	}
	return c, nil
}

type localTCP struct {
	file      string
	transport http.RoundTripper
}

func (t localTCP) RoundTrip(req *http.Request) (*http.Response, error) {
	address, token, err := readEndpoint(t.file)
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(req.Context())
	cloned.URL.Host = address
	cloned.Host = address
	cloned.Header.Set("Authorization", "Bearer "+token)
	return t.transport.RoundTrip(cloned)
}

func readEndpoint(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var endpoint struct {
		Port  int
		Token string
	}
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return "", "", fmt.Errorf("invalid loopback endpoint: %w", err)
	}
	if endpoint.Port < 1 || endpoint.Port > 65535 || len(endpoint.Token) != 64 {
		return "", "", fmt.Errorf("invalid loopback endpoint")
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(endpoint.Port)), endpoint.Token, nil
}

type APIError struct {
	Status  int
	Problem protocol.Problem
}

func (e *APIError) Error() string {
	return fmt.Sprintf("workspace HTTP %d: %s", e.Status, e.Problem.Message)
}

func responseError(status int, body []byte) error {
	problem := protocol.Problem{Code: "unexpected_response", Message: http.StatusText(status)}
	_ = json.Unmarshal(body, &problem)
	return &APIError{Status: status, Problem: problem}
}

// Uncertain reports whether the server may have committed before a response failed.
func Uncertain(err error) bool {
	var response *APIError
	return !errors.As(err, &response) || response.Status >= 500
}

func (c *Client) Info(ctx context.Context) (protocol.ServerInfo, error) {
	res, err := c.api.GetServerWithResponse(ctx)
	if err != nil {
		return protocol.ServerInfo{}, err
	}
	info := res.GetJSON200()
	if info == nil {
		return protocol.ServerInfo{}, responseError(res.StatusCode(), res.GetBody())
	}
	if info.Protocol != "v1" || !info.Ready {
		return protocol.ServerInfo{}, fmt.Errorf("incompatible or unready daemon protocol %q", info.Protocol)
	}
	return *info, nil
}

func (c *Client) Models(ctx context.Context) (protocol.ModelInventory, error) {
	res, err := c.api.GetModelsWithResponse(ctx)
	if err != nil {
		return protocol.ModelInventory{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.ModelInventory{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) SetProvider(ctx context.Context, id string, enabled bool) (protocol.ModelInventory, error) {
	return c.setProvider(ctx, id, protocol.ProviderSetting{Enabled: &enabled})
}

// SetProviderSessions saves how long a provider keeps session processes and
// how many it keeps; the backend applies them to running processes too.
func (c *Client) SetProviderSessions(ctx context.Context, id string, sessions protocol.ProviderSessions) (protocol.ModelInventory, error) {
	return c.setProvider(ctx, id, protocol.ProviderSetting{Sessions: &sessions})
}

func (c *Client) setProvider(ctx context.Context, id string, setting protocol.ProviderSetting) (protocol.ModelInventory, error) {
	res, err := c.longAPI.SetProviderWithResponse(ctx, id, setting)
	if err != nil {
		return protocol.ModelInventory{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.ModelInventory{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) RefreshProvider(ctx context.Context, id string) (protocol.ModelInventory, error) {
	res, err := c.longAPI.RefreshProviderWithResponse(ctx, id)
	if err != nil {
		return protocol.ModelInventory{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.ModelInventory{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) Defaults(ctx context.Context) (protocol.Settings, error) {
	res, err := c.api.GetDefaultsWithResponse(ctx)
	if err != nil {
		return protocol.Settings{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Settings{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) SetDefaults(ctx context.Context, input protocol.DefaultSettings) (protocol.Settings, error) {
	res, err := c.api.SetDefaultsWithResponse(ctx, input)
	if err != nil {
		return protocol.Settings{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Settings{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) Stop(ctx context.Context) error {
	res, err := c.api.StopServerWithResponse(ctx)
	if err != nil {
		return err
	}
	if res.StatusCode() != 202 {
		return responseError(res.StatusCode(), res.GetBody())
	}
	return nil
}

func (c *Client) Create(ctx context.Context, cwd string) (protocol.Conversation, error) {
	return c.CreateConversation(ctx, protocol.CreateInput{Cwd: cwd})
}

func (c *Client) CreateConversation(ctx context.Context, input protocol.CreateInput) (protocol.Conversation, error) {
	res, err := c.api.CreateConversationWithResponse(ctx, input)
	if err != nil {
		return protocol.Conversation{}, err
	}
	if body := res.GetJSON201(); body != nil {
		return *body, nil
	}
	return protocol.Conversation{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) SetConversationModel(ctx context.Context, id string, input protocol.SelectionChange) (protocol.Conversation, error) {
	res, err := c.api.SetConversationModelWithResponse(ctx, id, input)
	if err != nil {
		return protocol.Conversation{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Conversation{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) Conversation(ctx context.Context, id string) (protocol.Conversation, error) {
	res, err := c.api.GetConversationWithResponse(ctx, id)
	if err != nil {
		return protocol.Conversation{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Conversation{}, responseError(res.StatusCode(), res.GetBody())
}

// HistoryTree returns the fork relationships for the conversation's history tree.
func (c *Client) HistoryTree(ctx context.Context, id string) (protocol.HistoryTree, error) {
	res, err := c.api.GetHistoryTreeWithResponse(ctx, id)
	if err != nil {
		return protocol.HistoryTree{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.HistoryTree{}, responseError(res.StatusCode(), res.GetBody())
}

// HistoricalPath returns a page on a fixed conversation path and its context usage.
// at is a Durable entry ID on that conversation's history path.
func (c *Client) HistoricalPath(ctx context.Context, id, at string, before *string) (protocol.HistoricalPath, error) {
	res, err := c.api.GetHistoricalPathWithResponse(ctx, id, &protocol.GetHistoricalPathParams{At: at, Before: before})
	if err != nil {
		return protocol.HistoricalPath{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.HistoricalPath{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) History(ctx context.Context, id string, before *string) (protocol.HistoryPage, error) {
	res, err := c.api.GetEntriesWithResponse(ctx, id, &protocol.GetEntriesParams{Before: before})
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.HistoryPage{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) Submit(ctx context.Context, id string, input protocol.SubmitInput) (protocol.Receipt, error) {
	res, err := c.api.SubmitMessageWithResponse(ctx, id, input)
	if err != nil {
		return protocol.Receipt{}, err
	}
	if body := res.GetJSON202(); body != nil {
		return *body, nil
	}
	return protocol.Receipt{}, responseError(res.StatusCode(), res.GetBody())
}

// ForkAndSubmit creates or recovers the request's fork and admits its first message.
func (c *Client) ForkAndSubmit(ctx context.Context, attached string, input protocol.ForkInput) (protocol.ForkReceipt, error) {
	res, err := c.api.ForkAndSubmitMessageWithResponse(ctx, attached, input)
	if err != nil {
		return protocol.ForkReceipt{}, err
	}
	if body := res.GetJSON202(); body != nil {
		return *body, nil
	}
	return protocol.ForkReceipt{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) RecallQueued(ctx context.Context, id, submission string) (protocol.RecalledMessage, error) {
	res, err := c.api.RecallQueuedWithResponse(ctx, id, submission)
	if err != nil {
		return protocol.RecalledMessage{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.RecalledMessage{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) SteerQueued(ctx context.Context, id, submission string) error {
	res, err := c.api.SteerQueuedWithResponse(ctx, id, submission)
	if err != nil {
		return err
	}
	if res.StatusCode() != 202 {
		return responseError(res.StatusCode(), res.GetBody())
	}
	return nil
}

func (c *Client) Abort(ctx context.Context, id, target string) error {
	res, err := c.api.AbortRunWithResponse(ctx, id, protocol.AbortInput{Target: target})
	if err != nil {
		return err
	}
	if res.StatusCode() != 202 {
		return responseError(res.StatusCode(), res.GetBody())
	}
	return nil
}

// Event is one step of a conversation stream. View is the whole current view,
// rebuilt from the server's incremental updates.
type Event struct {
	View  *protocol.ConversationView
	Reset bool
	// EntriesUnchanged is set only for a validated incremental frame that retains every entry.
	EntriesUnchanged bool
	Err              error
	// Fatal reports an error that reconnecting cannot fix. The stream ends after it.
	Fatal bool
}

// ErrViewTooLarge reports a conversation whose view the server cannot fit in
// a stream frame even after trimming its older entries.
var ErrViewTooLarge = errors.New("conversation view is too large to stream")

// Subscribe reconnects until ctx is canceled or a fatal error ends the stream.
// Each reconnection yields a fresh snapshot.
func (c *Client) Subscribe(ctx context.Context, id string) <-chan Event {
	events := make(chan Event)
	go func() {
		defer close(events)
		pause := 250 * time.Millisecond
		send := func(event Event) bool {
			select {
			case events <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for ctx.Err() == nil {
			err := c.readStream(ctx, id, func(event Event) bool {
				pause = 250 * time.Millisecond
				return send(event)
			})
			fatal := errors.Is(err, ErrViewTooLarge)
			if ctx.Err() != nil || !send(Event{Err: err, Fatal: fatal}) || fatal {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
				pause = min(pause*2, 4*time.Second)
			}
		}
	}()
	return events
}

func (c *Client) dialStream(ctx context.Context, path string) (*websocket.Conn, error) {
	address := "workspace"
	headers := http.Header{}
	if c.endpointFile != "" {
		host, token, err := readEndpoint(c.endpointFile)
		if err != nil {
			return nil, err
		}
		address = host
		headers.Set("Authorization", "Bearer "+token)
	}
	ws, response, err := c.dialer.DialContext(ctx, "ws://"+address+path, headers)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return nil, err
	}
	return ws, nil
}

const (
	// maxFrameBytes matches the server's frame limit.
	maxFrameBytes = 2_500_000

	// closeViewTooLarge is the close code the server sends when a view cannot
	// be streamed at all.
	closeViewTooLarge = 4413
)

func (c *Client) readStream(ctx context.Context, id string, send func(Event) bool) error {
	ws, err := c.dialStream(ctx, "/v1/conversations/"+url.PathEscape(id)+"/stream")
	if err != nil {
		return err
	}
	defer ws.Close()
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	ws.SetReadLimit(maxFrameBytes)
	refreshDeadline := func() error { return ws.SetReadDeadline(time.Now().Add(23 * time.Second)) }
	_ = refreshDeadline()
	ping := ws.PingHandler()
	ws.SetPingHandler(func(s string) error {
		_ = refreshDeadline()
		return ping(s)
	})
	order := frameOrder{id: id}
	for {
		_, data, err := ws.ReadMessage()
		if websocket.IsCloseError(err, closeViewTooLarge) {
			return ErrViewTooLarge
		}
		if err != nil {
			return err
		}
		event, err := order.accept(data)
		if err != nil {
			return err
		}
		if event.View != nil && !send(event) {
			return ctx.Err()
		}
	}
}

// streamFrame decodes either kind of ConversationFrame in one pass. Drop and
// Keep are zero in snapshots.
type streamFrame struct {
	Type       string                    `json:"type"`
	Generation string                    `json:"generation"`
	Revision   int                       `json:"revision"`
	Drop       int                       `json:"drop"`
	Keep       int                       `json:"keep"`
	View       protocol.ConversationView `json:"view"`
}

// frameOrder checks that frames continue one server generation in revision
// order, and rebuilds each update's entries from the previous view's.
type frameOrder struct {
	id      string
	boot    string
	seq     int
	entries []protocol.Entry
}

var errStreamOrder = errors.New("conversation stream revision changed")

func (o *frameOrder) accept(data []byte) (Event, error) {
	var frame streamFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return Event{}, err
	}
	if frame.View.ConversationId != o.id {
		return Event{}, errStreamOrder
	}
	first := o.boot == ""
	if first {
		if frame.Type != "snapshot" || frame.Revision != 0 || frame.Generation == "" || frame.Generation == "00000000-0000-0000-0000-000000000000" {
			return Event{}, errStreamOrder
		}
		o.boot = frame.Generation
	} else {
		if frame.Generation != o.boot {
			return Event{}, errStreamOrder
		}
		if frame.Revision <= o.seq {
			return Event{}, nil // A repeated frame.
		}
		if frame.Revision != o.seq+1 {
			return Event{}, errStreamOrder
		}
	}
	unchanged := false
	switch frame.Type {
	case "snapshot":
		// A later snapshot replaces the view without discarding paged history.
	case "update":
		if first || frame.Drop < 0 || frame.Keep < 0 || frame.Drop+frame.Keep > len(o.entries) {
			return Event{}, errStreamOrder
		}
		unchanged = frame.Drop == 0 && frame.Keep == len(o.entries) && len(frame.View.Entries) == 0
		if unchanged {
			frame.View.Entries = o.entries
		} else {
			entries := make([]protocol.Entry, 0, frame.Keep+len(frame.View.Entries))
			entries = append(entries, o.entries[frame.Drop:frame.Drop+frame.Keep]...)
			frame.View.Entries = append(entries, frame.View.Entries...)
		}
	default:
		return Event{}, fmt.Errorf("unknown conversation frame %q", frame.Type)
	}
	if frame.View.Entries == nil {
		frame.View.Entries = []protocol.Entry{}
	}
	o.seq, o.entries = frame.Revision, frame.View.Entries
	return Event{View: &frame.View, Reset: first, EntriesUnchanged: unchanged}, nil
}

// EntryDiffs retrieves the file diffs a tool entry recorded.
func (c *Client) EntryDiffs(ctx context.Context, conversation, entry string) (protocol.EntryDiffs, error) {
	res, err := c.api.GetEntryDiffsWithResponse(ctx, conversation, entry)
	if err != nil {
		return protocol.EntryDiffs{}, err
	}
	if res.JSON200 == nil {
		return protocol.EntryDiffs{}, responseError(res.StatusCode(), res.Body)
	}
	return *res.JSON200, nil
}

// EntryImages retrieves stored image blocks separately from streaming updates.
// UserMessage retrieves the full stored content and images for editing.
func (c *Client) UserMessage(ctx context.Context, conversation, entry string) (protocol.UserMessage, error) {
	res, err := c.api.GetUserMessageWithResponse(ctx, conversation, entry)
	if err != nil {
		return protocol.UserMessage{}, err
	}
	if res.JSON200 == nil {
		return protocol.UserMessage{}, responseError(res.StatusCode(), res.Body)
	}
	return *res.JSON200, nil
}

func (c *Client) EntryImages(ctx context.Context, conversation, entry string) ([]protocol.ImageAttachment, error) {
	res, err := c.api.GetEntryImagesWithResponse(ctx, conversation, entry)
	if err != nil {
		return nil, err
	}
	if res.JSON200 == nil {
		return nil, responseError(res.StatusCode(), res.Body)
	}
	return res.JSON200.Images, nil
}
