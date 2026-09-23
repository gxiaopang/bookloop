package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

func TestNotificationHandlerListsAndMarksOnlyCurrentUsersNotifications(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &handlerNotificationRepository{items: []model.Notification{
		{ID: 1, UserID: 20, Title: "mine", Content: "mine", CreatedAt: time.Now().UTC()},
		{ID: 2, UserID: 30, Title: "other", Content: "other", CreatedAt: time.Now().UTC()},
	}}
	logger := config.NewLogger(io.Discard, "debug")
	handler := NewNotification(service.NewNotification(repository), logger)

	response := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(response)
	requestContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/notifications?unread_only=true", nil)
	requestContext.Set("current_user", &model.User{ID: 20})
	handler.List(requestContext)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"mine"`) ||
		strings.Contains(response.Body.String(), `"title":"other"`) {
		t.Fatalf("List() status/body = %d/%s, want only current user's notification", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	requestContext, _ = gin.CreateTestContext(response)
	requestContext.Request = httptest.NewRequest(http.MethodPut, "/api/v1/notifications/2/read", nil)
	requestContext.Params = gin.Params{{Key: "notification_id", Value: "2"}}
	requestContext.Set("current_user", &model.User{ID: 20})
	handler.MarkRead(requestContext)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"NOTIFICATION_NOT_FOUND"`) {
		t.Fatalf("MarkRead(other user) status/body = %d/%s, want 404", response.Code, response.Body.String())
	}
}

type handlerNotificationRepository struct{ items []model.Notification }

func (r *handlerNotificationRepository) ProcessEvent(context.Context, string, uint64, []model.Notification) (bool, error) {
	return true, nil
}

func (r *handlerNotificationRepository) ListByUser(
	_ context.Context, userID uint64, unreadOnly bool, _, _ int,
) ([]model.Notification, int64, int64, error) {
	var items []model.Notification
	var unread int64
	for _, item := range r.items {
		if item.UserID != userID {
			continue
		}
		if item.ReadAt == nil {
			unread++
		}
		if !unreadOnly || item.ReadAt == nil {
			items = append(items, item)
		}
	}
	return items, int64(len(items)), unread, nil
}

func (r *handlerNotificationRepository) MarkRead(_ context.Context, userID, notificationID uint64) error {
	for index := range r.items {
		if r.items[index].ID == notificationID && r.items[index].UserID == userID {
			now := time.Now().UTC()
			r.items[index].ReadAt = &now
			return nil
		}
	}
	return repository.ErrNotificationNotFound
}

func (r *handlerNotificationRepository) MarkAllRead(context.Context, uint64) error { return nil }
