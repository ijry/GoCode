package handlers

// Agent install/uninstall family. prepare_npx_agent / install_uv_tool prime the
// launcher's package cache (best-effort real tool invocation, bounded by a
// timeout) and record the installed version; download_agent_binary needs the
// binary-cache/download subsystem (not yet ported) and reports that cleanly;
// uninstall_agent clears the recorded install. A task-progress event is emitted
// on the taskId channel so the settings UI can show status.

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"gocode/src-go/acp"
	appstate "gocode/src-go/app_state"
	"gocode/src-go/web"
)

const agentInstallTimeout = 10 * time.Minute

// NpmBinary is the npm launcher the pi install/uninstall routes shell out to.
// It is a package-level hook so tests (and embedded runtimes) can point it at a
// fake/alternate binary without touching PATH.
var NpmBinary = "npm"

// piCodingAgentPackage is the upstream PI_CODING_AGENT_PACKAGE: the third-party
// `@earendil-works/pi-coding-agent` npm package, installed globally as `pi`.
// It is a literal dependency (not a GoCode brand string), so it is NOT rebranded.
const piCodingAgentPackage = "@earendil-works/pi-coding-agent"

func RegisterACPInstall(reg *Registry) {
	reg.Register("acp_prepare_npx_agent", func(c *web.Context) (any, error) {
		d, err := requireDB(c)
		if err != nil {
			return nil, err
		}
		agentType := strVal(c.Body["agentType"], c.Body["agent_type"])
		taskID := strVal(c.Body["taskId"], c.Body["task_id"])
		// Upstream AcpPrepareNpxAgentParams: registry_version / version pin the
		// package; clean_first wipes the npx cache first (we have no cache to
		// wipe, so it's a no-op here, but accepted to stay wire-compatible).
		registryVer := strVal(c.Body["registryVersion"], c.Body["registry_version"])
		pinVer := strVal(c.Body["version"])
		_ = GetBool(c, "cleanFirst", "clean_first") // no cache to clear in this port
		spec, ok := acp.LaunchSpecFor(agentType)
		if !ok || spec.Kind != "npx" {
			return nil, web.ErrInvalidInput("not an npx agent: " + agentType)
		}
		if !lookPathOK("npx") {
			return nil, web.ErrDependencyMissing("npx (Node.js) is not installed")
		}
		emitAgentTask(c, taskID, agentType, "running", "")
		// Resolve the package spec, honoring the client's pin when given.
		pkg := spec.Package
		if registryVer != "" {
			pkg = spec.Package + "@" + registryVer
		} else if pinVer != "" {
			pkg = spec.Package + "@" + pinVer
		}
		// Prime the npx cache by resolving the package's bin once.
		ctx, cancel := context.WithTimeout(context.Background(), agentInstallTimeout)
		defer cancel()
		out, runErr := runToolVersion(ctx, "npx", "-y", "-p", pkg, spec.Cmd, "--version")
		version := registryVersion(pkg)
		if v := parseVersion(out); v != "" {
			version = v
		}
		if runErr != nil {
			emitAgentTask(c, taskID, agentType, "error", runErr.Error())
			return nil, web.ErrExternalCommand("npx prepare failed").WithDetail(runErr.Error())
		}
		upsertAgentSetting(d, agentType, map[string]any{"installed_version": version})
		emitAgentTask(c, taskID, agentType, "done", version)
		return version, nil
	})

	reg.Register("acp_install_uv_tool", func(c *web.Context) (any, error) {
		taskID := strVal(c.Body["taskId"], c.Body["task_id"])
		if !lookPathOK("uv") && !lookPathOK("uvx") {
			return nil, web.ErrDependencyMissing("uv (Astral uv) is not installed")
		}
		emitAgentTask(c, taskID, "pi", "running", "")
		ctx, cancel := context.WithTimeout(context.Background(), agentInstallTimeout)
		defer cancel()
		spec, _ := acp.LaunchSpecFor("pi")
		_, runErr := runToolVersion(ctx, "uvx", "--from", spec.Package, spec.Cmd, "--version")
		if runErr != nil {
			emitAgentTask(c, taskID, "pi", "error", runErr.Error())
			return nil, web.ErrExternalCommand("uv tool install failed").WithDetail(runErr.Error())
		}
		emitAgentTask(c, taskID, "pi", "done", "")
		return nil, nil
	})

	// acp_install_pi_binary: npm-global install of the pi coding agent (upstream
	// PI_CODING_AGENT_PACKAGE). Streams npm output as agent-task log events so
	// the settings panel shows the live install log.
	reg.Register("acp_install_pi_binary", func(c *web.Context) (any, error) {
		taskID := strVal(c.Body["taskId"], c.Body["task_id"])
		if !lookPathOK(NpmBinary) {
			return nil, web.ErrDependencyMissing("npm (Node.js) is not installed")
		}
		emitAgentTask(c, taskID, "pi", "running", "")
		ctx, cancel := context.WithTimeout(context.Background(), agentInstallTimeout)
		defer cancel()
		if err := runNpmGlobal(ctx, c, taskID, "pi", "install", "-g", piCodingAgentPackage); err != nil {
			emitAgentTask(c, taskID, "pi", "error", err.Error())
			return nil, web.ErrExternalCommand("npm install -g " + piCodingAgentPackage + " failed").WithDetail(err.Error())
		}
		emitAgentTask(c, taskID, "pi", "done", "pi installed successfully")
		return nil, nil
	})

	// acp_uninstall_pi_binary: npm-global uninstall of the pi coding agent.
	// Mirrors upstream's envelope: Started → Log(`$ npm uninstall -g …`) →
	// Completed/Failed.
	reg.Register("acp_uninstall_pi_binary", func(c *web.Context) (any, error) {
		taskID := strVal(c.Body["taskId"], c.Body["task_id"])
		if !lookPathOK(NpmBinary) {
			return nil, web.ErrDependencyMissing("npm (Node.js) is not installed")
		}
		emitAgentTask(c, taskID, "pi", "running", "")
		emitAgentTask(c, taskID, "pi", "running", "$ npm uninstall -g "+piCodingAgentPackage)
		ctx, cancel := context.WithTimeout(context.Background(), agentInstallTimeout)
		defer cancel()
		if err := runNpmGlobal(ctx, c, taskID, "pi", "uninstall", "-g", piCodingAgentPackage); err != nil {
			emitAgentTask(c, taskID, "pi", "error", err.Error())
			return nil, web.ErrExternalCommand("npm uninstall -g " + piCodingAgentPackage + " failed").WithDetail(err.Error())
		}
		emitAgentTask(c, taskID, "pi", "done", "pi uninstalled successfully")
		return nil, nil
	})

	reg.Register("acp_download_agent_binary", func(c *web.Context) (any, error) {
		// Binary agents (cursor/antigravity) download a per-platform archive,
		// extract it into the cache and launch the entry. The client's version
		// pin is honored only when it templates the pinned URL (the built-ins do
		// not), matching upstream's backstop.
		agentType := strVal(c.Body["agentType"], c.Body["agent_type"])
		version := strVal(c.Body["version"])
		taskID := strVal(c.Body["taskId"], c.Body["task_id"])
		if _, ok := acp.BinaryDistFor(agentType); !ok {
			return nil, web.ErrInvalidInput("not a binary agent: " + agentType)
		}
		cacheRoot := ""
		if st, ok := c.State.(*appstate.AppState); ok && st != nil {
			cacheRoot = st.DataDir
		}
		emitAgentTask(c, taskID, agentType, "running", "")
		path, err := acp.EnsureAgentBinary(agentType, cacheRoot, version, func(msg string) {
			emitAgentTask(c, taskID, agentType, "running", msg)
		})
		if err != nil {
			emitAgentTask(c, taskID, agentType, "error", err.Error())
			return nil, web.ErrExternalCommand("download failed").WithDetail(err.Error())
		}
		emitAgentTask(c, taskID, agentType, "done", path)
		Emit(c, "agents://changed", map[string]any{})
		return path, nil
	})

	reg.Register("acp_uninstall_agent", func(c *web.Context) (any, error) {
		d, err := requireDB(c)
		if err != nil {
			return nil, err
		}
		agentType := strVal(c.Body["agentType"], c.Body["agent_type"])
		taskID := strVal(c.Body["taskId"], c.Body["task_id"])
		if agentType == "" {
			return nil, web.ErrInvalidInput("agentType required")
		}
		if d != nil && d.Conn != nil {
			_, _ = d.Conn.Exec(`UPDATE agent_setting SET installed_version = NULL, updated_at = ? WHERE agent_type = ?`, nowRFC3339(), agentType)
		}
		emitAgentTask(c, taskID, agentType, "done", "")
		Emit(c, "agents://changed", map[string]any{})
		return nil, nil
	})
}

