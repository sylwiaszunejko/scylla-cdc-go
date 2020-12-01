package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gocql/gocql"
	scylla_cdc "github.com/piodul/scylla-cdc-go"
)

const (
	sourceAddress      = "127.0.0.1"
	destinationAddress = "127.0.0.2"
)

type schema struct {
	tableName   string
	createQuery string
}

var (
	schemaSimple = schema{
		"ks.tbl_simple",
		"CREATE TABLE ks.tbl_simple (pk text, ck int, v1 int, v2 text, PRIMARY KEY (pk, ck))",
	}
	schemaMultipleClusteringKeys = schema{
		"ks.tbl_multiple_clustering_keys",
		"CREATE TABLE ks.tbl_multiple_clustering_keys (pk text, ck1 int, ck2 int, v int, PRIMARY KEY (pk, ck1, ck2))",
	}
	schemaLists = schema{
		"ks.tbl_lists",
		"CREATE TABLE ks.tbl_lists (pk text, ck int, v list<int>, PRIMARY KEY(pk, ck))",
	}
	schemaSets = schema{
		"ks.tbl_sets",
		"CREATE TABLE ks.tbl_sets (pk text, ck int, v set<int>, PRIMARY KEY (pk, ck))",
	}
	schemaMaps = schema{
		"ks.tbl_maps",
		"CREATE TABLE ks.tbl_maps (pk text, ck int, v map<int, int>, PRIMARY KEY (pk, ck))",
	}
)

