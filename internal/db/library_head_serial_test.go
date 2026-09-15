package db

import (
	"testing"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func TestLibraryHeadSerialConsistencyIsGlobalSerial(t *testing.T) {
	if LibraryHeadSerialConsistency != gocql.Serial {
		t.Fatalf("LibraryHeadSerialConsistency must be gocql.Serial, got %v", LibraryHeadSerialConsistency)
	}
	if LibraryHeadSerialConsistency == gocql.LocalSerial {
		t.Fatal("LibraryHeadSerialConsistency must not be gocql.LocalSerial; canonical HEAD authority is a global SERIAL Paxos domain")
	}
}
