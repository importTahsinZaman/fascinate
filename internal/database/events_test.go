package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCreateAndListEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	machine, err := store.CreateMachine(ctx, CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateEvent(ctx, CreateEventParams{
		ID:          "event-1",
		ActorUserID: &user.ID,
		MachineID:   &machine.ID,
		Kind:        "machine.create.succeeded",
		PayloadJSON: `{"machine_name":"habits"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateEvent(ctx, CreateEventParams{
		ID:          "event-2",
		ActorUserID: &user.ID,
		Kind:        "toolauth.capture.failed",
		PayloadJSON: `{"tool_id":"claude"}`,
	}); err != nil {
		t.Fatal(err)
	}

	machineEvents, err := store.ListMachineEvents(ctx, machine.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(machineEvents) != 1 || machineEvents[0].ID != "event-1" {
		t.Fatalf("unexpected machine events: %+v", machineEvents)
	}

	actorEvents, err := store.ListActorEvents(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(actorEvents) != 2 {
		t.Fatalf("expected 2 actor events, got %d", len(actorEvents))
	}
	if actorEvents[0].ID != "event-2" {
		t.Fatalf("expected most recent actor event first, got %+v", actorEvents)
	}
}
