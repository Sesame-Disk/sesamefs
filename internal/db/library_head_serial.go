package db

import gocql "github.com/apache/cassandra-gocql-driver/v2"

// LibraryHeadSerialConsistency is the Paxos domain for every conditional
// mutation that competes for canonical libraries.head_commit_id authority:
// v2/Sync HEAD advances, initial-HEAD publication, and the creation-rollback
// DELETE guard. It is global SERIAL, is not derived from
// database.serial_consistency / CASSANDRA_SERIAL_CONSISTENCY, and must never
// be LOCAL_SERIAL. Other LWTs may still inherit the session default.
const LibraryHeadSerialConsistency = gocql.Serial
