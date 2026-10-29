package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
)

var (
	ErrBookNotFound      = errors.New("book not found")
	ErrBookImageNotFound = errors.New("book image not found")
)

type BookListRecord struct {
	ID            uint64
	SellerID      uint64
	Title         string
	Price         string
	Condition     model.BookCondition
	Status        model.BookStatus
	CoverImageURL *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type BookStore interface {
	FindByIDForUpdate(context.Context, uint64) (*model.Book, error)
	UpdateDetails(context.Context, *model.Book) error
	UpdateWechatQRCode(context.Context, uint64, string) error
	UpdateStatus(context.Context, uint64, model.BookStatus) error
	ListImages(context.Context, uint64) ([]model.BookImage, error)
	CountImages(context.Context, uint64) (int64, error)
	CreateImage(context.Context, *model.BookImage) error
	FindImageByID(context.Context, uint64, uint64) (*model.BookImage, error)
	DeleteImage(context.Context, uint64) error
	ReplaceImageOrder(context.Context, uint64, []uint64) error
}

type BookRepository struct {
	db *gorm.DB
}

func NewBookRepository(db *gorm.DB) *BookRepository {
	return &BookRepository{db: db}
}

func (r *BookRepository) WithinTransaction(ctx context.Context, operation func(BookStore) error) error {
	return r.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return operation(&BookRepository{db: transaction})
	})
}

func (r *BookRepository) Create(ctx context.Context, book *model.Book) error {
	return r.db.WithContext(ctx).Create(book).Error
}

func (r *BookRepository) FindByID(ctx context.Context, id uint64) (*model.Book, error) {
	return r.findBook(ctx, r.db.WithContext(ctx), id)
}

func (r *BookRepository) FindByIDForUpdate(ctx context.Context, id uint64) (*model.Book, error) {
	database := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"})
	return r.findBook(ctx, database, id)
}

func (r *BookRepository) findBook(_ context.Context, database *gorm.DB, id uint64) (*model.Book, error) {
	var book model.Book
	if err := database.Where("id = ?", id).Take(&book).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBookNotFound
		}
		return nil, err
	}
	return &book, nil
}

func (r *BookRepository) FindPublicByID(ctx context.Context, id uint64) (*model.Book, []model.BookImage, error) {
	var book model.Book
	err := r.db.WithContext(ctx).
		Table("books AS b").
		Select("b.*").
		Joins("JOIN users AS u ON u.id = b.seller_id").
		Where("b.id = ? AND b.status = ? AND u.status = ?", id, model.BookStatusAvailable, model.UserStatusActive).
		Take(&book).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrBookNotFound
		}
		return nil, nil, err
	}
	images, err := r.ListImages(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return &book, images, nil
}

func (r *BookRepository) ListPublic(ctx context.Context, query dto.BookListQuery) ([]BookListRecord, int64, error) {
	database := r.db.WithContext(ctx).
		Table("books AS b").
		Joins("JOIN users AS u ON u.id = b.seller_id").
		Where("b.status = ? AND u.status = ?", model.BookStatusAvailable, model.UserStatusActive)
	if query.Keyword != "" {
		database = database.Where("b.title LIKE ? ESCAPE '\\\\'", "%"+escapeLike(query.Keyword)+"%")
	}
	if query.Condition != "" {
		database = database.Where("b.`condition` = ?", query.Condition)
	}

	var total int64
	if err := database.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := "b.created_at DESC, b.id DESC"
	switch query.Sort {
	case "price_asc":
		order = "b.price ASC, b.id ASC"
	case "price_desc":
		order = "b.price DESC, b.id DESC"
	}
	var records []BookListRecord
	err := database.
		Select(`b.id, b.seller_id, b.title, b.price, b.condition, b.status,
            b.created_at, b.updated_at,
            (SELECT bi.image_url FROM book_images AS bi
             WHERE bi.book_id = b.id ORDER BY bi.sort_order ASC, bi.id ASC LIMIT 1) AS cover_image_url`).
		Order(order).
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Scan(&records).Error
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (r *BookRepository) ListBySeller(ctx context.Context, sellerID uint64, page, pageSize int) ([]BookListRecord, int64, error) {
	database := r.db.WithContext(ctx).Table("books AS b").Where("b.seller_id = ?", sellerID)
	var total int64
	if err := database.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []BookListRecord
	err := database.
		Select(`b.id, b.seller_id, b.title, b.price, b.condition, b.status,
            b.created_at, b.updated_at,
            (SELECT bi.image_url FROM book_images AS bi
             WHERE bi.book_id = b.id ORDER BY bi.sort_order ASC, bi.id ASC LIMIT 1) AS cover_image_url`).
		Order("b.created_at DESC, b.id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&records).Error
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (r *BookRepository) UpdateDetails(ctx context.Context, book *model.Book) error {
	return r.db.WithContext(ctx).Model(&model.Book{}).
		Where("id = ?", book.ID).
		Updates(map[string]any{
			"title":       book.Title,
			"description": book.Description,
			"price":       book.Price,
			"condition":   book.Condition,
		}).Error
}

func (r *BookRepository) UpdateStatus(ctx context.Context, bookID uint64, status model.BookStatus) error {
	return r.db.WithContext(ctx).Model(&model.Book{}).
		Where("id = ?", bookID).
		Update("status", status).Error
}

func (r *BookRepository) UpdateWechatQRCode(ctx context.Context, bookID uint64, imageURL string) error {
	return r.db.WithContext(ctx).Model(&model.Book{}).
		Where("id = ?", bookID).
		Update("wechat_qr_code_url", imageURL).Error
}

func (r *BookRepository) ListImages(ctx context.Context, bookID uint64) ([]model.BookImage, error) {
	var images []model.BookImage
	err := r.db.WithContext(ctx).
		Where("book_id = ?", bookID).
		Order("sort_order ASC, id ASC").
		Find(&images).Error
	return images, err
}

func (r *BookRepository) CountImages(ctx context.Context, bookID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BookImage{}).
		Where("book_id = ?", bookID).
		Count(&count).Error
	return count, err
}

func (r *BookRepository) CreateImage(ctx context.Context, image *model.BookImage) error {
	return r.db.WithContext(ctx).Create(image).Error
}

func (r *BookRepository) FindImageByID(ctx context.Context, bookID, imageID uint64) (*model.BookImage, error) {
	var image model.BookImage
	err := r.db.WithContext(ctx).
		Where("book_id = ? AND id = ?", bookID, imageID).
		Take(&image).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBookImageNotFound
	}
	if err != nil {
		return nil, err
	}
	return &image, nil
}

func (r *BookRepository) DeleteImage(ctx context.Context, imageID uint64) error {
	return r.db.WithContext(ctx).Delete(&model.BookImage{}, imageID).Error
}

func (r *BookRepository) ReplaceImageOrder(ctx context.Context, bookID uint64, imageIDs []uint64) error {
	for index, imageID := range imageIDs {
		result := r.db.WithContext(ctx).Model(&model.BookImage{}).
			Where("book_id = ? AND id = ?", bookID, imageID).
			Update("sort_order", 1000+index)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBookImageNotFound
		}
	}
	for index, imageID := range imageIDs {
		if err := r.db.WithContext(ctx).Model(&model.BookImage{}).
			Where("book_id = ? AND id = ?", bookID, imageID).
			Update("sort_order", index+1).Error; err != nil {
			return err
		}
	}
	return nil
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
