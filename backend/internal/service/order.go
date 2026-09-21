package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

const (
	maxIdempotencyKeyLength     = 64
	transactionAttempts         = 3
	expirationBatchSize         = 100
	idempotencyOperationTimeout = 500 * time.Millisecond
)

type orderRepository interface {
	WithinTransaction(context.Context, func(repository.OrderStore) error) error
	FindByBuyerAndIdempotencyKey(context.Context, uint64, string) (*model.Order, error)
	FindByOrderNo(context.Context, string) (*model.Order, error)
	ListByBuyer(context.Context, uint64, int, int) ([]model.Order, int64, error)
	ListBySeller(context.Context, uint64, int, int) ([]model.Order, int64, error)
	ListAll(context.Context, int, int) ([]model.Order, int64, error)
	FindExpiredPendingOrderNos(context.Context, int) ([]string, error)
}

type orderNumberGenerator func(time.Time) (string, error)

type orderIdempotency interface {
	Acquire(context.Context, uint64, string, string, time.Duration) (*repository.IdempotencyClaim, error)
	Complete(context.Context, *repository.IdempotencyClaim, string, time.Duration) (bool, error)
	Release(context.Context, *repository.IdempotencyClaim) (bool, error)
}

type OrderIdempotencyOptions struct {
	ProcessingTTL time.Duration
	ResultTTL     time.Duration
	AllowDegraded bool
}

type OrderIdempotencyMetrics struct {
	Acquired           uint64
	CacheReplays       uint64
	InProgress         uint64
	Conflicts          uint64
	Degraded           uint64
	CompletionFailures uint64
}

type orderIdempotencyCounters struct {
	acquired           atomic.Uint64
	cacheReplays       atomic.Uint64
	inProgress         atomic.Uint64
	conflicts          atomic.Uint64
	degraded           atomic.Uint64
	completionFailures atomic.Uint64
}

type Order struct {
	orders             orderRepository
	idempotency        orderIdempotency
	paymentTimeout     time.Duration
	idempotencyOptions OrderIdempotencyOptions
	orderNumber        orderNumberGenerator
	logger             *slog.Logger
	idempotencyMetrics orderIdempotencyCounters
}

type CreateOrderResult struct {
	Order    dto.OrderResponse
	Replayed bool
}

func NewOrder(
	orders orderRepository,
	idempotency orderIdempotency,
	paymentTimeout time.Duration,
	idempotencyOptions OrderIdempotencyOptions,
	logger *slog.Logger,
) *Order {
	return &Order{
		orders:             orders,
		idempotency:        idempotency,
		paymentTimeout:     paymentTimeout,
		idempotencyOptions: idempotencyOptions,
		orderNumber:        generateOrderNumber,
		logger:             logger,
	}
}

