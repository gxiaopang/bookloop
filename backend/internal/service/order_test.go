package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	domainevent "github.com/gxiaopang/bookloop/backend/internal/event"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

func TestOrderCreateSnapshotsBookAndReservesIt(t *testing.T) {
	repository := newFakeOrderRepository()
	repository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repository.books[7] = &model.Book{
		ID: 7, SellerID: 10, Title: "Database Systems", Price: "35.00", Status: model.BookStatusAvailable,
	}
	service := newTestOrderService(repository)

	result, err := service.Create(context.Background(), 20, 7, "intent-1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Replayed || result.Order.BuyerID != 20 || result.Order.SellerID != 10 ||
		result.Order.BookTitle != "Database Systems" || result.Order.BookPrice != "35.00" ||
		result.Order.Status != string(model.OrderStatusPendingHandover) {
		t.Fatalf("Create() = %+v, want pending order with database snapshot", result)
	}
	if repository.books[7].Status != model.BookStatusReserved {
		t.Fatalf("book status = %q, want reserved", repository.books[7].Status)
	}
	if !result.Order.ExpiresAt.Equal(repository.now.Add(15 * time.Minute)) {
		t.Fatalf("expires_at = %s, want database time + timeout", result.Order.ExpiresAt)
	}
	if len(repository.outboxEvents) != 1 || repository.outboxEvents[0].EventType != domainevent.OrderCreated {
		t.Fatalf("outbox events = %+v, want one order.created event", repository.outboxEvents)
	}
	var payload domainevent.OrderPayload
	if err := json.Unmarshal(repository.outboxEvents[0].Payload, &payload); err != nil ||
		payload.OrderNo != result.Order.OrderNo || payload.BuyerID != 20 || payload.SellerID != 10 {
		t.Fatalf("outbox payload = %+v/%v, want committed order participants", payload, err)
	}
}

func TestOrderCreateEnforcesBuyerSellerAndAvailabilityRules(t *testing.T) {
	repository := newFakeOrderRepository()
	repository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repository.books[1] = &model.Book{ID: 1, SellerID: 10, Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	repository.books[2] = &model.Book{ID: 2, SellerID: 10, Status: model.BookStatusOffShelf}
	service := newTestOrderService(repository)

	if _, err := service.Create(context.Background(), 10, 1, "self-buy"); !errors.Is(err, ErrCannotPurchaseOwnBook) {
		t.Fatalf("Create(self) error = %v, want ErrCannotPurchaseOwnBook", err)
	}
	if _, err := service.Create(context.Background(), 20, 2, "off-shelf"); !errors.Is(err, ErrBookNotAvailable) {
		t.Fatalf("Create(off-shelf) error = %v, want ErrBookNotAvailable", err)
	}
	repository.users[10].Status = model.UserStatusDisabled
	if _, err := service.Create(context.Background(), 20, 1, "disabled-seller"); !errors.Is(err, ErrBookNotAvailable) {
		t.Fatalf("Create(disabled seller) error = %v, want ErrBookNotAvailable", err)
	}
	if len(repository.orders) != 0 || repository.books[1].Status != model.BookStatusAvailable {
		t.Fatalf("failed creates changed state: orders=%d status=%q", len(repository.orders), repository.books[1].Status)
	}
}

func TestOrderCreateDatabaseIdempotency(t *testing.T) {
	repository := newFakeOrderRepository()
	repository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	repository.books[2] = &model.Book{ID: 2, SellerID: 10, Title: "Two", Price: "20.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	service := newTestOrderService(repository)

	first, err := service.Create(context.Background(), 20, 1, "same-key")
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	replayed, err := service.Create(context.Background(), 20, 1, "same-key")
	if err != nil {
		t.Fatalf("replayed Create() error = %v", err)
	}
	if !replayed.Replayed || replayed.Order.OrderNo != first.Order.OrderNo || len(repository.orders) != 1 {
		t.Fatalf("replay = %+v, orders=%d; want same single order", replayed, len(repository.orders))
	}
	if _, err := service.Create(context.Background(), 20, 2, "same-key"); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("Create(reused key for another book) error = %v, want ErrIdempotencyKeyReused", err)
	}
}

func TestOrderRedisIdempotencyAcquireCompleteAndReplay(t *testing.T) {
	ordersRepository := newFakeOrderRepository()
	ordersRepository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	ordersRepository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	idempotency := &fakeOrderIdempotency{claim: &repositorypkgIdempotencyClaimAcquired}
	service := newTestOrderServiceWithIdempotency(ordersRepository, idempotency, true)

	created, err := service.Create(context.Background(), 20, 1, "redis-key")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if idempotency.completedOrderNo != created.Order.OrderNo {
		t.Fatalf("completed order_no = %q, want %q", idempotency.completedOrderNo, created.Order.OrderNo)
	}

	idempotency.claim = &repository.IdempotencyClaim{
		State:   repository.IdempotencyCompleted,
		OrderNo: created.Order.OrderNo,
	}
	replayed, err := service.Create(context.Background(), 20, 1, "redis-key")
	if err != nil {
		t.Fatalf("Create(cached replay) error = %v", err)
	}
	if !replayed.Replayed || replayed.Order.OrderNo != created.Order.OrderNo || len(ordersRepository.orders) != 1 {
		t.Fatalf("cached replay = %+v, orders=%d; want original order", replayed, len(ordersRepository.orders))
	}
	metrics := service.IdempotencyMetrics()
	if metrics.Acquired != 1 || metrics.CacheReplays != 1 {
		t.Fatalf("metrics = %+v, want acquired=1 cache_replays=1", metrics)
	}
}

func TestOrderRedisIdempotencyRebuildsResultAfterCacheClear(t *testing.T) {
	ordersRepository := newFakeOrderRepository()
	ordersRepository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	ordersRepository.books[1] = &model.Book{
		ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable,
	}
	idempotency := &fakeOrderIdempotency{claim: &repositorypkgIdempotencyClaimAcquired}
	service := newTestOrderServiceWithIdempotency(ordersRepository, idempotency, true)

	created, err := service.Create(context.Background(), 20, 1, "cache-clear")
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	// A fresh acquired claim simulates Redis having lost the completed record.
	idempotency.completedOrderNo = ""
	idempotency.claim = &repositorypkgIdempotencyClaimAcquired
	replayed, err := service.Create(context.Background(), 20, 1, "cache-clear")
	if err != nil {
		t.Fatalf("Create(after cache clear) error = %v", err)
	}
	if !replayed.Replayed || replayed.Order.OrderNo != created.Order.OrderNo || len(ordersRepository.orders) != 1 {
		t.Fatalf("replay = %+v, orders=%d; want original single order", replayed, len(ordersRepository.orders))
	}
	if idempotency.completedOrderNo != created.Order.OrderNo {
		t.Fatalf("rebuilt completed order_no = %q, want %q", idempotency.completedOrderNo, created.Order.OrderNo)
	}
}

func TestOrderRedisIdempotencyProcessingConflictAndCommittedRecovery(t *testing.T) {
	ordersRepository := newFakeOrderRepository()
	ordersRepository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	ordersRepository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	idempotency := &fakeOrderIdempotency{claim: &repository.IdempotencyClaim{State: repository.IdempotencyProcessing}}
	service := newTestOrderServiceWithIdempotency(ordersRepository, idempotency, true)

	if _, err := service.Create(context.Background(), 20, 1, "processing-key"); !errors.Is(err, ErrRequestInProgress) {
		t.Fatalf("Create(processing) error = %v, want ErrRequestInProgress", err)
	}
	idempotency.claim = &repository.IdempotencyClaim{State: repository.IdempotencyConflict}
	if _, err := service.Create(context.Background(), 20, 1, "conflicting-key"); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("Create(conflict) error = %v, want ErrIdempotencyKeyReused", err)
	}

	idempotency.claim = &repositorypkgIdempotencyClaimAcquired
	created, err := service.Create(context.Background(), 20, 1, "committed-key")
	if err != nil {
		t.Fatalf("Create(committed) error = %v", err)
	}
	idempotency.completedOrderNo = ""
	idempotency.claim = &repository.IdempotencyClaim{State: repository.IdempotencyProcessing}
	replayed, err := service.Create(context.Background(), 20, 1, "committed-key")
	if err != nil || !replayed.Replayed || replayed.Order.OrderNo != created.Order.OrderNo {
		t.Fatalf("processing recovery = %+v/%v, want committed replay", replayed, err)
	}
	if idempotency.completedOrderNo != created.Order.OrderNo {
		t.Fatalf("recovered completed order_no = %q, want %q", idempotency.completedOrderNo, created.Order.OrderNo)
	}
	metrics := service.IdempotencyMetrics()
	if metrics.InProgress != 1 || metrics.Conflicts != 1 || metrics.CacheReplays != 1 {
		t.Fatalf("metrics = %+v, want in_progress/conflict/replay = 1", metrics)
	}
}

func TestOrderRedisFailureDegradesAccordingToConfiguration(t *testing.T) {
	redisError := errors.New("redis unavailable")

	degradedRepository := newFakeOrderRepository()
	degradedRepository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	degradedRepository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	degraded := newTestOrderServiceWithIdempotency(
		degradedRepository,
		&fakeOrderIdempotency{acquireErr: redisError},
		true,
	)
	if _, err := degraded.Create(context.Background(), 20, 1, "degraded-key"); err != nil {
		t.Fatalf("Create(degraded) error = %v, want MySQL fallback", err)
	}
	if degraded.IdempotencyMetrics().Degraded != 1 || len(degradedRepository.orders) != 1 {
		t.Fatalf("degraded metrics/orders = %+v/%d, want one fallback", degraded.IdempotencyMetrics(), len(degradedRepository.orders))
	}

	strictRepository := newFakeOrderRepository()
	strictRepository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	strictRepository.books[1] = &model.Book{ID: 1, SellerID: 10, Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	strict := newTestOrderServiceWithIdempotency(
		strictRepository,
		&fakeOrderIdempotency{acquireErr: redisError},
		false,
	)
	if _, err := strict.Create(context.Background(), 20, 1, "strict-key"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("Create(strict) error = %v, want ErrDependencyUnavailable", err)
	}
	if len(strictRepository.orders) != 0 {
		t.Fatalf("strict Redis failure created %d orders, want zero", len(strictRepository.orders))
	}
}

func TestOrderRedisClaimReleaseAndCompletionFailure(t *testing.T) {
	ordersRepository := newFakeOrderRepository()
	ordersRepository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	ordersRepository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	idempotency := &fakeOrderIdempotency{
		claim:       &repositorypkgIdempotencyClaimAcquired,
		completeErr: errors.New("cache write failed"),
	}
	service := newTestOrderServiceWithIdempotency(ordersRepository, idempotency, true)
	if _, err := service.Create(context.Background(), 20, 1, "complete-failure"); err != nil {
		t.Fatalf("Create(cache completion failure) error = %v, want committed success", err)
	}
	if !idempotency.releaseCalled || service.IdempotencyMetrics().CompletionFailures != 1 {
		t.Fatalf("release/metrics = %v/%+v, want cleanup and completion failure", idempotency.releaseCalled, service.IdempotencyMetrics())
	}

	failedRepository := newFakeOrderRepository()
	failedRepository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	failedRepository.books[1] = &model.Book{ID: 1, SellerID: 10, Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	failedIdempotency := &fakeOrderIdempotency{claim: &repositorypkgIdempotencyClaimAcquired}
	failed := newTestOrderServiceWithIdempotency(failedRepository, failedIdempotency, true)
	if _, err := failed.Create(context.Background(), 10, 1, "business-failure"); !errors.Is(err, ErrCannotPurchaseOwnBook) {
		t.Fatalf("Create(self-buy) error = %v, want ErrCannotPurchaseOwnBook", err)
	}
	if !failedIdempotency.releaseCalled {
		t.Fatal("business failure did not release owned Redis claim")
	}
}

func TestOrderConcurrentPurchaseHasOneWinner(t *testing.T) {
	repository := newFakeOrderRepository()
	repository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	service := newTestOrderService(repository)

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for buyerID := uint64(20); buyerID <= 21; buyerID++ {
		waitGroup.Add(1)
		go func(buyerID uint64) {
			defer waitGroup.Done()
			<-start
			_, err := service.Create(context.Background(), buyerID, 1, "buyer-key")
			results <- err
		}(buyerID)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes, unavailable := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBookNotAvailable):
			unavailable++
		default:
			t.Fatalf("unexpected Create() error = %v", err)
		}
	}
	if successes != 1 || unavailable != 1 || len(repository.orders) != 1 {
		t.Fatalf("success/unavailable/orders = %d/%d/%d, want 1/1/1", successes, unavailable, len(repository.orders))
	}
}

func TestOrderTwoPartyHandoverStateMachine(t *testing.T) {
	repo := newFakeOrderRepository()
	repo.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repo.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	svc := newTestOrderService(repo)
	ctx := context.Background()
	created, err := svc.Create(ctx, 20, 1, "offline-flow")
	if err != nil {
		t.Fatal(err)
	}
	orderNo := created.Order.OrderNo

	for _, actorID := range []uint64{10, 99} {
		if _, err := svc.Complete(ctx, actorID, orderNo); !errors.Is(err, ErrOrderAccessDenied) {
			t.Fatalf("Complete(actor=%d) error = %v, want access denied", actorID, err)
		}
	}
	if _, err := svc.Complete(ctx, 20, orderNo); !errors.Is(err, ErrOrderStateConflict) {
		t.Fatalf("Complete(before handover) = %v, want state conflict", err)
	}
	for _, actorID := range []uint64{20, 99} {
		if _, err := svc.Deliver(ctx, actorID, orderNo); !errors.Is(err, ErrOrderAccessDenied) {
			t.Fatalf("Deliver(actor=%d) error = %v, want access denied", actorID, err)
		}
	}
	delivered, err := svc.Deliver(ctx, 10, orderNo)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Status != string(model.OrderStatusPendingReceipt) || delivered.HandedOverAt == nil ||
		!delivered.HandedOverAt.Equal(repo.now) || delivered.CompletedAt != nil || repo.books[1].Status != model.BookStatusReserved {
		t.Fatalf("after handover = %+v, book = %s; want pending receipt, timestamp, reserved book", delivered, repo.books[1].Status)
	}
	handedOverAt := *delivered.HandedOverAt
	repo.now = created.Order.ExpiresAt.Add(time.Hour)
	repeated, err := svc.Deliver(ctx, 10, orderNo)
	if err != nil || repeated.HandedOverAt == nil || !repeated.HandedOverAt.Equal(handedOverAt) {
		t.Fatalf("repeated Deliver = %+v/%v, want original handover time", repeated, err)
	}
	if _, err := svc.Cancel(ctx, 20, orderNo); !errors.Is(err, ErrOrderStateConflict) {
		t.Fatalf("Cancel(after handover) = %v, want state conflict", err)
	}
	expired, err := svc.ExpireOverdue(ctx)
	if err != nil || expired != 0 {
		t.Fatalf("ExpireOverdue = %d/%v, want zero", expired, err)
	}
	current, err := svc.Get(ctx, &model.User{ID: 20}, orderNo)
	if err != nil || current.Status != string(model.OrderStatusPendingReceipt) {
		t.Fatalf("Get(after original deadline) = %+v/%v", current, err)
	}
	listed, err := svc.List(ctx, 20, dto.OrderListQuery{As: "buyer"})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].Status != string(model.OrderStatusPendingReceipt) {
		t.Fatalf("List(after original deadline) = %+v/%v", listed, err)
	}
	if _, err := svc.Create(ctx, 21, 1, "after-handover"); !errors.Is(err, ErrBookNotAvailable) {
		t.Fatalf("Create(after handover) = %v, want book unavailable", err)
	}
	completed, err := svc.Complete(ctx, 20, orderNo)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != string(model.OrderStatusCompleted) || repo.books[1].Status != model.BookStatusSold ||
		completed.CompletedAt == nil || !completed.CompletedAt.Equal(repo.now) || !completed.HandedOverAt.Equal(handedOverAt) {
		t.Fatalf("completed = %+v, book = %s; want both timestamps and sold", completed, repo.books[1].Status)
	}
	if repeated, err := svc.Complete(ctx, 20, orderNo); err != nil || repeated.Status != string(model.OrderStatusCompleted) {
		t.Fatalf("repeated Complete = %+v/%v", repeated, err)
	}
	if repeated, err := svc.Deliver(ctx, 10, orderNo); err != nil || repeated.Status != string(model.OrderStatusCompleted) {
		t.Fatalf("Deliver(after completion) = %+v/%v", repeated, err)
	}
	if _, err := svc.Cancel(ctx, 20, orderNo); !errors.Is(err, ErrOrderStateConflict) {
		t.Fatalf("Cancel(completed) = %v, want state conflict", err)
	}
	var eventTypes []string
	for _, event := range repo.outboxEvents {
		eventTypes = append(eventTypes, event.EventType)
	}
	wantEvents := []string{domainevent.OrderCreated, domainevent.OrderDelivered, domainevent.OrderCompleted}
	if fmt.Sprint(eventTypes) != fmt.Sprint(wantEvents) {
		t.Fatalf("events = %v, want %v with no duplicates", eventTypes, wantEvents)
	}
}

