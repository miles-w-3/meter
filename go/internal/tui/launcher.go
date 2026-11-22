package tui

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

// The launcher picks or creates a project, then a conversation in it.
// It never changes the shared GUI layout.

type launchStage uint8

const (
	pickProject launchStage = iota
	pickConversation
	newProject
)

type launchDataMsg struct {
	catalog protocol.Catalog
	cwd     string
	err     error
}

type registeredMsg struct {
	project protocol.Project
	err     error
}

type launchErrMsg struct{ err error }

// openMsg hands a conversation from the launcher to a conversation view.
type openMsg struct {
	conversation protocol.Conversation
	notice       string
}

type launchItem struct {
	label, detail, filter string
	project               *protocol.Project
	conversation          *protocol.Conversation
	create                bool
}

func (i launchItem) FilterValue() string { return i.filter }

type launchDelegate struct{ colors terminalTheme }

func (d launchDelegate) Height() int                         { return 1 }
func (d launchDelegate) Spacing() int                        { return 0 }
func (d launchDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d launchDelegate) Render(out io.Writer, model list.Model, index int, item list.Item) {
	i := item.(launchItem)
	prefix, style := "  ", d.colors.fog
	if i.create {
		style = d.colors.muted
	}
	if index == model.Index() {
		prefix, style = "› ", d.colors.moss
	}
	line := prefix + style.Render(i.label)
	if i.detail != "" {
		line += "  " + d.colors.faint.Render(i.detail)
	}
	_, _ = io.WriteString(out, ansi.Truncate(line, model.Width(), "…"))
}

type launcher struct {
	ctx           context.Context
	api           *client.Client
	options       Options
	start         Start
	colors        terminalTheme
	plain, dumb   bool
	width, height int

	stage            launchStage
	catalog          protocol.Catalog
	loaded, busy     bool
	notice, autoName string
	startCwd         string
	project          protocol.Project
	list             list.Model
	filter           textinput.Model
	path, name       textinput.Model
	nameFocused      bool
	candidates       []string
}

func newLauncher(ctx context.Context, api *client.Client, options Options, start Start) *launcher {
	if options.Workspace == "" {
		options.Workspace = "default"
	}
	dumb := os.Getenv("TERM") == "dumb"
	_, noColor := os.LookupEnv("NO_COLOR")
	l := &launcher{ctx: ctx, api: api, options: options, start: start, dumb: dumb, plain: dumb || noColor, width: 80, height: 24}
	l.colors.set(l.plain, options.Transparent, colorprofile.Env(os.Environ()))
	l.filter = textinput.New()
	l.filter.Prompt = "Search  "
	l.filter.Placeholder = "Filter…"
	l.filter.CharLimit = 128
	l.filter.SetStyles(textinput.Styles{})
	l.path, l.name = textinput.New(), textinput.New()
	for _, input := range []*textinput.Model{&l.path, &l.name} {
		input.Prompt = ""
		input.CharLimit = 4096
		input.SetStyles(textinput.Styles{})
	}
	l.list = list.New(nil, launchDelegate{l.colors}, 20, 5)
	l.list.SetShowTitle(false)
	l.list.SetShowFilter(false)
	l.list.SetShowHelp(false)
	l.list.SetShowStatusBar(false)
	l.list.SetShowPagination(false)
	l.list.DisableQuitKeybindings()
	return l
}

func (l *launcher) Init() tea.Cmd {
	return tea.Batch(l.filter.Focus(), l.fetch())
}

func (l *launcher) fetch() tea.Cmd {
	if l.api == nil {
		return nil
	}
	api, ctx, cwd := l.api, l.ctx, l.start.Cwd
	return func() tea.Msg {
		catalog, err := api.Catalog(ctx)
		if err != nil {
			return launchDataMsg{err: err}
		}
		resolved := ""
		if cwd != "" {
			// Projects store the daemon's canonical path.
			if path, err := api.ResolveCwd(ctx, cwd); err == nil {
				resolved = path
			}
		}
		return launchDataMsg{catalog: catalog, cwd: resolved}
	}
}

