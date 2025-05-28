package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

// Call represents a call record in the database
type Call struct {
	ID        int        `json:"id"`
	UUID      string     `json:"uuid"`
	Direction string     `json:"direction"`
	Caller    string     `json:"caller"`
	Callee    string     `json:"callee"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Status    *string    `json:"status,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Store handles database operations
type Store struct {
	db  *pgxpool.Pool
	log *logrus.Logger
}

// NewStore creates a new Store
func NewStore(db *pgxpool.Pool, logger *logrus.Logger) *Store {
	return &Store{db: db, log: logger}
}

// CreateCall inserts a new call record into the database
func (s *Store) CreateCall(ctx context.Context, call *Call) error {
	query := `
		INSERT INTO calls (uuid, direction, caller, callee, start_time)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`

	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	row := s.db.QueryRow(ctxTimeout, query, call.UUID, call.Direction, call.Caller, call.Callee, call.StartTime)
	err := row.Scan(&call.ID, &call.CreatedAt)
	if err != nil {
		s.log.WithError(err).Error("Error creating call record")
		return err
	}
	s.log.WithFields(logrus.Fields{
		"uuid": call.UUID,
		"id":   call.ID,
	}).Info("Call record created")
	return nil
}

// UpdateCallHangup updates a call record with hangup information
func (s *Store) UpdateCallHangup(ctx context.Context, uuid string, endTime time.Time, status string) error {
	query := `
		UPDATE calls
		SET end_time = $1, status = $2
		WHERE uuid = $3`

	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmdTag, err := s.db.Exec(ctxTimeout, query, endTime, status, uuid)
	if err != nil {
		s.log.WithError(err).WithField("uuid", uuid).Error("Error updating call record for hangup")
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		s.log.WithField("uuid", uuid).Warn("No call record found to update for hangup")
		// Depending on requirements, this might be an error or just a warning.
		// For now, logging as a warning.
	}
	s.log.WithFields(logrus.Fields{
		"uuid":   uuid,
		"status": status,
	}).Info("Call record updated with hangup info")
	return nil
}

// GetCalls retrieves a list of calls with pagination
func (s *Store) GetCalls(ctx context.Context, limit, offset int) ([]Call, error) {
	query := `
		SELECT id, uuid, direction, caller, callee, start_time, end_time, status, created_at
		FROM calls
		ORDER BY start_time DESC
		LIMIT $1 OFFSET $2`

	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctxTimeout, query, limit, offset)
	if err != nil {
		s.log.WithError(err).Error("Error getting calls")
		return nil, err
	}
	defer rows.Close()

	var calls []Call
	for rows.Next() {
		var call Call
		if err := rows.Scan(
			&call.ID, &call.UUID, &call.Direction, &call.Caller, &call.Callee,
			&call.StartTime, &call.EndTime, &call.Status, &call.CreatedAt,
		); err != nil {
			s.log.WithError(err).Error("Error scanning call row")
			return nil, err
		}
		calls = append(calls, call)
	}

	if err = rows.Err(); err != nil {
		s.log.WithError(err).Error("Error iterating call rows")
		return nil, err
	}

	s.log.WithFields(logrus.Fields{
		"limit":  limit,
		"offset": offset,
		"count":  len(calls),
	}).Info("Retrieved calls")
	return calls, nil
}

// GetCallByUUID retrieves a single call by its UUID
func (s *Store) GetCallByUUID(ctx context.Context, uuid string) (*Call, error) {
	query := `
		SELECT id, uuid, direction, caller, callee, start_time, end_time, status, created_at
		FROM calls
		WHERE uuid = $1`

	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var call Call
	err := s.db.QueryRow(ctxTimeout, query, uuid).Scan(
		&call.ID, &call.UUID, &call.Direction, &call.Caller, &call.Callee,
		&call.StartTime, &call.EndTime, &call.Status, &call.CreatedAt,
	)
	if err != nil {
		s.log.WithError(err).WithField("uuid", uuid).Error("Error getting call by UUID")
		return nil, err // Consider pgx.ErrNoRows specifically if needed
	}
	s.log.WithField("uuid", uuid).Info("Retrieved call by UUID")
	return &call, nil
}

// InitSchema creates the calls table if it doesn't exist.
// This is a basic implementation; for production, use migrations.
func (s *Store) InitSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS calls (
		id         SERIAL PRIMARY KEY,
		uuid       TEXT UNIQUE NOT NULL,
		direction  TEXT NOT NULL,
		caller     TEXT NOT NULL,
		callee     TEXT NOT NULL,
		start_time TIMESTAMP NOT NULL,
		end_time   TIMESTAMP,
		status     TEXT,
		created_at TIMESTAMP DEFAULT now()
	);`

	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := s.db.Exec(ctxTimeout, query)
	if err != nil {
		s.log.WithError(err).Error("Error initializing database schema")
		return err
	}
	s.log.Info("Database schema initialized (calls table ensured)")
	return nil
}
