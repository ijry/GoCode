package app

// terminal.go mirrors upstream's workspace terminal: a toggled bottom panel
// shared by every conversation tab, holding an ordered strip of PTY tabs
// (components/terminal/terminal-panel.tsx + terminal-tab-bar.tsx +
// contexts/terminal-context.tsx). Each tab keeps the folder and working
// directory it was opened in, so a shell opened in a worktree stays bound to
// that worktree while the selection moves elsewhere.
//
// The body is mygo's terminal plugin (libghostty-vt) — a real terminal
// emulator, not the text replay it started as — wired to the backend PTY the
// same way upstream's xterm.js is: the emulator is the view, the process lives
// on the backend. ptyConn is that bridge (terminal.Options.Conn): what the
// emulator types goes out as terminal_write, what the firehose pushes on
// terminal://output/{id} is read by the emulator (after a terminal_snapshot
// priming so output from before the attach shows), the view's size reaches
// terminal_resize, and the backend's terminal://exit ends the stream so the
// view shows [Process exited].

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// ptyChunk is one buffered VT chunk with its scrollback sequence.
type ptyChunk struct {
	seq  uint64
	data []byte
}

// ptyConn bridges a terminal view to one backend PTY. The plugin's reader
// goroutine calls Read (blocked until the firehose pushes a chunk); the view
// calls Write per keystroke (queued, posted in order off the UI thread) and
// Resize per layout (coalesced, latest wins).
type ptyConn struct {
	id string
	b  *Backend

	mu       sync.Mutex
	cond     *sync.Cond
	cur      []byte // chunk being drained into Read's buffer (reader-only)
	queue    []ptyChunk
	pending  []ptyChunk // chunks seen before the snapshot priming
	primeBuf []byte     // snapshot bytes to deliver first
	seq      uint64     // last delivered live chunk seq
	primed   bool
	eof      bool
	closed   bool

	writeCh  chan []byte    // keystrokes/pastes, posted in order
	resizeCh chan [2]uint16 // latest size wins
	once     sync.Once
}

func newPtyConn(id string, b *Backend) *ptyConn {
	c := &ptyConn{
		id: id, b: b,
		writeCh:  make(chan []byte, 256),
		resizeCh: make(chan [2]uint16, 1),
	}
	c.cond = sync.NewCond(&c.mu)
	go c.writeLoop()
	go c.resizeLoop()
	return c
}

// prime stores the snapshot taken right after spawn: its bytes are delivered
// to the emulator first, then the chunks the firehose buffered meanwhile (the
// ones past the snapshot's seq; the rest is already inside the snapshot).
func (c *ptyConn) prime(data string, seq uint64) {
	c.mu.Lock()
	c.primeBuf = []byte(data)
	c.seq = seq
	c.primed = true
	for _, ch := range c.pending {
		if ch.seq > seq {
			c.queue = append(c.queue, ch)
		}
	}
	c.pending = nil
	c.cond.Broadcast()
	c.mu.Unlock()
}

// push delivers one firehose chunk. Before the priming it buffers (with its
// seq); after, it appends in order, skipping duplicates.
func (c *ptyConn) push(data string, seq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eof || c.closed {
		return
	}
	if !c.primed {
		c.pending = append(c.pending, ptyChunk{seq: seq, data: []byte(data)})
		return
	}
	if seq != 0 && seq <= c.seq {
		return
	}
	c.seq = seq
	c.queue = append(c.queue, ptyChunk{seq: seq, data: []byte(data)})
	c.cond.Broadcast()
}

// endOfStream marks the backend side gone (terminal://exit or teardown): Read
// drains what is left, then returns EOF, and the view shows [Process exited].
func (c *ptyConn) endOfStream() {
	c.mu.Lock()
	c.eof = true
	c.cond.Broadcast()
	c.mu.Unlock()
}