func (l *launcher) receive(msg launchDataMsg) tea.Cmd {
	if msg.err != nil {
		l.notice = clean(msg.err.Error())
		return nil
	}
	l.catalog, l.loaded = msg.catalog, true
	l.startCwd = msg.cwd
	if l.startCwd == "" {
		l.startCwd = l.start.Cwd
	}
	var here *protocol.Project
	for i, p := range l.catalog.Projects {
		if p.Cwd == l.startCwd {
			here = &l.catalog.Projects[i]
		}
		if l.start.project != nil && p.Id == l.start.project.String() {
			l.showConversations(p)
			return l.filter.Focus()
		}
	}
	switch {
	case l.start.Create && here != nil:
		return l.create(*here)
	case l.start.Create || len(l.catalog.Projects) == 0:
		return l.showNewProject(l.startCwd)
	}
	l.showProjects(here)
	return nil
}

func (l *launcher) showProjects(preselect *protocol.Project) {
	l.stage, l.notice = pickProject, ""
	latest := make(map[string]int)
	for _, conversation := range l.catalog.Conversations {
		if conversation.ProjectId != nil {
			key := conversation.ProjectId.String()
			latest[key] = max(latest[key], value(conversation.LastActivity))
		}
	}
	counts := make(map[string]int)
	for _, conversation := range l.catalog.Conversations {
		if conversation.ProjectId != nil {
			counts[conversation.ProjectId.String()]++
		}
	}
	projects := slices.Clone(l.catalog.Projects)
	slices.SortStableFunc(projects, func(a, b protocol.Project) int {
		if order := cmp.Compare(latest[b.Id], latest[a.Id]); order != 0 {
			return order
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	items := []list.Item{launchItem{label: "+ New project…", filter: "new project", create: true}}
	selected := 0
	for _, p := range projects {
		project := p
		detail := homePath(project.Cwd)
		if n := counts[project.Id]; n > 0 {
			detail += " · " + plural(n, "conversation")
		}
		items = append(items, launchItem{label: clean(project.Name), detail: detail, filter: project.Name + " " + project.Cwd, project: &project})
		switch {
		case preselect != nil && preselect.Id == project.Id:
			selected = len(items) - 1
		}
	}
	if selected == 0 && len(items) > 1 {
		selected = 1
	}
	l.setItems(items, selected)
}

func (l *launcher) showConversations(project protocol.Project) {
	l.stage, l.project, l.notice = pickConversation, project, ""
	var conversationsInProject []protocol.Conversation
	for _, conversation := range l.catalog.Conversations {
		if (conversation.ProjectId != nil && conversation.ProjectId.String() == project.Id) || (conversation.ProjectId == nil && conversation.Cwd == project.Cwd) {
			conversationsInProject = append(conversationsInProject, conversation)
		}
	}
	slices.SortStableFunc(conversationsInProject, func(a, b protocol.Conversation) int {
		return cmp.Compare(value(b.LastActivity), value(a.LastActivity))
	})
	items := []list.Item{launchItem{label: "+ New conversation", detail: "in this project", filter: "new conversation", create: true}}
	for _, conversation := range conversationsInProject {
		conversation := conversation
		item := conversationItem{conversation: conversation}
		items = append(items, launchItem{label: item.title(), detail: activityAge(conversation.LastActivity), filter: item.title() + " " + conversation.Id, conversation: &conversation})
	}
	// Prefer resuming the most recent conversation.
	l.setItems(items, min(1, len(items)-1))
}

func (l *launcher) showNewProject(path string) tea.Cmd {
	l.stage, l.notice, l.candidates = newProject, "", nil
	l.filter.Blur()
	if path == "" {
		path, _ = os.Getwd()
	}
	l.path.SetValue(path)
	l.path.CursorEnd()
	l.name.SetValue("")
	l.autoName, l.nameFocused = "", false
	l.name.Blur()
	return l.path.Focus()
}

func (l *launcher) setItems(items []list.Item, selected int) {
	l.filter.SetValue("")
	l.list.ResetFilter()
	l.list.SetItems(items)
	l.list.Select(selected)
	l.resize()
}

func (l *launcher) resize() {
	body := min(max(1, l.width-4), 80)
	l.list.SetSize(body, max(1, l.height-11))
	l.filter.SetWidth(max(1, body-10))
	l.path.SetWidth(max(1, body-4))
	l.name.SetWidth(max(1, body-4))
}

func (l *launcher) create(project protocol.Project) tea.Cmd {
	id, err := uuid.Parse(project.Id)
	if err != nil || l.api == nil {
		return nil
	}
	l.busy, l.notice = true, "Starting a conversation in "+clean(project.Name)+"…"
	api, ctx := l.api, l.ctx
	return func() tea.Msg {
		conversation, err := api.CreateConversation(ctx, protocol.CreateInput{Cwd: project.Cwd, ProjectId: &id})
		if err != nil {
			return launchErrMsg{err}
		}
		return openMsg{conversation: conversation}
	}
}

func (l *launcher) register() tea.Cmd {
	path := filepath.Clean(expandHome(l.path.Value()))
	name := strings.TrimSpace(l.name.Value())
	if l.api == nil || strings.TrimSpace(l.path.Value()) == "" || name == "" {
		return nil
	}
	l.busy, l.notice = true, "Adding project…"
	api, ctx := l.api, l.ctx
	return func() tea.Msg {
		project, err := api.RegisterProject(ctx, path, name)
		return registeredMsg{project, err}
	}
}

func (l *launcher) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.resize()
		return l, nil
	case tea.ColorProfileMsg:
		l.colors.set(l.plain, l.options.Transparent, msg.Profile)
		l.list.SetDelegate(launchDelegate{l.colors})
		return l, nil
	case launchDataMsg:
		return l, l.receive(msg)
	case registeredMsg:
		l.busy = false
		if msg.err != nil {
			l.notice = clean(msg.err.Error())
			return l, nil
		}
		l.catalog.Projects = append(l.catalog.Projects, msg.project)
		return l, l.create(msg.project)
	case launchErrMsg:
		l.busy, l.notice = false, clean(msg.err.Error())
		if client.Uncertain(msg.err) {
			l.notice = "Create outcome unknown; check the project's conversations before retrying"
		}
		return l, nil
	case tea.KeyPressMsg:
		return l, l.key(msg)
	case tea.PasteMsg:
		msg.Content = clean(msg.Content)
		message = msg
	}
	return l, l.updateInput(message)
}

func (l *launcher) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "ctrl+q":
		return tea.Quit
	}
	if l.busy {
		return nil
	}
	if l.stage == newProject {
		return l.projectFormKey(msg)
	}
	switch msg.String() {
	case "esc":
		if l.stage == pickConversation {
			project := l.project
			l.showProjects(&project)
			return nil
		}
		return tea.Quit
	case "enter":
		return l.choose()
	case "up", "down", "pgup", "pgdown":
		var cmd tea.Cmd
		l.list, cmd = l.list.Update(msg)
		return cmd
	}
	return l.updateInput(msg)
}

