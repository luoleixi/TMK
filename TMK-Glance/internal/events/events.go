package events

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID          string         `json:"event_id"`
	Type        string         `json:"event_type"`
	AggregateID string         `json:"aggregate_id"`
	OccurredAt  time.Time      `json:"occurred_at"`
	RequestID   string         `json:"request_id,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

func New(eventType, aggregateID, requestID string, payload map[string]any) Event {
	return Event{ID: uuid.NewString(), Type: eventType, AggregateID: aggregateID, RequestID: requestID, OccurredAt: time.Now().UTC(), Payload: payload}
}

type Handler func(Event)
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func NewBus() *Bus { return &Bus{handlers: make(map[string][]Handler)} }
func (b *Bus) Subscribe(eventType string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}
func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers[event.Type]...)
	handlers = append(handlers, b.handlers["*"]...)
	b.mu.RUnlock()
	for _, handler := range handlers {
		handler(event)
	}
}
