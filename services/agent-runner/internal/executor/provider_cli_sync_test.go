package executor

import (
	"archive/tar"
	"io"
	"strings"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/executor/providercli"
)

// Regression guard for a live bug: the CLI config was extracted at
// /home/goose through the ~/.claude symlink (an absolute link onto the
// persistent volume). Newer Docker daemons resolve an absolute symlink met
// during extraction against the extraction directory, so the file landed in
// /home/goose/home/paca/workspaces/... and the CLI never saw the agent's MCP
// servers (the Paca one included). Entries must name the real volume path and
// be extracted from "/".
func TestPersistedCLIEntries_UseTheRealVolumePathNotTheSymlink(t *testing.T) {
	for _, provider := range []string{"claude-code", "codex", "cursor-agent", "gemini-cli"} {
		adapter, ok := providercli.Get(provider)
		if !ok {
			t.Fatalf("no adapter for %s", provider)
		}
		files, err := adapter.SyncFiles(map[string]string{}, []agent.MCPServer{{ServerName: "paca", Transport: "stdio", Command: "/usr/bin/node", IsEnabled: true}}, nil)
		if err != nil {
			t.Fatalf("%s: SyncFiles: %v", provider, err)
		}
		entries, exclude := persistedCLIEntries(adapter, files)
		want := "home/paca/workspaces/.cli-home/" + provider + "/"
		if len(entries) != len(files) || len(entries) == 0 {
			t.Fatalf("%s: %d entries for %d files", provider, len(entries), len(files))
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.RelPath, want) {
				t.Errorf("%s: entry %q should live under %q", provider, e.RelPath, want)
			}
			if strings.Contains(e.RelPath, adapter.HomeDirName()+"/") && !strings.HasPrefix(e.RelPath, want) {
				t.Errorf("%s: entry %q still goes through the %s symlink", provider, e.RelPath, adapter.HomeDirName())
			}
		}
		// The volume path exists already: no directory header may be written for
		// it (or its ancestors), only for new directories below it.
		for _, dir := range []string{"home", "home/paca", "home/paca/workspaces", "home/paca/workspaces/.cli-home", "home/paca/workspaces/.cli-home/" + provider, "home/goose"} {
			if !exclude[dir] {
				t.Errorf("%s: %q should be excluded from generated directory entries", provider, dir)
			}
		}
	}
}

func TestPersistedCLIEntries_NestedFilesKeepTheirSubdirectories(t *testing.T) {
	adapter, _ := providercli.Get("claude-code")
	files := []providercli.FileEntry{
		{RelPath: ".claude/.claude.json", Content: "{}"},
		{RelPath: ".claude/skills/paca-do/SKILL.md", Content: "---\nname: paca-do\n---\nBody."},
	}
	entries, exclude := persistedCLIEntries(adapter, files)
	buf, err := buildFileTar(entries, exclude)
	if err != nil {
		t.Fatal(err)
	}
	dirs, names := map[string]bool{}, map[string]bool{}
	tr := tar.NewReader(buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeDir {
			dirs[strings.TrimSuffix(hdr.Name, "/")] = true
		} else {
			names[hdr.Name] = true
		}
	}
	base := "home/paca/workspaces/.cli-home/claude-code"
	if !names[base+"/.claude.json"] || !names[base+"/skills/paca-do/SKILL.md"] {
		t.Fatalf("files = %v", names)
	}
	// Only the new directories under the volume path get headers.
	for d := range dirs {
		if d != base+"/skills" && d != base+"/skills/paca-do" {
			t.Errorf("unexpected directory header %q (existing directories must not be rewritten)", d)
		}
	}
	if !dirs[base+"/skills"] || !dirs[base+"/skills/paca-do"] {
		t.Errorf("dirs = %v", dirs)
	}
}
