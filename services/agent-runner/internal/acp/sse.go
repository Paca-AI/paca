package acp

import (
	"bufio"
	"io"
	"strings"
)

// sseReader pulls one event's `data:` payload at a time off an SSE stream.
// Minimal on purpose: goose serve's /acp responses were only ever observed
// using `data:` lines (one per event) and bare `: ` comment/keepalive
// lines — no `event:`/`id:`/`retry:` fields — but per the SSE spec, a
// single event's data can legitimately span multiple `data:` lines (joined
// by "\n"), so that case is still handled. Any other field name is ignored
// rather than rejected, so an unexpected field added upstream doesn't break
// parsing of the fields we do care about.
type sseReader struct {
	r *bufio.Reader
}

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReader(r)}
}

// Next returns the next event's joined data payload. Returns io.EOF once
// the underlying stream is exhausted with no more events pending.
func (s *sseReader) Next() ([]byte, error) {
	var data []string
	for {
		line, err := s.r.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")

		if trimmed == "" {
			// Blank line: end of this event. Only treat it as one if any
			// data was actually accumulated — bare blank lines between
			// events (or a trailing one before EOF) carry nothing.
			if len(data) > 0 {
				return []byte(strings.Join(data, "\n")), nil
			}
			if err != nil {
				return nil, err
			}
			continue
		}

		if val, ok := strings.CutPrefix(trimmed, "data:"); ok {
			data = append(data, strings.TrimPrefix(val, " "))
		}
		// Comment lines (leading ':') and any other field are ignored.

		if err != nil {
			if len(data) > 0 {
				return []byte(strings.Join(data, "\n")), nil
			}
			return nil, err
		}
	}
}