func TestOrderDeliveryRejectsCancelledOrders(t *testing.T) {
	repo := newFakeOrderRepository()
	repo.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repo.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	svc := newTestOrderService(repo)
	created, err := svc.Create(context.Background(), 20, 1, "cancel-before-delivery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(context.Background(), 20, created.Order.OrderNo); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Deliver(context.Background(), 10, created.Order.OrderNo); !errors.Is(err, ErrOrderStateConflict) {
		t.Fatalf("Deliver(cancelled) = %v, want state conflict", err)
	}
	if _, err := svc.Complete(context.Background(), 20, created.Order.OrderNo); !errors.Is(err, ErrOrderStateConflict) {
		t.Fatalf("Complete(cancelled) = %v, want state conflict", err)
	}
}

func TestOrderCancelAndExpirationReleaseBook(t *testing.T) {
	repository := newFakeOrderRepository()
	repository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	service := newTestOrderService(repository)

	first, err := service.Create(context.Background(), 20, 1, "cancel-flow")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	cancelled, err := service.Cancel(context.Background(), 20, first.Order.OrderNo)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelled.Status != string(model.OrderStatusCancelled) || repository.books[1].Status != model.BookStatusAvailable {
		t.Fatalf("cancelled/status = %+v/%q, want cancelled/available", cancelled, repository.books[1].Status)
	}
	if _, err := service.Cancel(context.Background(), 20, first.Order.OrderNo); err != nil {
		t.Fatalf("repeated Cancel() error = %v, want success", err)
	}

	second, err := service.Create(context.Background(), 21, 1, "expiring-flow")
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	repository.now = repository.now.Add(16 * time.Minute)
	third, err := service.Create(context.Background(), 22, 1, "after-expiry")
	if err != nil {
		t.Fatalf("Create(after expiry) error = %v", err)
	}
	if repository.orders[second.Order.OrderNo].Status != model.OrderStatusExpired ||
		third.Order.Status != string(model.OrderStatusPendingHandover) || repository.books[1].Status != model.BookStatusReserved {
		t.Fatalf("old/new/book = %q/%q/%q, want expired/pending/reserved",
			repository.orders[second.Order.OrderNo].Status, third.Order.Status, repository.books[1].Status)
	}
}