// Read is the emulator's side of the bridge: snapshot bytes first, then live
// chunks in order, blocking between them, EOF after the stream ends.
func (c *ptyConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	for len(c.cur) == 0 {
		switch {
		case len(c.primeBuf) > 0:
			c.cur, c.primeBuf = c.primeBuf, nil
		case len(c.queue) > 0:
			c.cur, c.queue = c.queue[0].data, c.queue[1:]
		case c.eof || c.closed:
			c.mu.Unlock()
			return 0, io.EOF
		default:
			c.cond.Wait()
		}
	}
	c.mu.Unlock()
	n := copy(p, c.cur)
	c.cur = c.cur[n:]
	return n, nil
}

// Write queues bytes for the PTY's stdin. The plugin calls it on the main
// thread, so the HTTP round trip happens on the worker instead.
func (c *ptyConn) Write(p []byte) (int, error) {
	if c.isClosed() {
		return 0, io.ErrClosedPipe
	}
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case c.writeCh <- buf:
	default:
		// The worker is wedged on a wedged backend; drop rather than hang
		// the view (the emulator's [Process exited] tells the rest).
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

func (c *ptyConn) writeLoop() {
	for buf := range c.writeCh {
		_, _ = c.b.raw("terminal_write", map[string]any{"terminalId": c.id, "data": string(buf)})
	}
}

// Resize forwards the view's fitted size to the backend pseudo-console.
// Coalesced latest-wins: a resize burst leaves only the newest queued.
func (c *ptyConn) Resize(cols, rows int) error {
	if c.isClosed() || cols <= 0 || rows <= 0 {
		return nil
	}
	select { // drain a stale size, then queue the newest
	case <-c.resizeCh:
	default:
	}
	select {
	case c.resizeCh <- [2]uint16{uint16(cols), uint16(rows)}:
	default:
	}
	return nil
}

func (c *ptyConn) resizeLoop() {
	for size := range c.resizeCh {
		_, _ = c.b.raw("terminal_resize", map[string]any{
			"terminalId": c.id, "cols": size[0], "rows": size[1],
		})
	}
}

// Close stops the workers; the plugin calls it off terminal.Close, and a
// closed conn refuses further writes and reads (EOF).
func (c *ptyConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.cond.Broadcast()
		c.mu.Unlock()
		close(c.writeCh)
		close(c.resizeCh)
	})
	return nil
}

