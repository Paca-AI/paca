package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// sessionStore persists ACP session ids (keyed by session key + provider) to
// disk so a respawned subprocess — after idle eviction, a forced kill or a
// bridge restart — can session/load the old session and keep its history
// instead of starting a blank one with session/new. Best-effort: any I/O
// error just means a fresh session.
//
// The file lives outside the workspace (the user's repo), in a directory
// scoped by workspace path and agent id, so two daemons never share a file.
type sessionStore struct {
	mu   sync.Mutex
	path string // "" = in-memory only
	ids  map[string]string
}

// storePath returns <stateDir>/<hash(workspace)>/<agentID>/sessions.json. An
// empty stateDir falls back to the OS user config dir; "" is returned if
// neither is available.
func storePath(stateDir, workspace, agentID string) string {
	if stateDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		stateDir = filepath.Join(dir, "paca-acp-bridge")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		abs = workspace
	}
	sum := sha256.Sum256([]byte(abs))
	if agentID == "" {
		agentID = "default"
	}
	return filepath.Join(stateDir, hex.EncodeToString(sum[:])[:16], filepath.Base(agentID), "sessions.json")
}

func newSessionStore(path string) *sessionStore {
	s := &sessionStore{path: path, ids: map[string]string{}}
	s.load()
	return s
}

func (s *sessionStore) load() {
	if s.path == "" {
		return
	}
	if b, err := os.ReadFile(s.path); err == nil {
		var m map[string]string
		if json.Unmarshal(b, &m) == nil {
			s.ids = m
		}
	}
}

func storeKey(provider, key string) string { return provider + "|" + key }

func (s *sessionStore) get(provider, key string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ids[storeKey(provider, key)]
}

func (s *sessionStore) set(provider, key, id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := storeKey(provider, key)
	if s.ids[k] == id {
		return
	}
	// Merge with whatever is on disk now so a concurrent writer's entries
	// aren't clobbered by our startup snapshot.
	s.load()
	if id == "" {
		delete(s.ids, k)
	} else {
		s.ids[k] = id
	}
	if s.path == "" {
		return
	}
	b, err := json.Marshal(s.ids)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(s.path), 0o700) != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}
