package identity

import (
	"context"
	"testing"
)

func TestServiceCreateAndGet(t *testing.T) {
	store := NewMemoryStore()
	var recorded []AuditEvent
	svc := NewService(store, func(e AuditEvent) { recorded = append(recorded, e) })

	user, err := svc.Create(context.Background(), "Aris")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if user.ID != "1" || user.Name != "Aris" {
		t.Fatalf("unexpected user: %+v", user)
	}

	if len(recorded) != 1 || recorded[0].Action != "user.created" {
		t.Fatalf("expected user.created event, got %+v", recorded)
	}

	got, err := svc.GetByID(context.Background(), user.ID)
	if err != nil || got.Name != "Aris" {
		t.Fatalf("get by id: %+v err=%v", got, err)
	}

	if _, err := svc.Create(context.Background(), ""); err != ErrInvalidName {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestMemoryStoreList(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Create(context.Background(), "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "B"); err != nil {
		t.Fatal(err)
	}

	users := store.List(context.Background())
	if len(users) != 2 || users[0].Name != "A" || users[1].Name != "B" {
		t.Fatalf("unexpected list: %+v", users)
	}
}
