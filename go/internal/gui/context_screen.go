package gui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Cyenoch/mygoui/content"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
	palette "pi-workspace.local/workspace/internal/theme"
)

type contextScreen struct {
	conversation protocol.Conversation
	pane         protocol.LayoutNode
	cwd          string
}

func (a *app) openContextScreen(node protocol.LayoutNode, cwd string, p *pane) {
	if node.ConversationId == nil {
		return
	}
	conversation := protocol.Conversation{Id: *node.ConversationId, Cwd: cwd, Model: "unconfigured"}
	for _, item := range a.catalog.Conversations {
		if item.Id == *node.ConversationId {
			conversation = item
			break
		}
	}
	if p.state.Current.Selection != nil && p.state.Current.Selection.Model != nil {
		model := p.state.Current.Selection.Model.Provider + "/" + p.state.Current.Selection.Model.ModelId
		conversation.Model = model
	}
	a.contextOpen = &contextScreen{conversation: conversation, pane: node, cwd: cwd}
}

func contextStatus(p *pane) string {
	if p == nil {
		return "Detached"
	}
	if !p.connected {
		return "Reconnecting"
	}
	value := string(p.state.Current.Status)
	if value == "" {
		return "Idle"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func contextField(c *ui.Context, label, value, copyLabel string) {
	ui.Column(c.Key(label)).Gap(4).MinWidth(0).Children(func() {
		text(c, label, 11, muted).FontWeight(600)
		ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			text(c, value, 13, ink).Font(monoFont).Grow(1).MinWidth(0).SingleLine().Tooltip(value)
			if copyLabel != "" {
				content.CopyButton(c, content.CopyButtonOptions{Text: value, Label: copyLabel, Size: uitheme.ButtonExtraSmall})
			}
		})
	})
}

func contextMeter(c *ui.Context, usage *protocol.ContextUsage) {
	known := usage != nil && usage.Window > 0
	pct := 0.0
	if known {
		pct = palette.Percent(usage.Tokens, usage.Window)
	}
	ui.Column(c.Key("context-meter-detail")).Gap(10).Padding(14).Radius(8).Background(panel).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				text(c, "Active model context", 12, muted).FontWeight(600)
				if known {
					text(c, fmt.Sprintf("%s / %s tokens", formatCount(usage.Tokens), formatCount(usage.Window)), 18, ink).Font(monoFont).FontWeight(650)
				} else {
					text(c, "Not reported yet", 18, ink).FontWeight(650)
				}
			})
			if known {
				text(c, palette.PercentLabel(pct), 18, ink).Font(monoFont).FontWeight(650)
			}
		})
		ui.Row(c).Gap(4).FillWidth().Children(func() {
			for i := range palette.MeterCells {
				color := ui.Hex(palette.MeterCellColor(i, pct))
				ui.Box(c.Key(i)).Grow(1).Height(14).Radius(2).Background(color)
			}
		})
	})
}

type contextPart struct {
	label  string
	tokens int
	free   bool
}

func contextBreakdown(c *ui.Context, usage *protocol.ContextUsage) {
	if usage == nil || usage.Breakdown == nil {
		return
	}
	ui.Column(c.Key("context-breakdown")).Gap(8).Padding(12).Radius(8).Background(panel).Children(func() {
		text(c, "Context breakdown", 13, ink).FontWeight(650)
		breakdown := usage.Breakdown
		parts := []contextPart{
			{label: "System prompt", tokens: breakdown.SystemPrompt},
			{label: "Project instructions", tokens: breakdown.ProjectInstructions},
			{label: "Skills", tokens: breakdown.Skills},
			{label: "Tool definitions", tokens: breakdown.ToolDefinitions},
			{label: "User messages", tokens: breakdown.UserMessages},
			{label: "Assistant messages", tokens: breakdown.AssistantMessages},
			{label: "Tool results", tokens: breakdown.ToolResults},
			{label: "Free space", tokens: max(0, usage.Window-usage.Tokens), free: true},
		}
		visible := parts[:0]
		for _, part := range parts {
			if part.tokens > 0 || part.free {
				visible = append(visible, part)
			}
		}
		for i, part := range visible {
			pct := 0.0
			if usage.Window > 0 {
				pct = palette.Percent(part.tokens, usage.Window)
			}
			ui.Row(c.Key(i)).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				color := rain
				if part.free {
					color = theme.Border
				}
				ui.Box(c).Size(7, 7).Radius(4).Background(color).Shrink(0)
				text(c, part.label, 12, ink).Grow(1)
				text(c, formatCount(part.tokens), 12, muted).Font(monoFont).FontFeatures("tnum").Width(70)
				text(c, palette.PercentLabel(pct), 12, muted).Font(monoFont).FontFeatures("tnum").Width(42)
			})
		}
	})
}

func contextMetadata(c *ui.Context, a *app, screen *contextScreen, p *pane) {
	conversation := screen.conversation
	storage := a.backendInfo.Storage
	if storage == nil && a.clientData != "" {
		storage = &protocol.StorageInfo{
			DataDirectory: a.clientData,
			Conversations: filepath.Join(a.clientData, "durable.sqlite"),
			Workspace:     filepath.Join(a.clientData, "app.sqlite"),
		}
	}
	ui.Column(c.Key("session-meta")).Gap(14).Children(func() {
		if storage != nil {
			ui.Column(c.Key("context-storage")).Gap(10).Padding(12).Radius(8).Background(sidebar).Children(func() {
				text(c, "Storage", 13, ink).FontWeight(650)
				contextField(c, "Data directory", storage.DataDirectory, "Copy data directory")
				contextField(c, "Conversation database", storage.Conversations, "Copy conversation database path")
				contextField(c, "Workspace database", storage.Workspace, "Copy workspace database path")
			})
		}
		ui.Column(c.Key("context-details")).Gap(12).Padding(12).Radius(8).Background(panel).Children(func() {
			text(c, "Session", 13, ink).FontWeight(650)
			contextField(c, "Conversation ID", conversation.Id, "Copy conversation ID")
			contextField(c, "Working directory", screen.cwd, "Copy working directory")
			contextField(c, "Model", conversation.Model, "Copy model")
			contextField(c, "Status", contextStatus(p), "")
		})
	})
}

func (a *app) contextDialog(c *ui.Context) {
	screen := a.contextOpen
	if screen == nil {
		return
	}
	open := true
	ui.Modal(c.Key("conversation-context"), &open, func() {
		p := a.panes[screen.pane.Id]
		ui.Column(c).Width(640).Gap(14).Children(func() {
			text(c, "Context", 17, ink).FontWeight(650)
			var usage *protocol.ContextUsage
			if p != nil {
				usage = p.state.Current.ContextUsage
			}
			ui.Scroll(c).MaxHeight(520).Children(func() {
				ui.Column(c).Gap(14).Padding(0, 8, 0, 0).Children(func() {
					contextMeter(c, usage)
					contextMetadata(c, a, screen, p)
					contextBreakdown(c, usage)
				})
			})
		})
	})
	if !open {
		a.contextOpen = nil
	}
}