func (c *ptyConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// termTab is one terminal tab (upstream's TerminalTab). ID doubles as the PTY
// id: it is supplied to terminal_spawn so the tab and its process share one
// stable key. Err keeps a failed spawn (or emulator start) visible in that tab
// instead of silently dropping it; Term is the emulator once started.
type termTab struct {
	ID         string
	Title      string
	FolderID   string
	WorkingDir string
	Err        string
	Term       *terminal.Terminal
	Renamed    bool // the user named it; OSC titles no longer overwrite
}

// termTabIndexLocked returns the slot of a tab, or -1. Call with s.mu held.
func (s *Shell) termTabIndexLocked(id string) int {
	for i := range s.termTabs {
		if s.termTabs[i].ID == id {
			return i
		}
	}
	return -1
}

// termConnLocked returns the bridge of a tab, or nil. Call with s.mu held.
func (s *Shell) termConnLocked(id string) *ptyConn {
	if s.termConns == nil {
		return nil
	}
	return s.termConns[id]
}

// pushTerminalChunk feeds one firehose output chunk to a tab's bridge. It runs
// on the stream's goroutine; an unknown id (not yet spawned, or closed) drops.
func (s *Shell) pushTerminalChunk(id, data string, seq uint64) {
	s.mu.Lock()
	conn := s.termConnLocked(id)
	s.mu.Unlock()
	if conn != nil {
		conn.push(data, seq)
	}
}

// terminalEnded marks a tab's bridge at EOF after terminal://exit.
func (s *Shell) terminalEnded(id string) {
	s.mu.Lock()
	conn := s.termConnLocked(id)
	s.mu.Unlock()
	if conn != nil {
		conn.endOfStream()
	}
}

// viewTerminalPanel renders the bottom panel. Collapsed, it is a single strip
// that reopens the panel; expanded, that strip becomes upstream's tab bar
// (tabs + new + hide) with the active tab's emulator below it.
func (s *Shell) viewTerminalPanel(c *ui.Context) {
	t := c.Theme()
	s.termInputFocused = false
	panel := ui.Column(c.Key("term-panel")).FillWidth().Background(t.Surface)
	panel.Children(func() {
		if s.termOpen {
			s.viewTerminalTabBar(c)
		} else {
			s.viewTerminalCollapsedBar(c)
		}
		if s.termOpen {
			ui.Divider(c)
			ui.Column(c.Key("term-body")).FillWidth().Height(240).Children(func() {
				s.viewTerminalBody(c)
			})
		}
	})
	s.termHovered = panel.Hovered()
}

// viewTerminalCollapsedBar is the always-visible collapsed state: the chevron
// plus the panel title. Clicking anywhere on it expands the panel.
func (s *Shell) viewTerminalCollapsedBar(c *ui.Context) {
	t := c.Theme()
	title := cat(c).ConvTabTerminal()
	if root := s.folderPathByID(s.selectedFolder); root != "" {
		title = title + " · " + root
	}
	s.mu.Lock()
	count := len(s.termTabs)
	s.mu.Unlock()
	toggle := ui.Row(c.Key("term-toggle")).FillWidth().PaddingX(12).PaddingY(6).Gap(6).
		AlignItems(ui.Center).Label(cat(c).TerminalToggleLabel())
	toggle.Children(func() {
		ui.Icon(c, iconChevron).Width(13).Height(13).TextColor(t.TextMuted)
		ui.Text(c, title).FontSize(12).Bold()
		if count > 0 {
			ui.Text(c, fmt.Sprintf("%d", count)).FontSize(11).TextColor(t.TextMuted)
		}
	})
	if toggle.Clicked() {
		s.termOpen = true
	}
}

// viewTerminalTabBar renders upstream's 32px strip: the collapse chevron, the
// horizontally scrolling tab list, then the new-tab action.
func (s *Shell) viewTerminalTabBar(c *ui.Context) {
	t := c.Theme()
	s.mu.Lock()
	tabs := append([]termTab(nil), s.termTabs...)
	active := s.termActive
	s.mu.Unlock()

	ui.Row(c.Key("term-head")).FillWidth().Background(t.SurfacePressed).
		Padding(2, 6, 2, 6).Gap(4).AlignItems(ui.Center).Children(func() {
		iconButton(c, "term-collapse", iconChevronD, cat(c).TerminalToggleLabel(), func() {
			s.termOpen = false
		})
		if len(tabs) > 0 {
			ui.ScrollHorizontal(c.Key("term-tabs-scroll")).Grow(1).Children(func() {
				ui.Row(c.Key("term-tabs")).Gap(2).AlignItems(ui.Center).Children(func() {
					for _, tab := range tabs {
						s.viewTerminalTab(c, tab, tab.ID == active)
					}
				})
			})
		} else {
			ui.Row(c).Grow(1).PaddingX(6).Children(func() {
				ui.Text(c, cat(c).TerminalNoTabs()).FontSize(12).TextColor(t.TextMuted)
			})
		}
		// New tab opens in the selected folder, like upstream's createTerminal.
		root := s.folderPathByID(s.selectedFolder)
		add := ui.Row(c.Key("term-new")).Width(24).Height(24).Radius(5).
			AlignItems(ui.Center).Justify(ui.Center).Label(cat(c).TerminalNewTab())
		if root == "" {
			add.Tooltip(cat(c).TerminalOpenFolderFirst())
		}
		add.Children(func() {
			ui.Icon(c, iconPlus).Width(13).Height(13).TextColor(condColor(root != "", t.TextMuted, t.TextMuted.Alpha(0.4)))
		})
		if add.Clicked() && root != "" {
			s.spawnTerminal(root)
		}
	})
}

// viewTerminalTab renders one tab: the folder-aware tooltip, the title, and a
// close affordance, with upstream's close verbs on a right-click. While
// renaming, the title yields to an inline input with save/cancel buttons.
func (s *Shell) viewTerminalTab(c *ui.Context, tab termTab, active bool) {
	t := c.Theme()
	if s.termRenameID == tab.ID {
		ui.Row(c.Key("term-tab-rename-" + tab.ID)).Gap(3).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("term-rename-input"), &s.termRenameDraft).
				Placeholder(cat(c).TerminalRenamePlaceholder()).Width(120)
			iconButton(c, "term-rename-save", iconCheck, cat(c).TerminalSaveName(), func() {
				s.commitTerminalRename(tab.ID)
			})
			iconButton(c, "term-rename-cancel", iconX, cat(c).TerminalCancelRename(), func() {
				s.termRenameID = ""
			})
		})
		return
	}

	card := ui.Row(c.Key("term-tab-"+tab.ID)).Padding(2, 8, 2, 5).Gap(4).
		Radius(5).MaxWidth(190).AlignItems(ui.Center)
	if active {
		card.Background(t.Background)
	}
	if folder := s.folderNameByID(tab.FolderID); folder != "" {
		card.Tooltip(folder + " — " + tab.Title)
	} else {
		card.Tooltip(tab.Title)
	}
	card.Children(func() {
		ui.Text(c, tab.Title).FontSize(12).MaxLines(1).
			TextColor(condColor(active, t.Text, t.TextMuted))
		closeBtn := ui.Row(c.Key("term-tab-x-" + tab.ID)).Width(16).Height(16).Radius(4).
			AlignItems(ui.Center).Justify(ui.Center).
			Label(cat(c).TerminalCloseTabNamed(tab.Title)).Tooltip(cat(c).TerminalCloseTabNamed(tab.Title))
		closeBtn.Children(func() {
			ui.Icon(c, iconX).Width(10).Height(10).TextColor(t.TextMuted)
		})
		if closeBtn.Clicked() {
			s.closeTerminalTab(tab.ID)
		}
	})
	card.ContextMenu(func(m *ui.Menu) {
		if m.Item(cat(c).TerminalRenameTab()).Chosen() {
			s.mu.Lock()
			s.termRenameID = tab.ID
			s.termRenameDraft = tab.Title
			s.mu.Unlock()
		}
		m.Separator()
		if m.Item(cat(c).TerminalCloseTab()).Chosen() {
			s.closeTerminalTab(tab.ID)
		}
		if m.Item(cat(c).TerminalCloseOthers()).Disabled(len(s.termTabs) <= 1).Chosen() {
			s.closeOtherTerminalTabs(tab.ID)
		}
		if m.Item(cat(c).TerminalCloseAll()).Chosen() {
			s.closeAllTerminalTabs()
		}
	})
	if card.Clicked() {
		s.switchTerminalTab(tab.ID)
	}
}

