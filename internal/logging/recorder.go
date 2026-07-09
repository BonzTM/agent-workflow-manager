package logging

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// Entry is a single log record captured by a Recorder: its level, event
// name, and structured fields.
type Entry struct {
	Level  string
	Event  string
	Fields map[string]any
}

// Recorder is a concurrency-safe Logger implementation that captures log
// records in memory for inspection, primarily in tests.
type Recorder struct {
	mu      sync.Mutex
	entries []Entry
}

// NewRecorder returns an empty Recorder ready to capture log records.
func NewRecorder() *Recorder {
	return &Recorder{entries: make([]Entry, 0)}
}

// Info records event and its key/value fields at level "info".
func (r *Recorder) Info(_ context.Context, event string, fields ...any) {
	r.append("info", event, fields...)
}

func (r *Recorder) Error(_ context.Context, event string, fields ...any) {
	r.append("error", event, fields...)
}

// Entries returns a deep copy of all captured records in the order they
// were logged; mutating the result does not affect the Recorder.
func (r *Recorder) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Entry, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, Entry{
			Level:  entry.Level,
			Event:  entry.Event,
			Fields: cloneFields(entry.Fields),
		})
	}
	return out
}

// Reset discards all captured records.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = r.entries[:0]
}

func (r *Recorder) append(level, event string, fields ...any) {
	if r == nil {
		return
	}
	entry := Entry{
		Level:  level,
		Event:  event,
		Fields: fieldsMap(fields...),
	}

	r.mu.Lock()
	r.entries = append(r.entries, entry)
	r.mu.Unlock()
}

func fieldsMap(fields ...any) map[string]any {
	if len(fields) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(fields)/2+1)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok || key == "" {
			key = fmt.Sprintf("field_%d", i)
		}
		out[key] = fields[i+1]
	}
	if len(fields)%2 != 0 {
		out["field_unpaired"] = fields[len(fields)-1]
	}
	return out
}

func cloneFields(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	maps.Copy(out, in)
	return out
}
