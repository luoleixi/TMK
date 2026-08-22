package health

import "sync"

type Checker func() bool

var (
	mu       sync.RWMutex
	ready    bool
	checkers = map[string]Checker{
		"asr":        nil,
		"translator": nil,
		"tts":        nil,
	}
)

// SetReady records whether the application has completed its core startup.
// This is intentionally separate from optional upstream service health.
func SetReady(value bool) {
	mu.Lock()
	ready = value
	mu.Unlock()
}

func Ready() bool {
	mu.RLock()
	defer mu.RUnlock()
	return ready
}

func Register(name string, fn Checker) {
	mu.Lock()
	defer mu.Unlock()
	checkers[name] = fn
}

func Services() map[string]bool {
	mu.RLock()
	registered := make(map[string]Checker, len(checkers))
	for name, fn := range checkers {
		registered[name] = fn
	}
	mu.RUnlock()
	result, _ := inspect(registered)
	return result
}

func Snapshot() (bool, string, map[string]bool, map[string]string) {
	mu.RLock()
	isReady := ready
	registered := make(map[string]Checker, len(checkers))
	for name, fn := range checkers {
		registered[name] = fn
	}
	mu.RUnlock()
	services, states := inspect(registered)
	return isReady, status(isReady, states), services, states
}

func Status() string {
	_, current, _, _ := Snapshot()
	return current
}

func inspect(registered map[string]Checker) (map[string]bool, map[string]string) {
	services := make(map[string]bool, len(registered))
	states := make(map[string]string, len(registered))
	for name, fn := range registered {
		if fn == nil {
			services[name] = false
			states[name] = "unknown"
			continue
		}
		ok := run(fn)
		services[name] = ok
		if ok {
			states[name] = "ok"
		} else {
			states[name] = "unavailable"
		}
	}
	return services, states
}

func run(fn Checker) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return fn()
}

func status(isReady bool, states map[string]string) string {
	if !isReady {
		return "starting"
	}
	for _, state := range states {
		if state == "unavailable" {
			return "degraded"
		}
	}
	return "ok"
}