// viewTerminalBody renders the no-folder hint, the start prompt, or the active
// tab's emulator. The emulator takes keys once clicked (termInputFocused routes
// the shared ⌘T/⌘W chords); keystrokes reach the PTY as terminal_write.
func (s *Shell) viewTerminalBody(c *ui.Context) {
	t := c.Theme()
	root := s.folderPathByID(s.selectedFolder)
	if s.selectedFolder == "" || root == "" {
		ui.Text(c, cat(c).TerminalNoFolder()).FontSize(12).TextColor(t.TextMuted)
		return
	}

	s.mu.Lock()
	idx := s.termTabIndexLocked(s.termActive)
	var tab termTab
	if idx >= 0 {
		tab = s.termTabs[idx]
	}
	s.mu.Unlock()

	if idx < 0 {
		if ui.PrimaryButton(c.Key("term-start"), cat(c).TerminalStart()).Clicked() {
			s.spawnTerminal(root)
		}
		ui.Text(c, cat(c).TerminalStartHint(root)).FontSize(12).TextColor(t.TextMuted).Wrap()
		return
	}
	if tab.Err != "" {
		ui.Text(c, cat(c).TerminalError(tab.Err)).FontSize(12).TextColor(t.Danger).Wrap()
		return
	}
	if tab.Term == nil {
		loadingState(c, "")
		return
	}
	// A terminal shows in one view at a time; the tab switch simply points the
	// view at another emulator, whose program and scrollback kept running.
	v := terminal.View(c, tab.Term).Grow(1).Fill()
	s.termInputFocused = v.Focused()
}

