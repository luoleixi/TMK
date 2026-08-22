package shortcut

const (
	Start = "start"
	Pause = "pause"
	Stop  = "stop"
)

func Emit(emit func(string), action string) {
	if emit != nil {
		emit(action)
	}
}
