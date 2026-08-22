package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type OutboxMessage struct {
	ID, Topic, AggregateID, Payload string
	Attempts                        int
	AvailableAt                     time.Time
}
type InboxConsumer struct {
	Store *SessionStore
	Name  string
}

func (c InboxConsumer) Process(message OutboxMessage, handler func(OutboxMessage) error) error {
	claimed, err := c.Store.MarkInbox(c.Name, message.ID)
	if err != nil || !claimed {
		return err
	}
	if err := handler(message); err != nil {
		_, _ = c.Store.db.Exec(`DELETE FROM event_inbox WHERE consumer=? AND event_id=?`, c.Name, message.ID)
		return err
	}
	return c.Store.MarkOutboxPublished(message.ID)
}

func migrateOutboxMySQL(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS event_outbox (id VARCHAR(36) PRIMARY KEY,topic VARCHAR(100) NOT NULL,aggregate_id VARCHAR(64) NOT NULL,payload JSON NOT NULL,attempts INT NOT NULL DEFAULT 0,available_at DATETIME(6) NOT NULL,published_at DATETIME(6) NULL,created_at DATETIME(6) NOT NULL,INDEX idx_outbox_available(available_at,published_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4; CREATE TABLE IF NOT EXISTS event_inbox (consumer VARCHAR(100) NOT NULL,event_id VARCHAR(36) NOT NULL,processed_at DATETIME(6) NOT NULL,PRIMARY KEY(consumer,event_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}
func (s *SessionStore) EnqueueOutbox(topic, aggregateID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO event_outbox(id,topic,aggregate_id,payload,available_at,created_at) VALUES(?,?,?,?,NOW(6),NOW(6))`, uuid.NewString(), topic, aggregateID, data)
	return err
}
func (s *SessionStore) ClaimOutbox(limit int) ([]OutboxMessage, error) {
	rows, err := s.db.Query(`SELECT id,topic,aggregate_id,payload,attempts,available_at FROM event_outbox WHERE published_at IS NULL AND available_at<=NOW(6) ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []OutboxMessage
	for rows.Next() {
		var m OutboxMessage
		if err := rows.Scan(&m.ID, &m.Topic, &m.AggregateID, &m.Payload, &m.Attempts, &m.AvailableAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
func (s *SessionStore) MarkOutboxPublished(id string) error {
	_, err := s.db.Exec(`UPDATE event_outbox SET published_at=NOW(6) WHERE id=?`, id)
	return err
}
func (s *SessionStore) MarkInbox(consumer, eventID string) (bool, error) {
	result, err := s.db.Exec(`INSERT IGNORE INTO event_inbox(consumer,event_id,processed_at) VALUES(?,?,NOW(6))`, consumer, eventID)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n == 1, nil
}