func TestOrderParticipantsCanCancelBeforeDelivery(t *testing.T) {
	for _, sellerCancels := range []bool{false, true} {
		for _, missingContact := range []bool{false, true} {
			t.Run(fmt.Sprintf("seller=%v/missing-contact=%v", sellerCancels, missingContact), func(t *testing.T) {
				repo := newFakeOrderRepository()
				repo.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
				repo.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
				svc := newTestOrderService(repo)
				ctx := context.Background()
				created, err := svc.Create(ctx, 20, 1, "participant-cancel")
				if err != nil {
					t.Fatal(err)
				}
				if missingContact {
					repo.books[1].WechatQRCodeURL = nil
				}
				if _, err := svc.Cancel(ctx, 99, created.Order.OrderNo); !errors.Is(err, ErrOrderAccessDenied) {
					t.Fatalf("Cancel(non-participant) = %v", err)
				}
				actor, reason := uint64(20), "cancelled by buyer"
				if sellerCancels {
					actor, reason = 10, "cancelled by seller"
				}
				cancelled, err := svc.Cancel(ctx, actor, created.Order.OrderNo)
				if err != nil {
					t.Fatal(err)
				}
				wantBookStatus := model.BookStatusAvailable
				if missingContact {
					wantBookStatus = model.BookStatusOffShelf
				}
				if cancelled.Status != string(model.OrderStatusCancelled) || cancelled.CancelReason == nil ||
					*cancelled.CancelReason != reason || cancelled.CancelledAt == nil || repo.books[1].Status != wantBookStatus {
					t.Fatalf("cancelled=%+v book=%s", cancelled, repo.books[1].Status)
				}
				cancelledAt := *cancelled.CancelledAt
				repo.now = repo.now.Add(time.Minute)
				for _, repeatActor := range []uint64{10, 20} {
					repeated, err := svc.Cancel(ctx, repeatActor, created.Order.OrderNo)
					if err != nil || *repeated.CancelReason != reason || !repeated.CancelledAt.Equal(cancelledAt) {
						t.Fatalf("Cancel(repeat actor=%d) = %+v/%v", repeatActor, repeated, err)
					}
				}
				if len(repo.outboxEvents) != 2 || repo.outboxEvents[1].EventType != domainevent.OrderCancelled {
					t.Fatalf("events = %+v, want created and one cancelled event", repo.outboxEvents)
				}
				var payload domainevent.OrderPayload
				if err := json.Unmarshal(repo.outboxEvents[1].Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.CancelReason == nil || *payload.CancelReason != reason {
					t.Fatalf("event reason = %v, want %s", payload.CancelReason, reason)
				}
			})
		}
	}
}

func TestSellerCannotCancelAfterDeliveryOrCompletion(t *testing.T) {
	repo := newFakeOrderRepository()
	repo.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repo.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	svc := newTestOrderService(repo)
	ctx := context.Background()
	created, err := svc.Create(ctx, 20, 1, "seller-cancel-after-delivery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Deliver(ctx, 10, created.Order.OrderNo); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(ctx, 10, created.Order.OrderNo); !errors.Is(err, ErrOrderStateConflict) {
		t.Fatalf("seller Cancel(delivered) = %v", err)
	}
	if _, err := svc.Complete(ctx, 20, created.Order.OrderNo); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(ctx, 10, created.Order.OrderNo); !errors.Is(err, ErrOrderStateConflict) {
		t.Fatalf("seller Cancel(completed) = %v", err)
	}
	if repo.books[1].Status != model.BookStatusSold || len(repo.outboxEvents) != 3 {
		t.Fatal("rejected cancellation changed completed book or emitted events")
	}
}

func TestOrderDeliveryRejectsExpiredAndKeepsIncompleteListingsOffShelf(t *testing.T) {
	for _, missingContact := range []bool{false, true} {
		t.Run(fmt.Sprint(missingContact), func(t *testing.T) {
			repo := newFakeOrderRepository()
			repo.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
			repo.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
			svc := newTestOrderService(repo)
			created, err := svc.Create(context.Background(), 20, 1, "expire-complete")
			if err != nil {
				t.Fatal(err)
			}
			if missingContact {
				repo.books[1].WechatQRCodeURL = nil
			}
			repo.now = created.Order.ExpiresAt
			if _, err := svc.Deliver(context.Background(), 10, created.Order.OrderNo); !errors.Is(err, ErrOrderStateConflict) {
				t.Fatalf("Deliver(expired) error = %v", err)
			}
			want := model.BookStatusAvailable
			if missingContact {
				want = model.BookStatusOffShelf
			}
			if repo.books[1].Status != want || repo.orders[created.Order.OrderNo].Status != model.OrderStatusExpired {
				t.Fatalf("book/order status = %s/%s", repo.books[1].Status, repo.orders[created.Order.OrderNo].Status)
			}
		})
	}
}

func TestOrderExpirationScannerUsesDatabaseTime(t *testing.T) {
	repository := newFakeOrderRepository()
	repository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	service := newTestOrderService(repository)
	created, err := service.Create(context.Background(), 20, 1, "scanner-flow")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	repository.now = repository.now.Add(15 * time.Minute)

	expired, err := service.ExpireOverdue(context.Background())
	if err != nil {
		t.Fatalf("ExpireOverdue() error = %v", err)
	}
	if expired != 1 || repository.orders[created.Order.OrderNo].Status != model.OrderStatusExpired ||
		repository.books[1].Status != model.BookStatusAvailable {
		t.Fatalf("expired/order/book = %d/%q/%q, want 1/expired/available", expired,
			repository.orders[created.Order.OrderNo].Status, repository.books[1].Status)
	}
}

func TestOrderVisibilityAndListsAreParticipantScoped(t *testing.T) {
	repository := newFakeOrderRepository()
	repository.users[10] = &model.User{ID: 10, Status: model.UserStatusActive}
	repository.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	service := newTestOrderService(repository)
	created, err := service.Create(context.Background(), 20, 1, "visibility")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	for _, actor := range []*model.User{
		{ID: 20, Role: model.UserRoleUser},
		{ID: 10, Role: model.UserRoleUser},
		{ID: 99, Role: model.UserRoleAdmin},
	} {
		if _, err := service.Get(context.Background(), actor, created.Order.OrderNo); err != nil {
			t.Fatalf("Get(actor=%+v) error = %v", actor, err)
		}
	}
	if _, err := service.Get(context.Background(), &model.User{ID: 30, Role: model.UserRoleUser}, created.Order.OrderNo); !errors.Is(err, ErrOrderAccessDenied) {
		t.Fatalf("Get(non-participant) error = %v, want ErrOrderAccessDenied", err)
	}
	buyerList, err := service.List(context.Background(), 20, dto.OrderListQuery{As: "buyer"})
	if err != nil || len(buyerList.Items) != 1 {
		t.Fatalf("buyer List() = %+v/%v, want one order", buyerList, err)
	}
	otherList, err := service.List(context.Background(), 30, dto.OrderListQuery{As: "buyer"})
	if err != nil || len(otherList.Items) != 0 {
		t.Fatalf("other List() = %+v/%v, want empty", otherList, err)
	}
}

func TestGenerateOrderNumberIsULIDShaped(t *testing.T) {
	orderNo, err := generateOrderNumber(time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatalf("generateOrderNumber() error = %v", err)
	}
	if len(orderNo) != 26 || orderNo[:10] != "01HF7YAT00" {
		t.Fatalf("order number = %q, want 26-character time-prefixed ULID", orderNo)
	}
}

func newTestOrderService(repository *fakeOrderRepository) *Order {
	return newTestOrderServiceWithIdempotency(repository, nil, true)
}

func newTestOrderServiceWithIdempotency(
	repository *fakeOrderRepository,
	idempotency *fakeOrderIdempotency,
	allowDegraded bool,
) *Order {
	var idempotencyBoundary orderIdempotency
	if idempotency != nil {
		idempotencyBoundary = idempotency
	}
	service := NewOrder(
		repository,
		idempotencyBoundary,
		15*time.Minute,
		OrderIdempotencyOptions{
			ProcessingTTL: 30 * time.Second,
			ResultTTL:     24 * time.Hour,
			AllowDegraded: allowDegraded,
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	service.orderNumber = func(time.Time) (string, error) {
		repository.orderSequence++
		return "01HF7YAT00" + leftPadOrderSequence(repository.orderSequence), nil
	}
	return service
}

var repositorypkgIdempotencyClaimAcquired = repository.IdempotencyClaim{State: repository.IdempotencyAcquired}

type fakeOrderIdempotency struct {
	claim            *repository.IdempotencyClaim
	acquireErr       error
	completeErr      error
	releaseErr       error
	completedOrderNo string
	releaseCalled    bool
}

func (f *fakeOrderIdempotency) Acquire(
	context.Context,
	uint64,
	string,
	string,
	time.Duration,
) (*repository.IdempotencyClaim, error) {
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	if f.claim == nil {
		return &repository.IdempotencyClaim{State: repository.IdempotencyAcquired}, nil
	}
	return f.claim, nil
}

func (f *fakeOrderIdempotency) Complete(
	_ context.Context,
	_ *repository.IdempotencyClaim,
	orderNo string,
	_ time.Duration,
) (bool, error) {
	f.completedOrderNo = orderNo
	if f.completeErr != nil {
		return false, f.completeErr
	}
	return true, nil
}

func (f *fakeOrderIdempotency) Release(context.Context, *repository.IdempotencyClaim) (bool, error) {
	f.releaseCalled = true
	if f.releaseErr != nil {
		return false, f.releaseErr
	}
	return true, nil
}

func leftPadOrderSequence(sequence uint64) string {
	alphabet := "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	result := make([]byte, 16)
	for index := len(result) - 1; index >= 0; index-- {
		result[index] = alphabet[sequence%32]
		sequence /= 32
	}
	return string(result)
}

type fakeOrderRepository struct {
	usernameLookups   int
	usernameLookupErr error
	mutex             sync.Mutex
	books             map[uint64]*model.Book
	users             map[uint64]*model.User
	orders            map[string]*model.Order
	nextID            uint64
	now               time.Time
	orderSequence     uint64
	outboxEvents      []model.OutboxEvent
}

func newFakeOrderRepository() *fakeOrderRepository {
	return &fakeOrderRepository{
		books:  make(map[uint64]*model.Book),
		users:  make(map[uint64]*model.User),
		orders: make(map[string]*model.Order),
		nextID: 1,
		now:    time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
	}
}

func (r *fakeOrderRepository) WithinTransaction(_ context.Context, operation func(repository.OrderStore) error) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return operation(r)
}

func (r *fakeOrderRepository) CurrentTime(context.Context) (time.Time, error) {
	return r.now, nil
}

func (r *fakeOrderRepository) FindByBuyerAndIdempotencyKey(_ context.Context, buyerID uint64, key string) (*model.Order, error) {
	for _, order := range r.orders {
		if order.BuyerID == buyerID && order.IdempotencyKey == key {
			return order, nil
		}
	}
	return nil, repository.ErrOrderNotFound
}

func (r *fakeOrderRepository) FindByBuyerAndIdempotencyKeyForUpdate(ctx context.Context, buyerID uint64, key string) (*model.Order, error) {
	return r.FindByBuyerAndIdempotencyKey(ctx, buyerID, key)
}

func (r *fakeOrderRepository) FindBookByIDForUpdate(_ context.Context, bookID uint64) (*model.Book, error) {
	book, ok := r.books[bookID]
	if !ok {
		return nil, repository.ErrBookNotFound
	}
	return book, nil
}

func (r *fakeOrderRepository) FindPendingByBookForUpdate(_ context.Context, bookID uint64) (*model.Order, error) {
	for _, order := range r.orders {
		if order.BookID == bookID && order.Status == model.OrderStatusPendingHandover {
			return order, nil
		}
	}
	return nil, repository.ErrOrderNotFound
}

func (r *fakeOrderRepository) FindUserByIDForShare(_ context.Context, userID uint64) (*model.User, error) {
	user, ok := r.users[userID]
	if !ok {
		return nil, repository.ErrUserNotFound
	}
	return user, nil
}

func (r *fakeOrderRepository) FindByOrderNo(_ context.Context, orderNo string) (*model.Order, error) {
	order, ok := r.orders[orderNo]
	if !ok {
		return nil, repository.ErrOrderNotFound
	}
	return order, nil
}

func (r *fakeOrderRepository) FindByOrderNoForUpdate(ctx context.Context, orderNo string) (*model.Order, error) {
	return r.FindByOrderNo(ctx, orderNo)
}

func (r *fakeOrderRepository) Create(_ context.Context, order *model.Order) error {
	for _, existing := range r.orders {
		if existing.BuyerID == order.BuyerID && existing.IdempotencyKey == order.IdempotencyKey {
			return repository.ErrOrderIdempotencyDuplicate
		}
		if existing.BookID == order.BookID && (existing.Status == model.OrderStatusPendingHandover ||
			existing.Status == model.OrderStatusPendingReceipt || existing.Status == model.OrderStatusCompleted) {
			return repository.ErrOrderActiveBookDuplicate
		}
	}
	order.ID = r.nextID
	r.nextID++
	order.CreatedAt = r.now
	order.UpdatedAt = r.now
	r.orders[order.OrderNo] = order
	return nil
}

func (r *fakeOrderRepository) CreateOutboxEvent(_ context.Context, outboxEvent *model.OutboxEvent) error {
	outboxEvent.ID = uint64(len(r.outboxEvents) + 1)
	r.outboxEvents = append(r.outboxEvents, *outboxEvent)
	return nil
}

func (r *fakeOrderRepository) UpdateBookStatus(_ context.Context, bookID uint64, from, to model.BookStatus) error {
	book, ok := r.books[bookID]
	if !ok || book.Status != from {
		return repository.ErrConcurrentUpdate
	}
	book.Status = to
	book.UpdatedAt = r.now
	return nil
}

func (r *fakeOrderRepository) UpdateOrderStatus(
	_ context.Context,
	orderID uint64,
	from, to model.OrderStatus,
	updates map[string]any,
) error {
	var found *model.Order
	for _, order := range r.orders {
		if order.ID == orderID {
			found = order
			break
		}
	}
	if found == nil || found.Status != from {
		return repository.ErrConcurrentUpdate
	}
	found.Status = to
	if value, ok := updates["handed_over_at"].(time.Time); ok {
		found.HandedOverAt = &value
	}
	if value, ok := updates["completed_at"].(time.Time); ok {
		found.CompletedAt = &value
	}
	if value, ok := updates["cancelled_at"].(time.Time); ok {
		found.CancelledAt = &value
	}
	if value, ok := updates["cancel_reason"].(string); ok {
		found.CancelReason = &value
	}
	found.UpdatedAt = r.now
	return nil
}

func (r *fakeOrderRepository) ListByBuyer(_ context.Context, buyerID uint64, page, pageSize int) ([]model.Order, int64, error) {
	return r.list(func(order *model.Order) bool { return order.BuyerID == buyerID }, page, pageSize)
}

func (r *fakeOrderRepository) ListBySeller(_ context.Context, sellerID uint64, page, pageSize int) ([]model.Order, int64, error) {
	return r.list(func(order *model.Order) bool { return order.SellerID == sellerID }, page, pageSize)
}

func (r *fakeOrderRepository) ListAll(_ context.Context, page, pageSize int) ([]model.Order, int64, error) {
	return r.list(func(*model.Order) bool { return true }, page, pageSize)
}

func (r *fakeOrderRepository) list(matches func(*model.Order) bool, page, pageSize int) ([]model.Order, int64, error) {
	var orders []model.Order
	for _, order := range r.orders {
		if matches(order) {
			orders = append(orders, *order)
		}
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i].ID > orders[j].ID })
	total := int64(len(orders))
	start := (page - 1) * pageSize
	if start >= len(orders) {
		return []model.Order{}, total, nil
	}
	end := min(start+pageSize, len(orders))
	return orders[start:end], total, nil
}

