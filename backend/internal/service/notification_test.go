package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	domainevent "github.com/gxiaopang/bookloop/backend/internal/event"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

func TestNotificationProcessesOrderEventIdempotently(t *testing.T) {
	repository := newFakeNotificationRepository()
	service := NewNotification(repository)
	payload, err := json.Marshal(domainevent.OrderPayload{
		OrderNo: "01HF7YAT00ABCDEFGHJKMNPQRS", BuyerID: 20, SellerID: 10,
		BookID: 7, BookTitle: "Database Systems", Status: "completed",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	envelope := domainevent.Envelope{
		ID: 9, AggregateType: "order", AggregateID: "01HF7YAT00ABCDEFGHJKMNPQRS",
		Type: domainevent.OrderCompleted, Payload: payload, OccurredAt: time.Now().UTC(),
	}
	created, err := service.Process(context.Background(), envelope)
	if err != nil || !created {
		t.Fatalf("first Process() = %v/%v, want created", created, err)
	}
	created, err = service.Process(context.Background(), envelope)
	if err != nil || created {
		t.Fatalf("duplicate Process() = %v/%v, want idempotent replay", created, err)
	}
	if len(repository.items) != 2 || repository.items[0].UserID != 20 || repository.items[1].UserID != 10 {
		t.Fatalf("notifications = %+v, want buyer and seller notifications", repository.items)
	}
}

func TestNotificationListAndReadAreUserScoped(t *testing.T) {
	repository := newFakeNotificationRepository()
	repository.items = []model.Notification{
		{ID: 1, UserID: 20, Title: "one", CreatedAt: time.Now().UTC()},
		{ID: 2, UserID: 20, Title: "two", CreatedAt: time.Now().UTC()},
		{ID: 3, UserID: 30, Title: "other", CreatedAt: time.Now().UTC()},
	}
	service := NewNotification(repository)
	result, err := service.List(context.Background(), 20, false, 1, 20)
	if err != nil || len(result.Items) != 2 || result.UnreadCount != 2 {
		t.Fatalf("List() = %+v/%v, want two unread user notifications", result, err)
	}
	if err := service.MarkRead(context.Background(), 20, 1); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	if repository.items[0].ReadAt == nil {
		t.Fatal("MarkRead() did not set read_at")
	}
	if err := service.MarkRead(context.Background(), 20, 3); !errors.Is(err, ErrNotificationNotFound) {
		t.Fatalf("MarkRead(other user) error = %v, want ErrNotificationNotFound", err)
	}
	if err := service.MarkAllRead(context.Background(), 20); err != nil || repository.items[1].ReadAt == nil {
		t.Fatalf("MarkAllRead() error/items = %v/%+v", err, repository.items)
	}
}

type fakeNotificationRepository struct {
	processed map[uint64]bool
	items     []model.Notification
}

func newFakeNotificationRepository() *fakeNotificationRepository {
	return &fakeNotificationRepository{processed: make(map[uint64]bool)}
}

func (r *fakeNotificationRepository) ProcessEvent(
	_ context.Context, _ string, eventID uint64, notifications []model.Notification,
) (bool, error) {
	if r.processed[eventID] {
		return false, nil
	}
	r.processed[eventID] = true
	for index := range notifications {
		notifications[index].ID = uint64(len(r.items) + 1)
		notifications[index].CreatedAt = time.Now().UTC()
		r.items = append(r.items, notifications[index])
	}
	return true, nil
}

func (r *fakeNotificationRepository) ListByUser(
	_ context.Context, userID uint64, unreadOnly bool, page, pageSize int,
) ([]model.Notification, int64, int64, error) {
	var filtered []model.Notification
	var unread int64
	for _, item := range r.items {
		if item.UserID != userID {
			continue
		}
		if item.ReadAt == nil {
			unread++
		}
		if !unreadOnly || item.ReadAt == nil {
			filtered = append(filtered, item)
		}
	}
	start := min((page-1)*pageSize, len(filtered))
	end := min(start+pageSize, len(filtered))
	return filtered[start:end], int64(len(filtered)), unread, nil
}

func (r *fakeNotificationRepository) MarkRead(_ context.Context, userID, notificationID uint64) error {
	for index := range r.items {
		if r.items[index].ID == notificationID && r.items[index].UserID == userID {
			now := time.Now().UTC()
			r.items[index].ReadAt = &now
			return nil
		}
	}
	return repository.ErrNotificationNotFound
}

func (r *fakeNotificationRepository) MarkAllRead(_ context.Context, userID uint64) error {
	for index := range r.items {
		if r.items[index].UserID == userID && r.items[index].ReadAt == nil {
			now := time.Now().UTC()
			r.items[index].ReadAt = &now
		}
	}
	return nil
}
