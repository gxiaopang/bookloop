package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

var (
	ErrOrderNotFound             = errors.New("order not found")
	ErrOrderIdempotencyDuplicate = errors.New("order idempotency key duplicate")
	ErrOrderActiveBookDuplicate  = errors.New("active order for book duplicate")
	ErrOrderNumberDuplicate      = errors.New("order number duplicate")
	ErrConcurrentUpdate          = errors.New("concurrent update")
)

type OrderStore interface {
	CurrentTime(context.Context) (time.Time, error)
	FindByBuyerAndIdempotencyKey(context.Context, uint64, string) (*model.Order, error)
	FindByBuyerAndIdempotencyKeyForUpdate(context.Context, uint64, string) (*model.Order, error)
	FindBookByIDForUpdate(context.Context, uint64) (*model.Book, error)
	FindPendingByBookForUpdate(context.Context, uint64) (*model.Order, error)
	FindUserByIDForShare(context.Context, uint64) (*model.User, error)
	FindByOrderNoForUpdate(context.Context, string) (*model.Order, error)
	Create(context.Context, *model.Order) error
	UpdateBookStatus(context.Context, uint64, model.BookStatus, model.BookStatus) error
	UpdateOrderStatus(context.Context, uint64, model.OrderStatus, model.OrderStatus, map[string]any) error
}

type OrderRepository struct {
	db *gorm.DB
}

func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

func (r *OrderRepository) WithinTransaction(ctx context.Context, operation func(OrderStore) error) error {
	return r.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return operation(&OrderRepository{db: transaction})
	})
}

func (r *OrderRepository) CurrentTime(ctx context.Context) (time.Time, error) {
	var current time.Time
	err := r.db.WithContext(ctx).Raw("SELECT UTC_TIMESTAMP(3)").Scan(&current).Error
	return current.UTC(), err
}

func (r *OrderRepository) Create(ctx context.Context, order *model.Order) error {
	err := r.db.WithContext(ctx).Create(order).Error
	if err == nil {
		return nil
	}
	var mysqlError *drivermysql.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		switch {
		case strings.Contains(mysqlError.Message, "ux_orders_buyer_idempotency"):
			return ErrOrderIdempotencyDuplicate
		case strings.Contains(mysqlError.Message, "ux_orders_active_book"):
			return ErrOrderActiveBookDuplicate
		case strings.Contains(mysqlError.Message, "ux_orders_order_no"):
			return ErrOrderNumberDuplicate
		}
	}
	return err
}

func (r *OrderRepository) FindByBuyerAndIdempotencyKey(ctx context.Context, buyerID uint64, key string) (*model.Order, error) {
	var order model.Order
	err := r.db.WithContext(ctx).
		Where("buyer_id = ? AND idempotency_key = ?", buyerID, key).
		Take(&order).Error
	return orderResult(&order, err)
}

func (r *OrderRepository) FindByBuyerAndIdempotencyKeyForUpdate(
	ctx context.Context,
	buyerID uint64,
	key string,
) (*model.Order, error) {
	var order model.Order
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("buyer_id = ? AND idempotency_key = ?", buyerID, key).
		Take(&order).Error
	return orderResult(&order, err)
}

func (r *OrderRepository) FindByOrderNo(ctx context.Context, orderNo string) (*model.Order, error) {
	var order model.Order
	err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).Take(&order).Error
	return orderResult(&order, err)
}

func (r *OrderRepository) FindByOrderNoForUpdate(ctx context.Context, orderNo string) (*model.Order, error) {
	var order model.Order
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_no = ?", orderNo).
		Take(&order).Error
	return orderResult(&order, err)
}

func (r *OrderRepository) FindPendingByBookForUpdate(ctx context.Context, bookID uint64) (*model.Order, error) {
	var order model.Order
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("book_id = ? AND status = ?", bookID, model.OrderStatusPendingPayment).
		Take(&order).Error
	return orderResult(&order, err)
}

func (r *OrderRepository) FindBookByIDForUpdate(ctx context.Context, bookID uint64) (*model.Book, error) {
	var book model.Book
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", bookID).
		Take(&book).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBookNotFound
	}
	if err != nil {
		return nil, err
	}
	return &book, nil
}

func (r *OrderRepository) FindUserByIDForShare(ctx context.Context, userID uint64) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ?", userID).
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *OrderRepository) UpdateBookStatus(
	ctx context.Context,
	bookID uint64,
	from, to model.BookStatus,
) error {
	result := r.db.WithContext(ctx).
		Model(&model.Book{}).
		Where("id = ? AND status = ?", bookID, from).
		Update("status", to)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrConcurrentUpdate
	}
	return nil
}

func (r *OrderRepository) UpdateOrderStatus(
	ctx context.Context,
	orderID uint64,
	from, to model.OrderStatus,
	updates map[string]any,
) error {
	values := make(map[string]any, len(updates)+1)
	for key, value := range updates {
		values[key] = value
	}
	values["status"] = to
	result := r.db.WithContext(ctx).
		Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, from).
		Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrConcurrentUpdate
	}
	return nil
}

func (r *OrderRepository) ListByBuyer(ctx context.Context, buyerID uint64, page, pageSize int) ([]model.Order, int64, error) {
	return r.list(ctx, "buyer_id = ?", buyerID, page, pageSize)
}

func (r *OrderRepository) ListBySeller(ctx context.Context, sellerID uint64, page, pageSize int) ([]model.Order, int64, error) {
	return r.list(ctx, "seller_id = ?", sellerID, page, pageSize)
}

func (r *OrderRepository) ListAll(ctx context.Context, page, pageSize int) ([]model.Order, int64, error) {
	return r.list(ctx, "", nil, page, pageSize)
}

func (r *OrderRepository) list(
	ctx context.Context,
	condition string,
	argument any,
	page, pageSize int,
) ([]model.Order, int64, error) {
	database := r.db.WithContext(ctx).Model(&model.Order{})
	if condition != "" {
		database = database.Where(condition, argument)
	}
	var total int64
	if err := database.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []model.Order
	err := database.
		Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error
	if err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

func (r *OrderRepository) FindExpiredPendingOrderNos(ctx context.Context, limit int) ([]string, error) {
	var orderNos []string
	err := r.db.WithContext(ctx).
		Model(&model.Order{}).
		Where("status = ? AND expires_at <= UTC_TIMESTAMP(3)", model.OrderStatusPendingPayment).
		Order("expires_at ASC, id ASC").
		Limit(limit).
		Pluck("order_no", &orderNos).Error
	return orderNos, err
}

func IsRetryableTransactionError(err error) bool {
	var mysqlError *drivermysql.MySQLError
	return errors.As(err, &mysqlError) && (mysqlError.Number == 1205 || mysqlError.Number == 1213)
}

func orderResult(order *model.Order, err error) (*model.Order, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return order, nil
}
