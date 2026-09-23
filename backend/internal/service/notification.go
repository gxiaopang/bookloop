package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	domainevent "github.com/gxiaopang/bookloop/backend/internal/event"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

const notificationConsumerName = "notification.v1"

type notificationRepository interface {
	ProcessEvent(context.Context, string, uint64, []model.Notification) (bool, error)
	ListByUser(context.Context, uint64, bool, int, int) ([]model.Notification, int64, int64, error)
	MarkRead(context.Context, uint64, uint64) error
	MarkAllRead(context.Context, uint64) error
}

type Notification struct {
	notifications notificationRepository
}

func NewNotification(notifications notificationRepository) *Notification {
	return &Notification{notifications: notifications}
}

func (s *Notification) Process(ctx context.Context, envelope domainevent.Envelope) (bool, error) {
	if envelope.ID == 0 || envelope.AggregateType != "order" || envelope.AggregateID == "" {
		return false, ErrValidation
	}
	var payload domainevent.OrderPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode order event payload: %w", err)
	}
	if payload.OrderNo == "" || payload.OrderNo != envelope.AggregateID ||
		payload.BuyerID == 0 || payload.SellerID == 0 || payload.BuyerID == payload.SellerID {
		return false, ErrValidation
	}
	title, buyerContent, sellerContent, ok := notificationCopy(envelope.Type, payload)
	if !ok {
		return false, ErrValidation
	}
	orderNo := payload.OrderNo
	notifications := []model.Notification{
		{UserID: payload.BuyerID, EventID: envelope.ID, Type: envelope.Type, Title: title, Content: buyerContent, OrderNo: &orderNo},
		{UserID: payload.SellerID, EventID: envelope.ID, Type: envelope.Type, Title: title, Content: sellerContent, OrderNo: &orderNo},
	}
	return s.notifications.ProcessEvent(ctx, notificationConsumerName, envelope.ID, notifications)
}

func (s *Notification) List(
	ctx context.Context,
	userID uint64,
	unreadOnly bool,
	page, pageSize int,
) (*dto.NotificationListResponse, error) {
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	items, total, unread, err := s.notifications.ListByUser(ctx, userID, unreadOnly, page, pageSize)
	if err != nil {
		return nil, err
	}
	responses := make([]dto.NotificationResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, dto.NotificationResponse{
			ID: item.ID, Type: item.Type, Title: item.Title, Content: item.Content,
			OrderNo: item.OrderNo, ReadAt: item.ReadAt, CreatedAt: item.CreatedAt,
		})
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &dto.NotificationListResponse{
		Items: responses, UnreadCount: unread,
		Pagination: dto.PaginationResponse{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages},
	}, nil
}

func (s *Notification) MarkRead(ctx context.Context, userID, notificationID uint64) error {
	if userID == 0 || notificationID == 0 {
		return ErrValidation
	}
	err := s.notifications.MarkRead(ctx, userID, notificationID)
	if errors.Is(err, repository.ErrNotificationNotFound) {
		return ErrNotificationNotFound
	}
	return err
}

func (s *Notification) MarkAllRead(ctx context.Context, userID uint64) error {
	if userID == 0 {
		return ErrValidation
	}
	return s.notifications.MarkAllRead(ctx, userID)
}

func notificationCopy(eventType string, payload domainevent.OrderPayload) (string, string, string, bool) {
	book := payload.BookTitle
	switch eventType {
	case domainevent.OrderCreated:
		if payload.Status != string(model.OrderStatusPendingPayment) {
			return "", "", "", false
		}
		return "订单已创建", "你已成功下单《" + book + "》，请在支付截止时间前完成支付。",
			"你的《" + book + "》收到一笔新订单，正在等待买家支付。", true
	case domainevent.OrderPaid:
		if payload.Status != string(model.OrderStatusPaid) {
			return "", "", "", false
		}
		return "订单已支付", "《" + book + "》已支付，请与卖家完成线下交付。",
			"买家已支付《" + book + "》，请准备线下交付。", true
	case domainevent.OrderCancelled:
		if payload.Status != string(model.OrderStatusCancelled) {
			return "", "", "", false
		}
		return "订单已取消", "你已取消《" + book + "》的订单。",
			"《" + book + "》的订单已被买家取消，商品已恢复可售。", true
	case domainevent.OrderExpired:
		if payload.Status != string(model.OrderStatusExpired) {
			return "", "", "", false
		}
		return "订单已超时", "《" + book + "》因未及时支付已自动关闭。",
			"《" + book + "》的待支付订单已超时，商品已恢复可售。", true
	case domainevent.OrderCompleted:
		if payload.Status != string(model.OrderStatusCompleted) {
			return "", "", "", false
		}
		return "交易已完成", "《" + book + "》的交易已完成。",
			"买家已确认完成《" + book + "》的交易。", true
	default:
		return "", "", "", false
	}
}

type messagingStatusRepository interface {
	Stats(context.Context) (repository.OutboxStats, error)
}

type MessagingStatus struct{ outbox messagingStatusRepository }

func NewMessagingStatus(outbox messagingStatusRepository) *MessagingStatus {
	return &MessagingStatus{outbox: outbox}
}

func (s *MessagingStatus) Get(ctx context.Context) (*dto.MessagingStatusResponse, error) {
	stats, err := s.outbox.Stats(ctx)
	if err != nil {
		return nil, err
	}
	age := int64(0)
	if stats.OldestPending != nil {
		age = max(0, int64(time.Since(*stats.OldestPending).Seconds()))
	}
	return &dto.MessagingStatusResponse{
		PendingOutbox: stats.Pending, RetryingOutbox: stats.Retrying,
		OldestPendingAt: stats.OldestPending, OldestPendingAgeSec: age,
	}, nil
}
