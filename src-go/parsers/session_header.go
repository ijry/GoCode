package parsers

// session_header.go reads the `type:"session"` first line that pi and OpenClaw
// both write. It is the same shape for the two agents — version, id, timestamp,
// cwd — so the import pickers share one reader instead of two copies that could
// drift.

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// summaryHeadLimit bounds how much of a session file the listers read. These
// files are small (a transcript of a chat), but a head limit keeps a pathological
// one from stalling the import page.
const summaryHeadLimit = 192 * 1024

// sessionHeader is the metadata a `type:"session"` line carries.
type sessionHeader struct {
	ID        string
	Cwd       string
	Timestamp string
}

// readSessionHeader reads the header and, in the same pass, the first user
// message's text (used as the title). It stops as soon as both are known.
func readSessionHeader(path string, userText func(map[string]any) string) (sessionHeader, string) {
	f, err := os.Open(path)
	if err != nil {
		return sessionHeader{}, ""
	}
	defer f.Close()
	var hdr sessionHeader
	title := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	read := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		read += len(line) + 1
		if read > summaryHeadLimit {
			break
		}
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec == nil {
			continue
		}
		switch strOf(rec["type"]) {
		case "session":
			hdr.ID = strOf(rec["id"])
			hdr.Cwd = strOf(rec["cwd"])
			hdr.Timestamp = strOf(rec["timestamp"])
		case "message":
			if title != "" || userText == nil {
				continue
			}
			msg, _ := rec["message"].(map[string]any)
			if msg == nil || strOf(msg["role"]) != "user" {
				continue
			}
			title = summaryTitle(userText(msg))
		}
		if hdr.ID != "" && title != "" {
			break
		}
	}
	return hdr, title
}