// spawnTerminal starts a PTY in root as a new "Terminal N" tab.
func (s *Shell) spawnTerminal(root string) {
	s.newTerminalTab("", root, "")
}

// newTerminalTab appends a tab bound to root, focuses it and starts its PTY.
// initial, when non-empty, is handed to the shell at spawn (upstream's
// createTerminalWithCommand). The bridge registers before the spawn so no
// output is missed, then primes from a snapshot and starts the emulator. A
// failed spawn or emulator start stays as an error inside the tab.
func (s *Shell) newTerminalTab(title, root, initial string) string {
	if root == "" {
		return ""
	}
	svc := s.svc
	s.mu.Lock()
	s.termCounter++
	id := fmt.Sprintf("term-%d", s.termCounter)
	if title == "" {
		title = fmt.Sprintf("Terminal %d", s.termCounter)
	}
	s.termTabs = append(s.termTabs, termTab{
		ID: id, Title: title, FolderID: s.selectedFolder, WorkingDir: root,
	})
	s.termActive = id
	s.termOpen = true
	if s.termConns == nil {
		s.termConns = map[string]*ptyConn{}
	}
	conn := newPtyConn(id, s.B)
	s.termConns[id] = conn
	s.mu.Unlock()

	go func() {
		_, err := s.B.raw("terminal_spawn", map[string]any{
			"terminalId": id, "workingDir": root, "initialCommand": initial,
		})
		if err != nil {
			s.mu.Lock()
			if i := s.termTabIndexLocked(id); i >= 0 {
				s.termTabs[i].Err = err.Error()
			}
			s.mu.Unlock()
			conn.Close()
			s.mu.Lock()
			delete(s.termConns, id)
			s.mu.Unlock()
			svc.Invalidate()
			return
		}
		// Prime from the snapshot so output from before this attach shows,
		// then hand the bridge to a fresh emulator.
		var seq uint64
		var data string
		if raw, err := s.B.raw("terminal_snapshot", map[string]any{"terminalId": id}); err == nil {
			var snap struct {
				Data string `json:"data"`
				Seq  uint64 `json:"seq"`
			}
			if json.Unmarshal(raw, &snap) == nil {
				data, seq = snap.Data, snap.Seq
			}
		}
		conn.prime(data, seq)
		term, err := terminal.New(terminal.Options{
			Conn: conn,
			OnTitle: func(title string) {
				if title == "" {
					return
				}
				s.mu.Lock()
				if i := s.termTabIndexLocked(id); i >= 0 && !s.termTabs[i].Renamed {
					s.termTabs[i].Title = title
				}
				s.mu.Unlock()
				svc.Invalidate()
			},
		})
		s.mu.Lock()
		if i := s.termTabIndexLocked(id); i >= 0 {
			if err != nil {
				s.termTabs[i].Err = err.Error()
			} else {
				s.termTabs[i].Term = term
			}
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
	return id
}

// closeTerminalTab removes one tab and kills its PTY. Upstream focuses the tab
// that slid into the closed slot, else the last one; closing the last tab
// collapses the panel.
func (s *Shell) closeTerminalTab(id string) {
	s.mu.Lock()
	i := s.termTabIndexLocked(id)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	term := s.termTabs[i].Term
	s.termTabs = append(s.termTabs[:i:i], s.termTabs[i+1:]...)
	if s.termRenameID == id {
		s.termRenameID = ""
	}
	if len(s.termTabs) == 0 {
		s.termActive = ""
		s.termOpen = false
	} else if s.termActive == id {
		if i < len(s.termTabs) {
			s.termActive = s.termTabs[i].ID
		} else {
			s.termActive = s.termTabs[len(s.termTabs)-1].ID
		}
	}
	conn := s.termConnLocked(id)
	delete(s.termConns, id)
	s.mu.Unlock()
	if conn != nil {
		conn.endOfStream() // the emulator stops reading; Close frees the rest
		conn.Close()
	}
	if term != nil {
		_ = term.Close()
	}
	s.killTerminalPTY(id)
}

// closeOtherTerminalTabs keeps only the named tab (upstream's "Close Others").
func (s *Shell) closeOtherTerminalTabs(id string) {
	s.mu.Lock()
	i := s.termTabIndexLocked(id)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	type dead struct {
		id   string
		term *terminal.Terminal
	}
	var killIDs []string
	var deadTerms []dead
	for _, tab := range s.termTabs {
		if tab.ID != id {
			killIDs = append(killIDs, tab.ID)
			deadTerms = append(deadTerms, dead{tab.ID, tab.Term})
		}
	}
	if len(s.termTabs) > 1 {
		s.termTabs = []termTab{s.termTabs[i]}
	}
	s.termActive = id
	s.termOpen = true
	s.termRenameID = ""
	s.mu.Unlock()
	for _, d := range deadTerms {
		if conn := s.takeTerminalConn(d.id); conn != nil {
			conn.endOfStream()
			conn.Close()
		}
		if d.term != nil {
			_ = d.term.Close()
		}
	}
	for _, kid := range killIDs {
		s.killTerminalPTY(kid)
	}
}

// closeAllTerminalTabs empties the strip and collapses the panel.
func (s *Shell) closeAllTerminalTabs() {
	s.mu.Lock()
	if len(s.termTabs) == 0 {
		s.mu.Unlock()
		return
	}
	type dead struct {
		id   string
		term *terminal.Terminal
	}
	var ids []string
	var deadTerms []dead
	for _, tab := range s.termTabs {
		ids = append(ids, tab.ID)
		deadTerms = append(deadTerms, dead{tab.ID, tab.Term})
	}
	s.termTabs = nil
	s.termActive = ""
	s.termOpen = false
	s.termRenameID = ""
	s.mu.Unlock()
	for _, d := range deadTerms {
		if conn := s.takeTerminalConn(d.id); conn != nil {
			conn.endOfStream()
			conn.Close()
		}
		if d.term != nil {
			_ = d.term.Close()
		}
	}
	for _, id := range ids {
		s.killTerminalPTY(id)
	}
}

// takeTerminalConn unregisters and returns a tab's bridge, or nil.
func (s *Shell) takeTerminalConn(id string) *ptyConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn := s.termConnLocked(id)
	delete(s.termConns, id)
	return conn
}

// switchTerminalTab focuses one tab and leaves any rename in progress.
func (s *Shell) switchTerminalTab(id string) {
	s.mu.Lock()
	if s.termTabIndexLocked(id) < 0 {
		s.mu.Unlock()
		return
	}
	s.termActive = id
	s.termRenameID = ""
	s.mu.Unlock()
}

// commitTerminalRename applies a non-empty inline rename; renamed tabs no
// longer follow the program's OSC titles.
func (s *Shell) commitTerminalRename(id string) {
	title := strings.TrimSpace(s.termRenameDraft)
	if title == "" {
		return
	}
	s.mu.Lock()
	if i := s.termTabIndexLocked(id); i >= 0 {
		s.termTabs[i].Title = title
		s.termTabs[i].Renamed = true
	}
	s.termRenameID = ""
	s.mu.Unlock()
}

// killTerminalPTY asks the backend to terminate one PTY. The tab is already
// gone from the strip; this only reaps the process.
func (s *Shell) killTerminalPTY(id string) {
	svc := s.svc
	go func() {
		_, _ = s.B.raw("terminal_kill", map[string]any{"terminalId": id})
		svc.Invalidate()
	}()
}
