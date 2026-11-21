// Package gui renders Durable conversations in a native MyGo workspace.
package gui

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

//go:embed assets/meter.png
var appIcon []byte

//go:embed assets/fir-menu.png
var menuIcon []byte

type pane struct {
	conversation     string
	expandedTools    map[string]bool
	transcriptScroll ui.ScrollState
	rejected         bool
	cancel           context.CancelFunc
	state            client.State
	submission       client.Submission
	list             ui.ListState
	draft            string
	notice           string
	connected        bool
	starting         bool
	pendingFirst     bool
	loading          bool
	meterLevel       float32
	meterAt          time.Time
	partialLength    int
	modelDraft       *protocol.SelectionInput
	modelPicker      modelPickerState
}

type editor struct {
	id         uuid.UUID
	value      string
	suggestion string
}

type paneDrag struct {
	tab, pane uuid.UUID
}

type app struct {
	ctx                  context.Context
	api                  *client.Client
	window               *mygo.Window
	tray                 *mygo.Tray
	update               func(func())
	name                 string
	provider             string
	layout               protocol.Layout
	catalog              protocol.Catalog
	panes                map[uuid.UUID]*pane
	paneScroll           ui.ScrollState
	selectedPane         uuid.UUID
	selectedPaneTab      uuid.UUID
	widthDrag            *paneResize
	revealPane           uuid.UUID
	projectForm          bool
	tabEditor, cwdEditor *editor
	closeTabConfirm      *uuid.UUID
	closePaneConfirm     *paneDrag
	nameEditor           *nameEditor
	paletteOpen          bool
	paletteFilter        string
	writeToast           string
	badgeCount           int
	pathQuery            string
	pathResultsQuery     string
	pathError            string
	pathCancel           context.CancelFunc
	pathResults          []string
	projectPath          string
	projectName          string
	filter               string
	notice               string
	loaded, fetching     bool
	saving, creating     bool
	conflict             bool
	mayRetry, mayAbort   bool
	mayDictate           bool
	dictation            *dictationRecording
	spaceHold            *spaceHold
	dictationVisible     bool
	dictationTextFocused bool
	backend              BackendControls
	backendInfo          protocol.ServerInfo
	backendOpen          bool
	backendPending       string
	backendConfirm       string
	backendNotice        string
	backendOnline        bool
	settingsSection      string
	modelsInventory      protocol.ModelInventory
	modelDefaults        protocol.Settings
	modelsFilter         string
	modelsLoading        bool
	modelsLoaded         bool
	modelsError          string
	recentModels         []protocol.ModelRef
}

type geometry struct {
	Width, Height int
}

func Run(parent context.Context, api *client.Client, info protocol.ServerInfo, data, name, cwd string, backend BackendControls) error {
	if err := registerFonts(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a := &app{
		ctx: ctx, api: api, name: name, provider: info.Provider, projectPath: cwd,
		panes: make(map[uuid.UUID]*pane), backend: backend,
	}
	a.applyBackendInfo(info)
	if backend.StartupError != nil {
		a.backendNotice = backend.StartupError.Error()
		a.backendOpen = true
	}
	path := filepath.Join(data, "desktop-window.json")
	size := geometry{Width: 1190, Height: 780}
	if stored, err := os.ReadFile(path); err == nil {
		var saved geometry
		if json.Unmarshal(stored, &saved) == nil && saved.Width >= 850 && saved.Height >= 550 && saved.Width <= 5000 && saved.Height <= 5000 {
			size = saved
		}
	}
	mygo.App.SetName("meter")
	mygo.App.WhenReady(func() {
		a.window = mygo.NewWindow(mygo.WindowOptions{
			Title: "meter", Width: size.Width, Height: size.Height,
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 49,
			MinWidth: 850, MinHeight: 550, Content: ui.View(a.view),
		})
		a.update = a.window.Update
		if err := a.window.SetIcon(appIcon); err != nil {
			log.Printf("set window icon: %v", err)
		}
		if runtime.GOOS == "darwin" {
			if err := mygo.App.Dock.SetIcon(appIcon); err != nil {
				log.Printf("set dock icon: %v", err)
			}
			var err error
			a.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: menuIcon, IconIsTemplate: true, ToolTip: "meter"})
			if err != nil {
				log.Printf("create menu bar icon: %v", err)
			} else {
				a.tray.OnClick(func() { a.window.Show(); a.window.Focus() })
			}
		}
		a.window.OnClose(func(*mygo.CloseEvent) {
			bounds := a.window.Bounds()
			if bounds.Width >= 850 && bounds.Height >= 550 {
				saveGeometry(path, geometry{Width: bounds.Width, Height: bounds.Height})
			}
		})
		a.window.OnBlur(a.cancelDictation)
		a.window.OnClosed(func() { cancel(); mygo.App.Quit() })
		a.reload()
		a.refreshModels()
	})
	return mygo.App.Run()
}