func (s *Order) Create(
	ctx context.Context,
	buyerID, bookID uint64,
	idempotencyKey string,
) (*CreateOrderResult, error) {
	if bookID == 0 || !validIdempotencyKey(idempotencyKey) {
		return nil, ErrValidation
	}
	fingerprint := orderRequestFingerprint(buyerID, bookID)
	if s.idempotency == nil {
		return s.createWithMySQL(ctx, buyerID, bookID, idempotencyKey, fingerprint)
	}

	claimContext, cancelClaim := context.WithTimeout(ctx, idempotencyOperationTimeout)
	claim, err := s.idempotency.Acquire(
		claimContext,
		buyerID,
		idempotencyKey,
		fingerprint,
		s.idempotencyOptions.ProcessingTTL,
	)
	cancelClaim()
	if err != nil {
		s.idempotencyMetrics.degraded.Add(1)
		s.logger.Warn("Redis idempotency acquisition failed",
			"buyer_id", buyerID,
			"error", err,
			"degraded", s.idempotencyOptions.AllowDegraded,
		)
		if !s.idempotencyOptions.AllowDegraded {
			return nil, ErrDependencyUnavailable
		}
		return s.createWithMySQL(ctx, buyerID, bookID, idempotencyKey, fingerprint)
	}

	switch claim.State {
	case repository.IdempotencyConflict:
		s.idempotencyMetrics.conflicts.Add(1)
		s.logger.Info("order idempotency key fingerprint conflict", "buyer_id", buyerID)
		return nil, ErrIdempotencyKeyReused

	case repository.IdempotencyCompleted:
		result, replayed, replayErr := s.replayCachedOrder(ctx, buyerID, fingerprint, claim.OrderNo)
		if replayErr != nil {
			return nil, replayErr
		}
		if replayed {
			s.idempotencyMetrics.cacheReplays.Add(1)
			s.logger.Info("replayed order from Redis idempotency result",
				"buyer_id", buyerID,
				"order_no", claim.OrderNo,
			)
			return result, nil
		}
		return s.createWithMySQL(ctx, buyerID, bookID, idempotencyKey, fingerprint)

	case repository.IdempotencyProcessing:
		result, replayed, replayErr := s.replayCommittedOrder(ctx, buyerID, idempotencyKey, fingerprint)
		if replayErr != nil {
			return nil, replayErr
		}
		if replayed {
			s.idempotencyMetrics.cacheReplays.Add(1)
			s.logger.Info("replayed committed order while Redis still marked processing",
				"buyer_id", buyerID,
				"order_no", result.Order.OrderNo,
			)
			// The original request may have committed MySQL and crashed before
			// publishing its result. The compare-and-set script makes it safe for
			// this retry to finish that processing record with the verified order.
			s.completeIdempotencyClaim(ctx, claim, result.Order.OrderNo, buyerID)
			return result, nil
		}
		s.idempotencyMetrics.inProgress.Add(1)
		s.logger.Info("duplicate order request is still processing", "buyer_id", buyerID)
		return nil, ErrRequestInProgress

	case repository.IdempotencyAcquired:
		s.idempotencyMetrics.acquired.Add(1)
	default:
		return nil, ErrDependencyUnavailable
	}

	result, err := s.createWithMySQL(ctx, buyerID, bookID, idempotencyKey, fingerprint)
	if err != nil {
		s.releaseIdempotencyClaim(ctx, claim, buyerID)
		return nil, err
	}
	s.completeIdempotencyClaim(ctx, claim, result.Order.OrderNo, buyerID)
	return result, nil
}