func (r *fakeOrderRepository) FindExpiredPendingOrderNos(_ context.Context, limit int) ([]string, error) {
	var orderNos []string
	for _, order := range r.orders {
		if order.Status == model.OrderStatusPendingHandover && !order.ExpiresAt.After(r.now) {
			orderNos = append(orderNos, order.OrderNo)
		}
	}
	if len(orderNos) > limit {
		orderNos = orderNos[:limit]
	}
	return orderNos, nil
}

func (r *fakeOrderRepository) FindUsernames(_ context.Context, ids []uint64) (map[uint64]string, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.usernameLookups++
	if r.usernameLookupErr != nil {
		return nil, r.usernameLookupErr
	}
	names := make(map[uint64]string)
	for _, id := range ids {
		if user := r.users[id]; user != nil {
			names[id] = user.Username
		}
	}
	return names, nil
}

func TestOrderResponsesUseDatabaseUsernames(t *testing.T) {
	repo := newFakeOrderRepository()
	repo.users[10] = &model.User{ID: 10, Username: "seller_reader", Status: model.UserStatusActive}
	repo.users[20] = &model.User{ID: 20, Username: "buyer_reader", Status: model.UserStatusActive}
	repo.books[1] = &model.Book{ID: 1, SellerID: 10, Title: "One", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
	svc := newTestOrderService(repo)
	ctx := context.Background()
	created, err := svc.Create(ctx, 20, 1, "display-usernames")
	if err != nil {
		t.Fatal(err)
	}
	checkNames := func(response dto.OrderResponse) {
		t.Helper()
		if response.BuyerUsername != "buyer_reader" || response.SellerUsername != "seller_reader" {
			t.Fatalf("unexpected usernames: %+v", response)
		}
	}
	checkNames(created.Order)
	replayed, err := svc.Create(ctx, 20, 1, "display-usernames")
	if err != nil {
		t.Fatal(err)
	}
	checkNames(replayed.Order)
	detail, err := svc.Get(ctx, repo.users[20], created.Order.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	checkNames(*detail)
	// Populate an entire page; names must be fetched once rather than per item.
	for i := 2; i <= 5; i++ {
		repo.books[uint64(i)] = &model.Book{ID: uint64(i), SellerID: 10, Title: "Another", Price: "10.00", Status: model.BookStatusAvailable, WechatQRCodeURL: testContactURL()}
		if _, err := svc.Create(ctx, 20, uint64(i), fmt.Sprintf("names-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	repo.usernameLookups = 0
	listed, err := svc.List(ctx, 20, dto.OrderListQuery{As: "buyer"})
	if err != nil || len(listed.Items) != 5 || repo.usernameLookups != 1 {
		t.Fatalf("list = %+v/%v, username queries = %d; want one batch", listed, err, repo.usernameLookups)
	}
	for _, item := range listed.Items {
		checkNames(item)
	}
	cancelled, err := svc.Cancel(ctx, 10, created.Order.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	checkNames(*cancelled)
	repo.usernameLookupErr = errors.New("username lookup unavailable")
	if _, err := svc.List(ctx, 20, dto.OrderListQuery{As: "buyer"}); !errors.Is(err, repo.usernameLookupErr) {
		t.Fatalf("lookup error silently ignored: %v", err)
	}
}