var testCases = []struct {
	schema  schema
	pk      string
	queries []string
}{
	// Operations test cases
	{
		schemaSimple,
		"simpleInserts",
		[]string{
			"INSERT INTO %s (pk, ck, v1, v2) VALUES ('simpleInserts', 1, 2, 'abc')",
			"INSERT INTO %s (pk, ck, v1) VALUES ('simpleInserts', 2, 3)",
			"INSERT INTO %s (pk, ck, v2) VALUES ('simpleInserts', 2, 'def')",
		},
	},
	{
		schemaSimple,
		"simpleUpdates",
		[]string{
			"UPDATE %s SET v1 = 1 WHERE pk = 'simpleUpdates' AND ck = 1",
			"UPDATE %s SET v2 = 'abc' WHERE pk = 'simpleUpdates' AND ck = 2",
			"UPDATE %s SET v1 = 5, v2 = 'def' WHERE pk = 'simpleUpdates' AND ck = 3",
		},
	},
	{
		schemaSimple,
		"rowDeletes",
		[]string{
			"INSERT INTO %s (pk, ck, v1, v2) VALUES ('rowDeletes', 1, 2, 'abc')",
			"INSERT INTO %s (pk, ck, v1, v2) VALUES ('rowDeletes', 2, 3, 'def')",
			"DELETE FROM %s WHERE pk = 'rowDeletes' AND ck = 1",
		},
	},
	{
		schemaSimple,
		"partitionDeletes",
		[]string{
			"INSERT INTO %s (pk, ck, v1, v2) VALUES ('partitionDeletes', 1, 2, 'abc')",
			"INSERT INTO %s (pk, ck, v1, v2) VALUES ('partitionDeletes', 2, 3, 'def')",
			"DELETE FROM %s WHERE pk = 'partitionDeletes'",
			// Insert one more row, just to check if replication works at all
			"INSERT INTO %s (pk, ck, v1, v2) VALUES ('partitionDeletes', 4, 5, 'def')",
		},
	},
	{
		schemaMultipleClusteringKeys,
		"rangeDeletes",
		[]string{
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 1, 1, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 1, 2, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 1, 3, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 1, 4, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 2, 1, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 2, 2, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 2, 3, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 2, 4, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 3, 1, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 3, 2, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 3, 3, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 3, 4, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 4, 1, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 4, 2, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 4, 3, 0)",
			"INSERT INTO %s (pk, ck1, ck2, v) VALUES ('rangeDeletes', 4, 4, 0)",
			"DELETE FROM %s WHERE pk = 'rangeDeletes' AND ck1 > 3",
			"DELETE FROM %s WHERE pk = 'rangeDeletes' AND ck1 <= 1",
			"DELETE FROM %s WHERE pk = 'rangeDeletes' AND ck1 = 2 AND ck2 > 1 AND ck2 < 4",
		},
	},

	// Lists test cases
	{
		schemaLists,
		"listOverwrites",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('listOverwrites', 1, [1, 2, 3])",
			"INSERT INTO %s (pk, ck, v) VALUES ('listOverwrites', 1, [4, 5, 6, 7])",
			"INSERT INTO %s (pk, ck, v) VALUES ('listOverwrites', 2, [6, 5, 4, 3, 2, 1])",
			"INSERT INTO %s (pk, ck, v) VALUES ('listOverwrites', 2, null)",
			"INSERT INTO %s (pk, ck, v) VALUES ('listOverwrites', 3, [1, 11, 111])",
			"UPDATE %s SET v = [2, 22, 222] WHERE pk = 'listOverwrites' AND ck = 3",
		},
	},
	{
		schemaLists,
		"listAppends",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('listAppends', 1, [1, 2, 3])",
			"UPDATE %s SET v = v + [4, 5, 6] WHERE pk = 'listAppends' AND ck = 1",
			"UPDATE %s SET v = [-2, -1, 0] + v WHERE pk = 'listAppends' AND ck = 1",
		},
	},
	{
		schemaLists,
		"listRemoves",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('listRemoves', 1, [1, 2, 3])",
			"UPDATE %s SET v = v + [4, 5, 6] WHERE pk = 'listRemoves' AND ck = 1",
			"UPDATE %s SET v = v - [1, 2, 3] WHERE pk = 'listRemoves' AND ck = 1",
		},
	},

	// Set test cases
	{
		schemaSets,
		"setOverwrites",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('setOverwrites', 1, {1, 2, 3, 4})",
			"INSERT INTO %s (pk, ck, v) VALUES ('setOverwrites', 1, {4, 5, 6, 7})",
			"INSERT INTO %s (pk, ck, v) VALUES ('setOverwrites', 2, {8, 9, 10, 11})",
			"INSERT INTO %s (pk, ck, v) VALUES ('setOverwrites', 2, null)",
			"INSERT INTO %s (pk, ck, v) VALUES ('setOverwrites', 3, {12, 13, 14, 15})",
			"UPDATE %s SET v = null WHERE pk = 'setOverwrites' AND ck = 3",
		},
	},
	{
		schemaSets,
		"setAppends",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('setAppends', 1, {1, 2, 3, 4})",
			"UPDATE %s SET v = v + {5, 6} WHERE pk = 'setAppends' AND ck = 1",
			"UPDATE %s SET v = v + {5, 6} WHERE pk = 'setAppends' AND ck = 2",
		},
	},
	{
		schemaSets,
		"setRemovals",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('setRemovals', 1, {1, 2, 3, 4})",
			"UPDATE %s SET v = v - {1, 3} WHERE pk = 'setRemovals' AND ck = 1",
			"UPDATE %s SET v = v - {1138} WHERE pk = 'setRemovals' AND ck = 2",
		},
	},

	// Map test cases
	{
		schemaMaps,
		"mapOverwrites",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('mapOverwrites', 1, {1: 2, 3: 4})",
			"INSERT INTO %s (pk, ck, v) VALUES ('mapOverwrites', 1, {5: 6, 7: 8})",
			"INSERT INTO %s (pk, ck, v) VALUES ('mapOverwrites', 2, {9: 10, 11: 12})",
			"INSERT INTO %s (pk, ck, v) VALUES ('mapOverwrites', 2, null)",
			"INSERT INTO %s (pk, ck, v) VALUES ('mapOverwrites', 3, {13: 14, 15: 16})",
			"UPDATE %s SET v = null WHERE pk = 'mapOverwrites' AND ck = 3",
		},
	},
	{
		schemaMaps,
		"mapSets",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('mapSets', 1, {1: 2, 3: 4, 5: 6})",
			"UPDATE %s SET v[1] = 42 WHERE pk = 'mapSets' AND ck = 1",
			"UPDATE %s SET v[3] = null WHERE pk = 'mapSets' AND ck = 1",
			"UPDATE %s SET v[3] = 123 WHERE pk = 'mapSets' AND ck = 1",
			"UPDATE %s SET v[5] = 321 WHERE pk = 'mapSets' AND ck = 2",
		},
	},
	{
		schemaMaps,
		"mapAppends",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('mapAppends', 1, {1: 2, 3: 4})",
			"UPDATE %s SET v = v + {5: 6} WHERE pk = 'mapAppends' AND ck = 1",
			"UPDATE %s SET v = v + {5: 6} WHERE pk = 'mapAppends' AND ck = 2",
		},
	},
	{
		schemaMaps,
		"mapRemovals",
		[]string{
			"INSERT INTO %s (pk, ck, v) VALUES ('mapRemovals', 1, {1: 2, 3: 4})",
			"UPDATE %s SET v = v - {1} WHERE pk = 'mapRemovals' AND ck = 1",
			"UPDATE %s SET v = v - {1138} WHERE pk = 'mapRemovals' AND ck = 2",
		},
	},

	// TODO: UDTs
	// TODO: Tuples
}