func (l *launcher) choose() tea.Cmd {
	item, ok := l.list.SelectedItem().(launchItem)
	switch {
	case !ok || !l.loaded:
		return nil
	case l.stage == pickProject && item.create:
		return l.showNewProject(l.startCwd)
	case l.stage == pickProject:
		l.showConversations(*item.project)
		return nil
	case item.create:
		return l.create(l.project)
	}
	conversation := *item.conversation
	return func() tea.Msg { return openMsg{conversation: conversation} }
}

func (l *launcher) projectFormKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if len(l.catalog.Projects) == 0 {
			return tea.Quit
		}
		l.path.Blur()
		l.name.Blur()
		l.showProjects(nil)
		return l.filter.Focus()
	case "tab":
		if !l.nameFocused {
			value, matches := completeDirectory(l.path.Value())
			l.path.SetValue(value)
			l.path.CursorEnd()
			l.candidates = matches
		}
		return nil
	case "shift+tab", "up":
		l.nameFocused = false
		l.name.Blur()
		return l.path.Focus()
	case "down":
		return l.focusName()
	case "enter":
		if !l.nameFocused {
			return l.focusName()
		}
		return l.register()
	}
	return l.updateInput(msg)
}

// focusName moves to the name, suggesting the directory's name until edited.
func (l *launcher) focusName() tea.Cmd {
	if name := l.name.Value(); name == "" || name == l.autoName {
		l.autoName = filepath.Base(filepath.Clean(expandHome(l.path.Value())))
		l.name.SetValue(l.autoName)
		l.name.CursorEnd()
	}
	l.nameFocused, l.candidates = true, nil
	l.path.Blur()
	return l.name.Focus()
}