func runToolVersion(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func parseVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// A bare semver-ish line is the version; otherwise skip chatter.
		if t[0] >= '0' && t[0] <= '9' {
			return t
		}
	}
	return ""
}

// registryVersion extracts the pinned version from a package@version spec.
func registryVersion(pkg string) string {
	if i := strings.LastIndex(pkg, "@"); i > 0 {
		return pkg[i+1:]
	}
	return ""
}

// emitAgentTask broadcasts an agent install/uninstall progress event on the
// taskId channel the settings UI listens on.
func emitAgentTask(c *web.Context, taskID, agentType, status, detail string) {
	if taskID == "" {
		return
	}
	Emit(c, "acp-agent-task://"+taskID, map[string]any{
		"task_id": taskID, "agent_type": agentType, "status": status, "detail": detail,
	})
}

// runNpmGlobal runs `npm <args...>` (a global install/uninstall), streaming each
// merged stdout/stderr line as an agent-task progress event so the settings
// panel shows the live npm log. Matches upstream
// install_npm_global_package_streaming's Started → Log* → Completed/Failed
// envelope (uninstall collects via .output(), rendered as a single Log line
// plus the result). The command is bounded by ctx's deadline.
func runNpmGlobal(ctx context.Context, c *web.Context, taskID, agentType string, args ...string) error {
	cmd := exec.CommandContext(ctx, NpmBinary, args...)
	// Merge stderr into stdout via a single pipe so both streams surface in the
	// same ordered log. We assign an *os.File directly (not StdoutPipe), so Go's
	// (*Cmd).Wait() will NOT close it for us — we must close our copy after Wait
	// returns, otherwise the reader below never sees EOF and deadlocks.
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\r")
			if line != "" {
				emitAgentTask(c, taskID, agentType, "running", line)
			}
		}
	}()
	waitErr := cmd.Wait()
	// Child has released its copy of the write end; closing ours EOFs the reader.
	pw.Close()
	<-done
	return waitErr
}
