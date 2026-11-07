package runner

import (
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
type sessionStore struct {
	mu   sync.Mutex
	path string
	ids  map[string]string
}

func newSessionStore(workspace string) *sessionStore {
	s := &sessionStore{path: filepath.Join(workspace, ".paca-bridge", "sessions.json"), ids: map[string]string{}}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.ids)
	}
	return s
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
	if id == "" {
		delete(s.ids, k)
	} else if s.ids[k] == id {
		return
	} else {
		s.ids[k] = id
	}
	b, err := json.Marshal(s.ids)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(s.path), 0o755) != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}