func (s *Order) createWithMySQL(
	ctx context.Context,
	buyerID, bookID uint64,
	idempotencyKey, fingerprint string,
) (*CreateOrderResult, error) {

	for orderNumberAttempt := 0; orderNumberAttempt < transactionAttempts; orderNumberAttempt++ {
		var (
			created             *model.Order
			expiredDuringCreate *model.Order
			replayed            bool
		)
		err := s.withTransactionRetry(ctx, func(store repository.OrderStore) error {
			created = nil
			expiredDuringCreate = nil
			replayed = false
			existing, err := store.FindByBuyerAndIdempotencyKey(ctx, buyerID, idempotencyKey)
			if err == nil {
				if existing.RequestFingerprint != fingerprint {
					return ErrIdempotencyKeyReused
				}
				created = existing
				replayed = true
				return nil
			}
			if !errors.Is(err, repository.ErrOrderNotFound) {
				return err
			}

			book, err := store.FindBookByIDForUpdate(ctx, bookID)
			if err != nil {
				return mapOrderRepositoryError(err)
			}

			// The request may have waited for the book lock while an identical
			// request committed. Recheck the database idempotency record now.
			existing, err = store.FindByBuyerAndIdempotencyKeyForUpdate(ctx, buyerID, idempotencyKey)
			if err == nil {
				if existing.RequestFingerprint != fingerprint {
					return ErrIdempotencyKeyReused
				}
				created = existing
				replayed = true
				return nil
			}
			if !errors.Is(err, repository.ErrOrderNotFound) {
				return err
			}

			now, err := store.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if book.Status == model.BookStatusReserved {
				pending, pendingErr := store.FindPendingByBookForUpdate(ctx, book.ID)
				if pendingErr != nil {
					if errors.Is(pendingErr, repository.ErrOrderNotFound) {
						return ErrBookNotAvailable
					}
					return pendingErr
				}
				if pending.ExpiresAt.After(now) {
					return ErrBookNotAvailable
				}
				if err := expireLocked(ctx, store, book, pending, now); err != nil {
					return err
				}
				expiredDuringCreate = pending
				book.Status = model.BookStatusAvailable
			}
			if book.Status != model.BookStatusAvailable {
				return ErrBookNotAvailable
			}
			if book.SellerID == buyerID {
				return ErrCannotPurchaseOwnBook
			}
			seller, err := store.FindUserByIDForShare(ctx, book.SellerID)
			if err != nil {
				return err
			}
			if seller.Status != model.UserStatusActive {
				return ErrBookNotAvailable
			}

			orderNo, err := s.orderNumber(now)
			if err != nil {
				return err
			}
			order := &model.Order{
				OrderNo:            orderNo,
				BookID:             book.ID,
				BuyerID:            buyerID,
				SellerID:           book.SellerID,
				BookTitle:          book.Title,
				BookPrice:          book.Price,
				Status:             model.OrderStatusPendingPayment,
				IdempotencyKey:     idempotencyKey,
				RequestFingerprint: fingerprint,
				ExpiresAt:          now.Add(s.paymentTimeout),
			}
			if err := store.Create(ctx, order); err != nil {
				return err
			}
			if err := store.UpdateBookStatus(ctx, book.ID, model.BookStatusAvailable, model.BookStatusReserved); err != nil {
				return err
			}
			created = order
			return nil
		})
		if err == nil {
			if expiredDuringCreate != nil {
				s.logTransition(expiredDuringCreate, 0,
					string(model.OrderStatusPendingPayment), string(model.OrderStatusExpired))
			}
			if !replayed {
				s.logTransition(created, buyerID, "", string(model.OrderStatusPendingPayment))
			}
			return &CreateOrderResult{Order: orderResponse(created), Replayed: replayed}, nil
		}
		if errors.Is(err, repository.ErrOrderNumberDuplicate) {
			continue
		}
		if errors.Is(err, repository.ErrOrderIdempotencyDuplicate) {
			existing, findErr := s.orders.FindByBuyerAndIdempotencyKey(ctx, buyerID, idempotencyKey)
			if findErr != nil {
				return nil, findErr
			}
			if existing.RequestFingerprint != fingerprint {
				return nil, ErrIdempotencyKeyReused
			}
			return &CreateOrderResult{Order: orderResponse(existing), Replayed: true}, nil
		}
		if errors.Is(err, repository.ErrOrderActiveBookDuplicate) {
			return nil, ErrBookNotAvailable
		}
		return nil, mapOrderRepositoryError(err)
	}
	return nil, repository.ErrOrderNumberDuplicate
}