func TestReplicator(t *testing.T) {
	// Collect all schemas
	schemas := make(map[string]string)
	for _, tc := range testCases {
		schemas[tc.schema.tableName] = tc.schema.createQuery
	}

	// TODO: Provide IPs from the env
	sourceSession := createSessionAndSetupSchema(t, sourceAddress, true, schemas)
	defer sourceSession.Close()

	destinationSession := createSessionAndSetupSchema(t, destinationAddress, false, schemas)
	defer destinationSession.Close()

	// Execute all of the queries
	for _, tc := range testCases {
		for _, qStr := range tc.queries {
			execQuery(t, sourceSession, fmt.Sprintf(qStr, tc.schema.tableName))
		}
	}

	<-time.After(3 * time.Second)

	t.Log("running replicators")

	adv := scylla_cdc.AdvancedReaderConfig{
		ChangeAgeLimit:         time.Minute,
		PostNonEmptyQueryDelay: 10 * time.Second,
		PostEmptyQueryDelay:    10 * time.Second,
		PostFailedQueryDelay:   10 * time.Second,
		QueryTimeWindowSize:    5 * time.Minute,
		ConfidenceWindowSize:   0,
	}

	// TODO: Make it possible for the replicator to replicate multiple tables simultaneously
	schemaNames := make([]string, 0)
	for tbl := range schemas {
		schemaNames = append(schemaNames, tbl)
	}

	finishF, err := RunReplicator(context.Background(), sourceAddress, destinationAddress, schemaNames, &adv)
	if err != nil {
		t.Fatal(err)
	}

	// Wait 10 seconds
	<-time.After(10 * time.Second)

	t.Log("validating results")

	if err := finishF(); err != nil {
		t.Fatal(err)
	}

	// Compare
	sourceSet := fetchFullSet(t, sourceSession, schemas)
	destinationSet := fetchFullSet(t, destinationSession, schemas)

	failedCount := 0

	for _, tc := range testCases {
		sourceData := sourceSet[tc.pk]
		destinationData := destinationSet[tc.pk]

		if len(sourceData) != len(destinationData) {
			t.Logf(
				"%s: source len %d, destination len %d\n",
				tc.pk,
				len(sourceData),
				len(destinationData),
			)
			t.Log("  source:")
			for _, row := range sourceData {
				t.Logf("    %v", row)
			}
			t.Log("  dest:")
			for _, row := range destinationData {
				t.Logf("    %v", row)
			}
			t.Fail()
			failedCount++
			continue
		}

		failed := false
		for i := 0; i < len(sourceData); i++ {
			if !reflect.DeepEqual(sourceData[i], destinationData[i]) {
				t.Logf("%s: mismatch", tc.pk)
				t.Logf("  source: %v", sourceData[i])
				t.Logf("  dest:   %v", destinationData[i])
				failed = true
			}
		}

		if failed {
			t.Fail()
			failedCount++
		} else {
			t.Logf("%s: OK", tc.pk)
		}
	}

	if failedCount > 0 {
		t.Logf("failed %d/%d test cases", failedCount, len(testCases))
	}
}

func createSessionAndSetupSchema(t *testing.T, addr string, withCdc bool, schemas map[string]string) *gocql.Session {
	cfg := gocql.NewCluster(addr)
	session, err := cfg.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	execQuery(t, session, "DROP KEYSPACE IF EXISTS ks")
	execQuery(t, session, "CREATE KEYSPACE ks WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}")

	for _, tbl := range schemas {
		tblQuery := tbl
		if withCdc {
			tblQuery += " WITH cdc = {'enabled': true}"
		}
		execQuery(t, session, tblQuery)
	}

	err = session.AwaitSchemaAgreement(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	return session
}

func execQuery(t *testing.T, session *gocql.Session, query string) {
	t.Logf("executing query %s", query)
	err := session.Query(query).Exec()
	if err != nil {
		t.Fatal(err)
	}
}

func fetchFullSet(t *testing.T, session *gocql.Session, schemas map[string]string) map[string][]map[string]interface{} {
	groups := make(map[string][]map[string]interface{})

	for tbl := range schemas {
		data, err := session.Query("SELECT * FROM " + tbl).Iter().SliceMap()
		if err != nil {
			t.Fatal(err)
		}

		for _, row := range data {
			pk := row["pk"].(string)
			groups[pk] = append(groups[pk], row)
		}
	}

	return groups
}