func saveGeometry(path string, value geometry) {
	file, err := os.CreateTemp(filepath.Dir(path), ".window-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	written := json.NewEncoder(file).Encode(value)
	closed := file.Close()
	if written == nil && closed == nil {
		_ = os.Rename(file.Name(), path)
	}
}

func (a *app) reload() {
	if a.fetching || a.saving || a.creating {
		return
	}
	a.fetching = true
	go func() {
		layout, layoutErr := a.api.Layout(a.ctx, a.name)
		catalog, catalogErr := a.api.Catalog(a.ctx)
		a.update(func() {
			a.fetching = false
			if catalogErr == nil {
				a.catalog = catalog
			}
			if layoutErr == nil {
				a.layout, a.loaded, a.conflict = layout, true, false
				a.tabEditor, a.cwdEditor = nil, nil
				a.scopePrototypeTabs()
				a.syncPanes()
			}
			a.notice = ""
			if err := errors.Join(layoutErr, catalogErr); err != nil {
				a.notice = err.Error()
			}
		})
	}()
}

func (a *app) refreshCatalog() {
	if a.fetching || a.creating || a.saving {
		return
	}
	a.fetching = true
	go func() {
		catalog, err := a.api.Catalog(a.ctx)
		a.update(func() {
			a.fetching = false
			if err != nil {
				a.notice = err.Error()
				return
			}
			a.catalog = catalog
		})
	}()
}

func (a *app) syncPanes() {
	active := make(map[uuid.UUID]bool)
	for _, tab := range a.layout.Tabs {
		for _, node := range tab.Nodes {
			active[node.Id] = true
			p := a.panes[node.Id]
			if node.ConversationId == nil {
				if p == nil {
					a.panes[node.Id] = &pane{}
				}
				continue
			}
			if p != nil && p.conversation == *node.ConversationId {
				continue
			}
			if p == nil {
				p = &pane{}
				a.panes[node.Id] = p
			}
			if !p.pendingFirst {
				p.draft = ""
			}
			if p.cancel != nil {
				p.cancel()
			}
			ctx, cancel := context.WithCancel(a.ctx)
			p.cancel, p.conversation = cancel, *node.ConversationId
			go func(id uuid.UUID, conversation string) {
				for event := range a.api.Subscribe(ctx, conversation) {
					a.update(func() {
						if a.panes[id] == p {
							p.connected = event.View != nil
							old := p.state.Current.Entries
							p.state.Apply(event)
							if p.connected && p.pendingFirst {
								p.pendingFirst, p.starting = false, false
								a.send(p, p.conversation, "prompt", false)
							}
							p.updateMeter(time.Now())
							a.captureTitle(p)
							a.updateBadge()
							current := p.state.Current.Entries
							if !event.Reset && len(current) > 0 && (len(old) == 0 || old[len(old)-1].Id != current[len(current)-1].Id) {
								a.recordPaneActivity(id, p.conversation, current[len(current)-1])
							}
						}
					})
				}
			}(node.Id, *node.ConversationId)
		}
	}
	for id, p := range a.panes {
		if !active[id] {
			if p.cancel != nil {
				p.cancel()
			}
			delete(a.panes, id)
		}
	}
	a.updateBadge()
}

func (a *app) recordPaneActivity(paneID uuid.UUID, conversationID string, latest protocol.Entry) {
	for i := range a.catalog.Conversations {
		if a.catalog.Conversations[i].Id == conversationID {
			a.catalog.Conversations[i].LastActivity = new(int(time.Now().UnixMilli()))
			break
		}
	}
	if latest.ToolName != nil && *latest.ToolName == "write" && latest.ToolSucceeded != nil && *latest.ToolSucceeded {
		if tab := a.activeTab(); tab != nil && nodeIndex(tab, paneID) >= 0 {
			a.writeToast = firstLine(clean(latest.Text))
		}
	}
}

func (a *app) updateBadge() {
	if runtime.GOOS != "darwin" || a.window == nil {
		return
	}
	count := 0
	for _, p := range a.panes {
		if p.connected && p.state.Current.Status == "running" {
			count++
		}
	}
	if count != a.badgeCount {
		a.badgeCount = count
		mygo.App.SetBadgeCount(count)
	}
}

func (a *app) captureTitle(p *pane) {
	for i, conversation := range a.catalog.Conversations {
		if conversation.Id != p.conversation {
			continue
		}
		if conversation.Title != nil {
			return
		}
		for _, entry := range p.state.Current.Entries {
			if entry.Kind != "pi.user" && entry.Kind != "user" {
				continue
			}
			title := firstLine(clean(entry.Text))
			if title == "" || (a.provider == "fixture" && strings.HasPrefix(title, "fixture:")) {
				continue
			}
			if chars := []rune(title); len(chars) > 48 {
				title = strings.TrimSpace(string(chars[:47])) + "…"
			}
			a.catalog.Conversations[i].Title = &title
			return
		}
		return
	}
}

func (a *app) busy() bool { return !a.loaded || a.saving || a.fetching || a.creating || a.conflict }

func (a *app) save(edit func(*protocol.Layout)) { a.saveThen(edit, nil) }

func (a *app) saveThen(edit func(*protocol.Layout), done func()) {
	if a.busy() {
		return
	}
	next := copyLayout(a.layout)
	edit(&next)
	a.saving = true
	go func() {
		layout, err := a.api.SaveLayout(a.ctx, a.name, next)
		a.update(func() {
			a.saving = false
			if err != nil {
				a.notice = err.Error()
				for _, p := range a.panes {
					if p.pendingFirst {
						p.pendingFirst, p.starting, p.notice = false, false, err.Error()
					}
				}
				a.revealPane = uuid.Nil
				var problem *client.APIError
				a.conflict = !errors.As(err, &problem) || problem.Problem.Code == "layout_conflict"
				return
			}
			a.layout = layout
			a.notice = ""
			a.syncPanes()
			if done != nil {
				done()
			}
		})
	}()
}

func (a *app) renameConversation(id, name string) {
	go func() {
		updated, err := a.api.RenameConversation(a.ctx, id, name)
		a.update(func() {
			if err != nil {
				a.notice = "Tab renamed, but conversation rename failed: " + err.Error()
				return
			}
			for i, item := range a.catalog.Conversations {
				if item.Id == id {
					a.catalog.Conversations[i] = updated
					return
				}
			}
			a.catalog.Conversations = append(a.catalog.Conversations, updated)
		})
	}()
}

// The early prototype stored top-level conversation tabs. Give tabs rooted
// in a registered project a project and a name on first load.
func (a *app) scopePrototypeTabs() {
	for i := range a.layout.Tabs {
		tab := &a.layout.Tabs[i]
		if tab.ProjectId != nil {
			continue
		}
		for _, node := range orderedPanes(tab) {
			if node.ConversationId == nil {
				continue
			}
			conversation, _ := a.metadata(*node.ConversationId)
			if conversation.ProjectId != nil {
				tab.ProjectId = conversation.ProjectId
				tab.Name = new("Recovered")
				break
			}
		}
	}
	if a.layout.ActiveProject != nil || a.layout.Revision > 0 {
		return
	}
	if tab := a.activeTab(); tab != nil && tab.ProjectId != nil {
		a.layout.ActiveProject = tab.ProjectId
		return
	}
	for _, project := range a.catalog.Projects {
		if id, err := uuid.Parse(project.Id); err == nil {
			selectProject(&a.layout, id)
			return
		}
	}
}

func (a *app) activeProject() (protocol.Project, bool) {
	if a.layout.ActiveProject != nil {
		id := a.layout.ActiveProject.String()
		for _, project := range a.catalog.Projects {
			if project.Id == id {
				return project, true
			}
		}
	}
	return protocol.Project{}, false
}

func (a *app) activeTab() *protocol.LayoutTab {
	if a.layout.ActiveTab != nil {
		if i := tabIndex(&a.layout, *a.layout.ActiveTab); i >= 0 {
			return &a.layout.Tabs[i]
		}
	}
	return nil
}

func (a *app) selectProject(id uuid.UUID) {
	a.tabEditor, a.cwdEditor = nil, nil
	a.paneScroll.X = 0
	a.save(func(layout *protocol.Layout) { selectProject(layout, id) })
}

func (a *app) register() {
	name, path := strings.TrimSpace(a.projectName), expandHome(a.projectPath)
	if a.busy() || path == "" || name == "" {
		return
	}
	a.creating = true
	go func() {
		project, err := a.api.RegisterProject(a.ctx, path, name)
		a.update(func() {
			a.creating = false
			if err != nil {
				a.pathError = err.Error()
				return
			}
			id, err := uuid.Parse(project.Id)
			if err != nil {
				a.notice = err.Error()
				return
			}
			updated := false
			for i, old := range a.catalog.Projects {
				if old.Id == project.Id {
					a.catalog.Projects[i], updated = project, true
					break
				}
			}
			if !updated {
				a.catalog.Projects = append(a.catalog.Projects, project)
			}
			a.projectForm = false
			a.projectPath, a.projectName = "", ""
			a.save(func(layout *protocol.Layout) {
				selectProject(layout, id)
				if layout.ActiveTab == nil {
					addTab(layout, id, suggestTabName(*layout, id))
				}
			})
		})
	}()
}

func (a *app) create(tabID uuid.UUID, node protocol.LayoutNode, cwd string, p *pane) {
	if a.busy() || p.starting || strings.TrimSpace(p.draft) == "" {
		return
	}
	input := protocol.CreateInput{Cwd: cwd, ProjectId: a.layout.ActiveProject, Selection: p.modelDraft}
	p.starting, p.notice = true, "Creating conversation…"
	a.creating = true
	go func() {
		conversation, err := a.api.CreateConversation(a.ctx, input)
		a.update(func() {
			a.creating = false
			if err != nil {
				p.starting, p.notice = false, err.Error()
				return
			}
			a.catalog.Conversations = append([]protocol.Conversation{conversation}, a.catalog.Conversations...)
			p.pendingFirst, p.notice = true, "Connecting…"
			a.openIn(tabID, node.Id, conversation.Id)
		})
	}()
}

func (a *app) openIn(tabID, nodeID uuid.UUID, id string) {
	a.save(func(layout *protocol.Layout) {
		tab := &layout.Tabs[tabIndex(layout, tabID)]
		i := nodeIndex(tab, nodeID)
		node := &tab.Nodes[i]
		node.ConversationId, node.Empty, node.Cwd = &id, nil, nil
	})
}

func (a *app) recent(id string) {
	tab := a.activeTab()
	if tab == nil {
		if a.layout.ActiveProject != nil && len(a.layout.Tabs) < 24 {
			a.save(func(layout *protocol.Layout) {
				addTab(layout, *layout.ActiveProject, suggestTabName(*layout, *layout.ActiveProject))
				tab := &layout.Tabs[len(layout.Tabs)-1]
				tab.Nodes[0] = protocol.LayoutNode{Id: tab.Root, ConversationId: &id}
			})
		}
		return
	}
	panes := orderedPanes(tab)
	for _, node := range panes {
		if node.Empty != nil {
			a.openIn(tab.Id, node.Id, id)
			return
		}
	}
	if len(panes) < maxPanes {
		a.appendPane(tab.Id, protocol.LayoutNode{Id: uuid.New(), ConversationId: &id})
	}
}

func (a *app) appendPane(tabID uuid.UUID, node protocol.LayoutNode) {
	a.save(func(layout *protocol.Layout) {
		tab := &layout.Tabs[tabIndex(layout, tabID)]
		setPanes(tab, append(orderedPanes(tab), node))
	})
	a.revealPane = node.Id
}

func (a *app) changeCwd(tabID uuid.UUID, node protocol.LayoutNode, cwd string) {
	if a.busy() || cwd == "" {
		return
	}
	a.creating = true
	go func() {
		if node.ConversationId == nil {
			path, err := a.api.ResolveCwd(a.ctx, cwd)
			a.update(func() {
				a.creating = false
				if err != nil {
					a.pathError = err.Error()
					return
				}
				a.cwdEditor = nil
				a.save(func(layout *protocol.Layout) {
					tab := &layout.Tabs[tabIndex(layout, tabID)]
					tab.Nodes[nodeIndex(tab, node.Id)].Cwd = &path
				})
			})
			return
		}
		id := *node.ConversationId
		conversation, err := a.api.SetConversationCwd(a.ctx, id, cwd)
		a.update(func() {
			a.creating = false
			if err != nil {
				a.pathError = err.Error()
				return
			}
			a.cwdEditor = nil
			a.notice = ""
			for i := range a.catalog.Conversations {
				if a.catalog.Conversations[i].Id == id {
					a.catalog.Conversations[i] = conversation
					return
				}
			}
			a.catalog.Conversations = append(a.catalog.Conversations, conversation)
		})
	}()
}

func (a *app) send(p *pane, id string, mode protocol.SubmitInputMode, retry bool) {
	if !p.connected {
		return
	}
	if retry {
		if !a.mayRetry || !p.submission.Uncertain() {
			return
		}
	} else {
		if !p.submission.Begin(mode, p.draft) {
			return
		}
		p.draft = ""
		p.list.ScrollToEnd()
	}
	input, ok := p.submission.Take()
	if !ok {
		return
	}
	p.notice = "Sending…"
	go func() {
		_, err := a.api.Submit(a.ctx, id, input)
		a.update(func() {
			p.submission.Resolve(err)
			p.notice = ""
			if err == nil {
				return
			}
			p.notice = err.Error()
			if p.submission.Uncertain() {
				p.notice = "Submission outcome unknown"
			} else if p.draft == "" {
				p.draft = p.submission.Restore()
			} else {
				p.rejected = true
			}
		})
	}()
}

func (a *app) history(p *pane, id string) {
	if p.loading || p.state.Before == nil || !p.connected {
		return
	}
	p.loading = true
	cursor := p.state.Before
	go func() {
		page, err := a.api.History(a.ctx, id, cursor)
		a.update(func() {
			p.loading = false
			if err != nil {
				p.notice = err.Error()
				return
			}
			p.state.Prepend(cursor, page)
		})
	}()
}

func (a *app) abort(p *pane, id, target string) {
	go func() {
		err := a.api.Abort(a.ctx, id, target)
		a.update(func() {
			p.notice = "Abort requested"
			if err != nil {
				p.notice = err.Error()
			}
		})
	}()
}