func (l *launcher) updateInput(message tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case l.stage == newProject && l.nameFocused:
		l.name, cmd = l.name.Update(message)
	case l.stage == newProject:
		before := l.path.Value()
		l.path, cmd = l.path.Update(message)
		if l.path.Value() != before {
			l.candidates, l.notice = nil, ""
		}
	default:
		before := l.filter.Value()
		l.filter, cmd = l.filter.Update(message)
		if query := l.filter.Value(); query != before {
			if query == "" {
				l.list.ResetFilter()
			} else {
				l.list.SetFilterText(query)
			}
		}
	}
	return cmd
}

func (l *launcher) content(width, rows int) []string {
	c := l.colors
	if !l.loaded && l.stage != newProject {
		if l.notice != "" {
			return []string{c.errorStyle.Render(l.notice)}
		}
		return []string{c.muted.Render("Loading projects…")}
	}
	switch l.stage {
	case newProject:
		lines := []string{c.muted.Render("Directory"), "  " + l.path.View()}
		for _, name := range l.candidates[:min(len(l.candidates), max(0, rows-7))] {
			lines = append(lines, "  "+c.faint.Render(name+string(filepath.Separator)))
		}
		return append(lines, "", c.muted.Render("Name"), "  "+l.name.View())
	default:
		lines := []string{l.filter.View(), ""}
		if len(l.list.VisibleItems()) == 0 {
			return append(lines, c.muted.Render("Nothing matches"))
		}
		return append(lines, strings.Split(l.list.View(), "\n")...)
	}
}

func (l *launcher) hint() string {
	c := l.colors
	switch {
	case l.notice != "":
		if l.busy {
			return c.muted.Render(l.notice)
		}
		return c.warning.Render(l.notice)
	case l.stage == newProject && l.nameFocused:
		return c.faint.Render("⏎ add project and start · ↑ directory · esc back")
	case l.stage == newProject:
		return c.faint.Render("tab complete · ⏎ name · esc back")
	case l.stage == pickConversation:
		return c.faint.Render("⏎ open · type to filter · esc projects")
	}
	return c.faint.Render("⏎ choose · type to filter · esc quit")
}

func (l *launcher) View() tea.View {
	w, h := max(1, l.width), max(1, l.height)
	body := min(max(1, w-4), 80)
	left := (w - body) / 2
	c := l.colors
	row := func(s string) string { return strings.Repeat(" ", left) + ansi.Truncate(s, body, "") }
	title := "Choose a project"
	switch l.stage {
	case pickConversation:
		title = clean(l.project.Name) + "  " + c.faint.Render(homePath(l.project.Cwd))
	case newProject:
		title = "New project"
	}
	lines := []string{"", row(c.moss.Render("meter") + "  " + c.fog.Render(title)), "", ""}
	rows := max(0, h-len(lines)-2)
	content := l.content(body, rows)
	for i := range rows {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		lines = append(lines, row(line))
	}
	lines = append(lines, row(l.hint()), "")
	lines = lines[:min(len(lines), h)]
	for i, line := range lines {
		line = ansi.Truncate(line, w, "")
		if l.plain {
			line = ansi.Strip(line)
		}
		if bg := c.frameBackground; bg != "" {
			line = paintBackground(line+strings.Repeat(" ", max(0, w-ansi.StringWidth(line))), bg)
		}
		lines[i] = line
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = !l.dumb
	if !l.dumb {
		view.WindowTitle = "meter"
		view.KeyboardEnhancements.ReportEventTypes = true
		view.KeyboardEnhancements.ReportAlternateKeys = true
		view.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
		view.KeyboardEnhancements.ReportAssociatedText = true
	}
	return view
}

func homePath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			return "~" + string(filepath.Separator) + rest
		}
	}
	return path
}

func plural(n int, word string) string {
	if n != 1 {
		word += "s"
	}
	return fmt.Sprintf("%d %s", n, word)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + strings.TrimPrefix(path, "~")
		}
	}
	return path
}
func completeDirectory(path string) (string, []string) {
	expanded := expandHome(path)
	dir, prefix := filepath.Dir(expanded), filepath.Base(expanded)
	if strings.HasSuffix(expanded, string(filepath.Separator)) {
		dir, prefix = expanded, ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return path, nil
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) && (!strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(prefix, ".")) {
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) == 1 {
		return filepath.Join(dir, matches[0]) + string(filepath.Separator), nil
	}
	return path, matches
}