func (s *Order) replayCachedOrder(
	ctx context.Context,
	buyerID uint64,
	fingerprint, orderNo string,
) (*CreateOrderResult, bool, error) {
	order, err := s.orders.FindByOrderNo(ctx, orderNo)
	if errors.Is(err, repository.ErrOrderNotFound) {
		s.logger.Warn("Redis completed idempotency result has no MySQL order",
			"buyer_id", buyerID,
			"order_no", orderNo,
		)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if order.BuyerID != buyerID || order.RequestFingerprint != fingerprint {
		s.logger.Warn("Redis completed idempotency result does not match MySQL",
			"buyer_id", buyerID,
			"order_no", orderNo,
		)
		return nil, false, nil
	}
	return &CreateOrderResult{Order: orderResponse(order), Replayed: true}, true, nil
}

func (s *Order) replayCommittedOrder(
	ctx context.Context,
	buyerID uint64,
	idempotencyKey, fingerprint string,
) (*CreateOrderResult, bool, error) {
	order, err := s.orders.FindByBuyerAndIdempotencyKey(ctx, buyerID, idempotencyKey)
	if errors.Is(err, repository.ErrOrderNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if order.RequestFingerprint != fingerprint {
		return nil, false, ErrIdempotencyKeyReused
	}
	return &CreateOrderResult{Order: orderResponse(order), Replayed: true}, true, nil
}

func (s *Order) completeIdempotencyClaim(
	ctx context.Context,
	claim *repository.IdempotencyClaim,
	orderNo string,
	buyerID uint64,
) {
	cacheContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), idempotencyOperationTimeout)
	completed, err := s.idempotency.Complete(
		cacheContext,
		claim,
		orderNo,
		s.idempotencyOptions.ResultTTL,
	)
	cancel()
	if err == nil && completed {
		return
	}
	s.idempotencyMetrics.completionFailures.Add(1)
	s.logger.Warn("failed to cache completed order idempotency result",
		"buyer_id", buyerID,
		"order_no", orderNo,
		"owner_matched", completed,
		"error", err,
	)
	s.releaseIdempotencyClaim(ctx, claim, buyerID)
}

func (s *Order) releaseIdempotencyClaim(
	ctx context.Context,
	claim *repository.IdempotencyClaim,
	buyerID uint64,
) {
	cacheContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), idempotencyOperationTimeout)
	released, err := s.idempotency.Release(cacheContext, claim)
	cancel()
	if err != nil {
		s.logger.Warn("failed to release order idempotency processing claim",
			"buyer_id", buyerID,
			"error", err,
		)
		return
	}
	if !released {
		s.logger.Info("order idempotency claim ownership changed before release", "buyer_id", buyerID)
	}
}

func (s *Order) IdempotencyMetrics() OrderIdempotencyMetrics {
	return OrderIdempotencyMetrics{
		Acquired:           s.idempotencyMetrics.acquired.Load(),
		CacheReplays:       s.idempotencyMetrics.cacheReplays.Load(),
		InProgress:         s.idempotencyMetrics.inProgress.Load(),
		Conflicts:          s.idempotencyMetrics.conflicts.Load(),
		Degraded:           s.idempotencyMetrics.degraded.Load(),
		CompletionFailures: s.idempotencyMetrics.completionFailures.Load(),
	}
}

func (s *Order) List(
	ctx context.Context,
	userID uint64,
	query dto.OrderListQuery,
) (*dto.OrderListResponse, error) {
	if query.As != "buyer" && query.As != "seller" {
		return nil, ErrValidation
	}
	page, pageSize, err := normalizePagination(query.Page, query.PageSize)
	if err != nil {
		return nil, err
	}
	load := func() ([]model.Order, int64, error) {
		if query.As == "buyer" {
			return s.orders.ListByBuyer(ctx, userID, page, pageSize)
		}
		return s.orders.ListBySeller(ctx, userID, page, pageSize)
	}
	orders, total, err := load()
	if err != nil {
		return nil, err
	}
	changed, err := s.expireOrdersInPage(ctx, orders)
	if err != nil {
		return nil, err
	}
	if changed {
		orders, total, err = load()
		if err != nil {
			return nil, err
		}
	}
	return orderListResponse(orders, page, pageSize, total), nil
}

