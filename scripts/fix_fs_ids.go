//go:build ignore

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/gocql/gocql"
)

func main() {
	cassandraHost := flag.String("cassandra", "localhost:9042", "Cassandra host:port")
	dryRun := flag.Bool("dry-run", true, "Only print what would be changed")
	flag.Parse()

	// Connect to Cassandra
	cluster := gocql.NewCluster(*cassandraHost)
	cluster.Keyspace = "sesamefs"
	cluster.Consistency = gocql.Quorum
	session, err := cluster.CreateSession()
	if err != nil {
		log.Fatalf("Failed to connect to Cassandra: %v", err)
	}
	defer session.Close()

	// Query all directory fs_objects
	iter := session.Query(`
		SELECT library_id, fs_id, dir_entries FROM fs_objects WHERE obj_type = 'dir' ALLOW FILTERING
	`).Iter()

	var libraryID gocql.UUID
	var fsID string
	var dirEntries string

	fixes := 0
	correct := 0

	for iter.Scan(&libraryID, &fsID, &dirEntries) {
		// Build the JSON exactly like pack-fs does
		var rawDirents json.RawMessage
		if dirEntries != "" && dirEntries != "[]" {
			rawDirents = json.RawMessage(dirEntries)
		} else {
			rawDirents = json.RawMessage("[]")
		}

		jsonObj := map[string]interface{}{
			"version": 1,
			"type":    3,
			"dirents": rawDirents,
		}

		jsonBytes, err := json.Marshal(jsonObj)
		if err != nil {
			log.Printf("Error marshaling %s: %v", fsID, err)
			continue
		}

		// Compute correct fs_id
		hash := sha1.Sum(jsonBytes)
		computedFSID := hex.EncodeToString(hash[:])

		if fsID != computedFSID {
			fixes++
			fmt.Printf("MISMATCH library=%s\n  stored=%s\n  computed=%s\n  json=%s\n\n",
				libraryID, fsID, computedFSID, string(jsonBytes))

			if !*dryRun {
				// Update fs_objects with new fs_id
				// This is complex because we also need to update all references
				// For now, just log the mismatches
				log.Printf("Would need to fix %s -> %s", fsID, computedFSID)
			}
		} else {
			correct++
		}
	}

	if err := iter.Close(); err != nil {
		log.Fatalf("Error iterating: %v", err)
	}

	fmt.Printf("\nSummary: %d correct, %d need fixing\n", correct, fixes)
}
