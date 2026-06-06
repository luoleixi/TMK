package health

import "sync"

type Checker func() bool

var (
	mu       sync.RWMutex
	checkers = map[string]Checker{
		"asr":        nil,
		"translator": nil,
		"tts":        nil,
	}
)

func Register(name string, fn Checker) {
	mu.Lock()
	defer mu.Unlock()
	checkers[name] = fn
}

func Services() map[string]bool {
	mu.RLock()
	defer mu.RUnlock()
	result := make(map[string]bool, len(checkers))
	for name, fn := range checkers {
		if fn == nil {
			result[name] = false
		} else {
			result[name] = fn()
		}
	}
	return result
}

func Status() string {
	allOK := true
	anyOK := false
	for _, ok := range Services() {
		if ok {
			anyOK = true
		} else {
			allOK = false
		}
	}
	if !anyOK {
		return "starting"
	}
	if allOK {
		return "ok"
	}
	return "degraded"
}