func (s *Order) ListAll(ctx context.Context, administrator *model.User, page, pageSize int) (*dto.OrderListResponse, error) {
	if administrator == nil || administrator.Role != model.UserRoleAdmin {
		return nil, ErrForbidden
	}
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	orders, total, err := s.orders.ListAll(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	changed, err := s.expireOrdersInPage(ctx, orders)
	if err != nil {
		return nil, err
	}
	if changed {
		orders, total, err = s.orders.ListAll(ctx, page, pageSize)
		if err != nil {
			return nil, err
		}
	}
	return orderListResponse(orders, page, pageSize, total), nil
}

func (s *Order) Get(ctx context.Context, actor *model.User, orderNo string) (*dto.OrderResponse, error) {
	if actor == nil {
		return nil, ErrUnauthorized
	}
	order, err := s.orders.FindByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, mapOrderRepositoryError(err)
	}
	if !canViewOrder(actor, order) {
		return nil, ErrOrderAccessDenied
	}
	if order.Status == model.OrderStatusPendingPayment {
		order, _, err = s.expireIfDue(ctx, orderNo)
		if err != nil {
			return nil, err
		}
	}
	response := orderResponse(order)
	return &response, nil
}

func (s *Order) Pay(ctx context.Context, buyerID uint64, orderNo string) (*dto.OrderResponse, error) {
	return s.transition(ctx, buyerID, orderNo, orderActionPay)
}

func (s *Order) Cancel(ctx context.Context, buyerID uint64, orderNo string) (*dto.OrderResponse, error) {
	return s.transition(ctx, buyerID, orderNo, orderActionCancel)
}

func (s *Order) Complete(ctx context.Context, buyerID uint64, orderNo string) (*dto.OrderResponse, error) {
	return s.transition(ctx, buyerID, orderNo, orderActionComplete)
}

type orderAction string

const (
	orderActionPay      orderAction = "pay"
	orderActionCancel   orderAction = "cancel"
	orderActionComplete orderAction = "complete"
)

func (s *Order) transition(
	ctx context.Context,
	buyerID uint64,
	orderNo string,
	action orderAction,
) (*dto.OrderResponse, error) {
	snapshot, err := s.orders.FindByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, mapOrderRepositoryError(err)
	}
	if snapshot.BuyerID != buyerID {
		return nil, ErrOrderAccessDenied
	}

	var (
		changed       *model.Order
		postCommitErr error
		fromStatus    model.OrderStatus
	)
	err = s.withTransactionRetry(ctx, func(store repository.OrderStore) error {
		changed = nil
		postCommitErr = nil
		book, err := store.FindBookByIDForUpdate(ctx, snapshot.BookID)
		if err != nil {
			return mapOrderRepositoryError(err)
		}
		order, err := store.FindByOrderNoForUpdate(ctx, orderNo)
		if err != nil {
			return mapOrderRepositoryError(err)
		}
		if order.BuyerID != buyerID {
			return ErrOrderAccessDenied
		}
		fromStatus = order.Status
		now, err := store.CurrentTime(ctx)
		if err != nil {
			return err
		}

		switch action {
		case orderActionPay:
			if order.Status == model.OrderStatusPaid {
				changed = order
				return nil
			}
			if order.Status != model.OrderStatusPendingPayment {
				return ErrOrderStateConflict
			}
			if !order.ExpiresAt.After(now) {
				if err := expireLocked(ctx, store, book, order, now); err != nil {
					return err
				}
				changed = order
				postCommitErr = ErrOrderStateConflict
				return nil
			}
			if book.Status != model.BookStatusReserved {
				return ErrOrderStateConflict
			}
			if err := store.UpdateOrderStatus(ctx, order.ID, model.OrderStatusPendingPayment, model.OrderStatusPaid, map[string]any{
				"paid_at": now,
			}); err != nil {
				return err
			}
			if err := store.UpdateBookStatus(ctx, book.ID, model.BookStatusReserved, model.BookStatusSold); err != nil {
				return err
			}
			order.Status = model.OrderStatusPaid
			order.PaidAt = &now
			order.UpdatedAt = now
			changed = order

		case orderActionCancel:
			if order.Status == model.OrderStatusCancelled {
				changed = order
				return nil
			}
			if order.Status != model.OrderStatusPendingPayment || book.Status != model.BookStatusReserved {
				return ErrOrderStateConflict
			}
			if !order.ExpiresAt.After(now) {
				if err := expireLocked(ctx, store, book, order, now); err != nil {
					return err
				}
				changed = order
				postCommitErr = ErrOrderStateConflict
				return nil
			}
			reason := "cancelled by buyer"
			if err := store.UpdateOrderStatus(ctx, order.ID, model.OrderStatusPendingPayment, model.OrderStatusCancelled, map[string]any{
				"cancelled_at":  now,
				"cancel_reason": reason,
			}); err != nil {
				return err
			}
			if err := store.UpdateBookStatus(ctx, book.ID, model.BookStatusReserved, model.BookStatusAvailable); err != nil {
				return err
			}
			order.Status = model.OrderStatusCancelled
			order.CancelledAt = &now
			order.CancelReason = &reason
			order.UpdatedAt = now
			changed = order

		case orderActionComplete:
			if order.Status == model.OrderStatusCompleted {
				changed = order
				return nil
			}
			if order.Status != model.OrderStatusPaid || book.Status != model.BookStatusSold {
				return ErrOrderStateConflict
			}
			if err := store.UpdateOrderStatus(ctx, order.ID, model.OrderStatusPaid, model.OrderStatusCompleted, map[string]any{
				"completed_at": now,
			}); err != nil {
				return err
			}
			order.Status = model.OrderStatusCompleted
			order.CompletedAt = &now
			order.UpdatedAt = now
			changed = order
		}
		return nil
	})
	if err != nil {
		return nil, mapOrderRepositoryError(err)
	}
	if postCommitErr != nil {
		if changed != nil && changed.Status != fromStatus {
			s.logTransition(changed, buyerID, string(fromStatus), string(changed.Status))
		}
		return nil, postCommitErr
	}
	if changed.Status != fromStatus {
		s.logTransition(changed, buyerID, string(fromStatus), string(changed.Status))
	}
	response := orderResponse(changed)
	return &response, nil
}

