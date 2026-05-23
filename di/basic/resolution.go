package basic

import (
	"runtime"
	"strconv"
	"strings"

	"gochen/errors"
)

type resolutionState struct {
	stack []string
}

func (s *resolutionState) push(label string) {
	s.stack = append(s.stack, label)
}

func (s *resolutionState) pop() {
	if len(s.stack) == 0 {
		return
	}
	s.stack = s.stack[:len(s.stack)-1]
}

func (s *resolutionState) cyclePath(label string) []string {
	for i, existing := range s.stack {
		if existing == label {
			path := append([]string(nil), s.stack[i:]...)
			path = append(path, label)
			return path
		}
	}
	return []string{label}
}

func (s *resolutionState) intersects(labels []string) bool {
	if s == nil {
		return false
	}
	for _, label := range labels {
		for _, existing := range s.stack {
			if existing == label {
				return true
			}
		}
	}
	return false
}

func (s *resolutionState) cyclePathFromAny(labels []string) []string {
	for _, label := range labels {
		for i, existing := range s.stack {
			if existing == label {
				path := append([]string(nil), s.stack[i:]...)
				path = append(path, label)
				return path
			}
		}
	}
	return append([]string(nil), labels...)
}

func (c *Container) currentResolutionState() *resolutionState {
	if c == nil {
		return nil
	}
	gid, ok := currentGoroutineID()
	if !ok {
		return nil
	}
	state, ok := c.resolutions.Load(gid)
	if !ok {
		return nil
	}
	resolution, _ := state.(*resolutionState)
	return resolution
}

func (c *Container) withResolutionFrame(label string, fn func() (any, error)) (any, error) {
	if c == nil {
		return nil, errors.NewCode(errors.Internal, "container is nil")
	}
	if label == "" {
		return fn()
	}

	gid, ok := currentGoroutineID()
	if !ok {
		return fn()
	}

	stateAny, _ := c.resolutions.LoadOrStore(gid, &resolutionState{})
	state := stateAny.(*resolutionState)
	if path := state.cyclePath(label); len(path) > 1 {
		return nil, newCircularDependencyError(path)
	}

	state.push(label)
	defer func() {
		state.pop()
		if len(state.stack) == 0 {
			c.resolutions.Delete(gid)
		}
	}()

	return fn()
}

func newCircularDependencyError(path []string) error {
	return errors.NewCode(errors.Dependency, "circular dependency detected").
		WithContext("cycle", strings.Join(path, " -> "))
}

func currentGoroutineID() (uint64, bool) {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	line := strings.TrimSpace(string(buf[:n]))
	const prefix = "goroutine "
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}
	line = strings.TrimPrefix(line, prefix)
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx <= 0 {
		return 0, false
	}
	id, err := strconv.ParseUint(line[:spaceIdx], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
