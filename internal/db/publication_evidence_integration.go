//go:build integration

package db

import (
	"github.com/Sesame-Disk/sesamefs/internal/config"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// NewPublicationEvidenceDB wraps an isolated native-protocol test session.
// It exists only in integration builds; productive methods use the supplied
// real Cassandra session without replacing any query or outcome classifier.
func NewPublicationEvidenceDB(session *gocql.Session, cfg config.DatabaseConfig) *DB {
	return &DB{session: session, config: cfg}
}