func (s *Order) ExpireOverdue(ctx context.Context) (int, error) {
	orderNos, err := s.orders.FindExpiredPendingOrderNos(ctx, expirationBatchSize)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, orderNo := range orderNos {
		_, changed, err := s.expireIfDue(ctx, orderNo)
		if err != nil {
			return expired, err
		}
		if changed {
			expired++
		}
	}
	return expired, nil
}

func (s *Order) expireOrdersInPage(ctx context.Context, orders []model.Order) (bool, error) {
	changed := false
	for index := range orders {
		if orders[index].Status != model.OrderStatusPendingPayment {
			continue
		}
		_, expired, err := s.expireIfDue(ctx, orders[index].OrderNo)
		if err != nil {
			return false, err
		}
		changed = changed || expired
	}
	return changed, nil
}

func (s *Order) expireIfDue(ctx context.Context, orderNo string) (*model.Order, bool, error) {
	snapshot, err := s.orders.FindByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, false, mapOrderRepositoryError(err)
	}
	var (
		current *model.Order
		expired bool
	)
	err = s.withTransactionRetry(ctx, func(store repository.OrderStore) error {
		current = nil
		expired = false
		book, err := store.FindBookByIDForUpdate(ctx, snapshot.BookID)
		if err != nil {
			return mapOrderRepositoryError(err)
		}
		now, err := store.CurrentTime(ctx)
		if err != nil {
			return err
		}
		order, err := store.FindByOrderNoForUpdate(ctx, orderNo)
		if err != nil {
			return mapOrderRepositoryError(err)
		}
		current = order
		if order.Status != model.OrderStatusPendingPayment || order.ExpiresAt.After(now) {
			return nil
		}
		if err := expireLocked(ctx, store, book, order, now); err != nil {
			return err
		}
		expired = true
		return nil
	})
	if err != nil {
		return nil, false, mapOrderRepositoryError(err)
	}
	if expired {
		s.logTransition(current, 0, string(model.OrderStatusPendingPayment), string(model.OrderStatusExpired))
	}
	return current, expired, nil
}

