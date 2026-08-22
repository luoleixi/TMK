package events

import "testing"

func TestBusPublishesTypedAndWildcardEvents(t *testing.T) {
	bus := NewBus()
	typed, wildcard := 0, 0
	bus.Subscribe("EvaluationCreated", func(Event) { typed++ })
	bus.Subscribe("*", func(Event) { wildcard++ })
	bus.Publish(New("EvaluationCreated", "task-1", "req-1", nil))
	if typed != 1 || wildcard != 1 {
		t.Fatalf("typed=%d wildcard=%d", typed, wildcard)
	}
}
