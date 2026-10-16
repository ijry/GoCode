package app

// detail_tabs.go is the workspace main area's tab strip: the conversation and
// the terminal. The folder-scoped panels (files, git changes, commit history)
// live in the right-hand aux panel instead (see aux_panel.go), matching
// upstream's workspace skeleton (Sidebar | main | AuxPanel).

import "github.com/egoist/mygo/ui"

func (s *Shell) viewDetailTabs(c *ui.Context) {
	if s.detailTab == "" {
		s.detailTab = "chat"
	}
	ui.Row(c.Key("detail-tabs")).Gap(4).AlignItems(ui.Center).Children(func() {
		for _, tab := range []struct{ id, label string }{
			{"chat", "Chat"},
			{"terminal", "Terminal"},
		} {
			active := s.detailTab == tab.id
			var clicked bool
			if active {
				clicked = ui.PrimaryButton(c.Key("dt-"+tab.id), tab.label).Clicked()
			} else {
				clicked = ui.Button(c.Key("dt-"+tab.id), tab.label).Clicked()
			}
			if clicked {
				s.detailTab = tab.id
			}
		}
	})
	ui.Divider(c)
	switch s.detailTab {
	case "terminal":
		s.viewTerminal(c)
	default:
		s.viewConversationDetail(c)
	}
}