func expireLocked(
	ctx context.Context,
	store repository.OrderStore,
	book *model.Book,
	order *model.Order,
	now time.Time,
) error {
	if book.ID != order.BookID || book.Status != model.BookStatusReserved || order.Status != model.OrderStatusPendingPayment {
		return ErrOrderStateConflict
	}
	reason := "payment timeout"
	if err := store.UpdateOrderStatus(ctx, order.ID, model.OrderStatusPendingPayment, model.OrderStatusExpired, map[string]any{
		"cancelled_at":  now,
		"cancel_reason": reason,
	}); err != nil {
		return err
	}
	if err := store.UpdateBookStatus(ctx, book.ID, model.BookStatusReserved, model.BookStatusAvailable); err != nil {
		return err
	}
	order.Status = model.OrderStatusExpired
	order.CancelledAt = &now
	order.CancelReason = &reason
	order.UpdatedAt = now
	book.Status = model.BookStatusAvailable
	return nil
}

func (s *Order) withTransactionRetry(ctx context.Context, operation func(repository.OrderStore) error) error {
	var err error
	for attempt := 0; attempt < transactionAttempts; attempt++ {
		err = s.orders.WithinTransaction(ctx, operation)
		if !repository.IsRetryableTransactionError(err) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}

func validIdempotencyKey(key string) bool {
	if len(key) == 0 || len(key) > maxIdempotencyKeyLength {
		return false
	}
	for index := range len(key) {
		if key[index] < 0x21 || key[index] > 0x7e {
			return false
		}
	}
	return true
}

func orderRequestFingerprint(buyerID, bookID uint64) string {
	canonical := "v1:create_order:" + strconv.FormatUint(buyerID, 10) + ":" + strconv.FormatUint(bookID, 10)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
}

func canViewOrder(actor *model.User, order *model.Order) bool {
	return actor.Role == model.UserRoleAdmin || actor.ID == order.BuyerID || actor.ID == order.SellerID
}

func mapOrderRepositoryError(err error) error {
	switch {
	case errors.Is(err, repository.ErrOrderNotFound):
		return ErrOrderNotFound
	case errors.Is(err, repository.ErrBookNotFound):
		return ErrBookNotFound
	case errors.Is(err, repository.ErrOrderActiveBookDuplicate):
		return ErrBookNotAvailable
	default:
		return err
	}
}

func orderResponse(order *model.Order) dto.OrderResponse {
	return dto.OrderResponse{
		OrderNo:      order.OrderNo,
		BookID:       order.BookID,
		BuyerID:      order.BuyerID,
		SellerID:     order.SellerID,
		BookTitle:    order.BookTitle,
		BookPrice:    order.BookPrice,
		Status:       string(order.Status),
		ExpiresAt:    order.ExpiresAt,
		PaidAt:       order.PaidAt,
		CompletedAt:  order.CompletedAt,
		CancelledAt:  order.CancelledAt,
		CancelReason: order.CancelReason,
		CreatedAt:    order.CreatedAt,
		UpdatedAt:    order.UpdatedAt,
	}
}

func orderListResponse(orders []model.Order, page, pageSize int, total int64) *dto.OrderListResponse {
	items := make([]dto.OrderResponse, 0, len(orders))
	for index := range orders {
		items = append(items, orderResponse(&orders[index]))
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &dto.OrderListResponse{
		Items: items,
		Pagination: dto.PaginationResponse{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	}
}

func (s *Order) logTransition(order *model.Order, actorID uint64, from, to string) {
	s.logger.Info("order state changed",
		"order_no", order.OrderNo,
		"book_id", order.BookID,
		"actor_id", actorID,
		"from_status", from,
		"to_status", to,
	)
}
