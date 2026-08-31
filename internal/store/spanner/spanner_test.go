package spanner

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	dbadmin "cloud.google.com/go/spanner/admin/database/apiv1"
	"cloud.google.com/go/spanner/admin/database/apiv1/databasepb"
	insadmin "cloud.google.com/go/spanner/admin/instance/apiv1"
	"cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ChristopherDavenport/project-box/internal/store/conformance"
)

// TestConformance runs the shared suite against the emulator.
//
// Locally, no emulator means a skip with instructions — development should not
// require it minute to minute. In CI its absence is a FAILURE: "emulator down,
// tests skipped, all green" is a conformance suite that proves nothing, and
// the whole point of two backends is that both are held to the properties on
// every commit.
func TestConformance(t *testing.T) {
	if os.Getenv("SPANNER_EMULATOR_HOST") == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("SPANNER_EMULATOR_HOST unset in CI — the Spanner conformance leg is required, not optional")
		}
		t.Skip("SPANNER_EMULATOR_HOST not set; start one with: gcloud emulators spanner start")
	}
	db := bootstrap(t)
	conformance.Run(t, func(t *testing.T) conformance.Harness {
		s, err := Open(context.Background(), db)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		// No Advance: Spanner's clock is the server's, which is exactly the
		// property under test — so expiry subtests wait in real time.
		return conformance.Harness{Store: s}
	})
}

// bootstrap creates a fresh database (unique per run, so reruns never see a
// previous run's chains) on an emulator instance, applying DDL exactly as the
// package exports it.
func bootstrap(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	const project, instance = "box-test", "conformance"

	ia, err := insadmin.NewInstanceAdminClient(ctx)
	if err != nil {
		t.Fatalf("instance admin: %v", err)
	}
	defer ia.Close()
	op, err := ia.CreateInstance(ctx, &instancepb.CreateInstanceRequest{
		Parent:     "projects/" + project,
		InstanceId: instance,
		Instance: &instancepb.Instance{
			Config:      fmt.Sprintf("projects/%s/instanceConfigs/emulator-config", project),
			DisplayName: "conformance",
			NodeCount:   1,
		},
	})
	if status.Code(err) == codes.AlreadyExists {
		// fine: reruns share the instance, never the database
	} else if err != nil {
		t.Fatalf("create instance: %v", err)
	} else if _, err := op.Wait(ctx); err != nil && status.Code(err) != codes.AlreadyExists {
		t.Fatalf("create instance wait: %v", err)
	}

	da, err := dbadmin.NewDatabaseAdminClient(ctx)
	if err != nil {
		t.Fatalf("database admin: %v", err)
	}
	defer da.Close()
	dbName := fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000_000)
	dop, err := da.CreateDatabase(ctx, &databasepb.CreateDatabaseRequest{
		Parent:          fmt.Sprintf("projects/%s/instances/%s", project, instance),
		CreateStatement: "CREATE DATABASE " + dbName,
		ExtraStatements: DDL,
	})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	if _, err := dop.Wait(ctx); err != nil {
		t.Fatalf("create database wait: %v", err)
	}
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", project, instance, dbName)
}
