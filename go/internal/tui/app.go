package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

// Start says what the TUI opens with.
type Start struct {
	// Conversation opens directly, skipping the project picker.
	Conversation *protocol.Conversation
	// Cwd preselects the project registered for this directory.
	Cwd string
	// Create starts a new conversation in Cwd's project, adding the project
	// first if none is registered there.
	Create bool

	project *uuid.UUID // reopen at a project's conversations
}

// App shows the launcher until a conversation is chosen, then the
// conversation, and the launcher again when the conversation is closed.
type leaveMsg struct{ project *uuid.UUID }

type App struct {
	ctx     context.Context
	api     *client.Client
	info    protocol.ServerInfo
	options Options

	launcher *launcher
	model    *Model
	cached   *Model
	size     *tea.WindowSizeMsg
	profile  *tea.ColorProfileMsg
	keyboard *tea.KeyboardEnhancementsMsg
}

func NewApp(ctx context.Context, api *client.Client, info protocol.ServerInfo, options Options, start Start) *App {
	a := &App{ctx: ctx, api: api, info: info, options: options}
	if start.Conversation != nil {
		a.model = New(ctx, api, *start.Conversation, info, options)
	} else {
		a.launcher = newLauncher(ctx, api, options, start)
	}
	return a
}

// ConversationID is the conversation open when the TUI exits, if any.
func (a *App) ConversationID() string {
	if a.model == nil {
		return ""
	}
	return a.model.ConversationID()
}

func (a *App) Init() tea.Cmd {
	if a.model != nil {
		return a.model.Init()
	}
	return a.launcher.Init()
}

func (a *App) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		a.size = &msg
	case tea.ColorProfileMsg:
		a.profile = &msg
	case tea.KeyboardEnhancementsMsg:
		a.keyboard = &msg
	case openMsg:
		if a.model != nil {
			return a, nil
		}
		if a.cached != nil {
			a.model, a.cached = a.cached, nil
			a.model.open(msg.conversation)
		} else {
			a.model = New(a.ctx, a.api, msg.conversation, a.info, a.options)
		}
		a.launcher = nil
		a.replay(a.model)
		a.model.notice = msg.notice
		return a, a.model.Init()
	case leaveMsg:
		if a.model == nil {
			return a, nil
		}
		a.model.stop()
		a.cached = a.model
		a.model, a.launcher = nil, newLauncher(a.ctx, a.api, a.options, Start{project: msg.project})
		a.replay(a.launcher)
		return a, a.launcher.Init()
	}
	if a.model != nil {
		_, cmd := a.model.Update(message)
		return a, cmd
	}
	_, cmd := a.launcher.Update(message)
	return a, cmd
}

// replay gives a new view the terminal's size and colors, which arrive once.
func (a *App) replay(view tea.Model) {
	if a.size != nil {
		view.Update(*a.size)
	}
	if a.profile != nil {
		view.Update(*a.profile)
	}
	if a.keyboard != nil {
		view.Update(*a.keyboard)
	}
}

func (a *App) View() tea.View {
	if a.model != nil {
		return a.model.View()
	}
	return a.launcher.View()
}
