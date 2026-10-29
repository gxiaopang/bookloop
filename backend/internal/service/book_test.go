package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/storage"
)

func TestBookCreateValidatesAndForcesOffShelf(t *testing.T) {
	repository := newFakeBookRepository()
	service := newTestBookService(repository)

	created, err := service.Create(context.Background(), 42, dto.CreateBookRequest{
		Title:     " Go in Action ",
		Price:     "12.5",
		Condition: "good",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.SellerID != 42 || created.Status != "off_shelf" || created.Price != "12.50" || created.Title != "Go in Action" {
		t.Fatalf("Create() = %+v, want normalized off-shelf book", created)
	}
	if created.Description != nil || len(created.Images) != 0 {
		t.Fatalf("Create() description/images = %+v/%+v, want nil/empty", created.Description, created.Images)
	}

	for _, request := range []dto.CreateBookRequest{
		{Title: "", Price: "12.00", Condition: "good"},
		{Title: "Book", Price: "0.00", Condition: "good"},
		{Title: "Book", Price: "12.001", Condition: "good"},
		{Title: "Book", Price: "12.00", Condition: "unknown"},
	} {
		if _, err := service.Create(context.Background(), 42, request); !errors.Is(err, ErrValidation) {
			t.Fatalf("Create(%+v) error = %v, want ErrValidation", request, err)
		}
	}
}

func TestBookGetMineReturnsPrivateStatusesOnlyToOwner(t *testing.T) {
	repository := newFakeBookRepository()
	book := repository.addBook(10, model.BookStatusOffShelf)
	repository.addImage(book.ID, 1)
	service := newTestBookService(repository)

	result, err := service.GetMine(context.Background(), 10, book.ID)
	if err != nil {
		t.Fatalf("GetMine(owner) error = %v", err)
	}
	if result.ID != book.ID || result.Status != string(model.BookStatusOffShelf) || len(result.Images) != 1 {
		t.Fatalf("GetMine(owner) = %+v, want private book with images", result)
	}
	if _, err := service.GetMine(context.Background(), 99, book.ID); !errors.Is(err, ErrBookNotOwner) {
		t.Fatalf("GetMine(non-owner) error = %v, want ErrBookNotOwner", err)
	}
}

func TestBookOwnerAndStateRules(t *testing.T) {
	repository := newFakeBookRepository()
	book := repository.addBook(10, model.BookStatusOffShelf)
	service := newTestBookService(repository)

	newTitle := "Changed"
	if _, err := service.Update(context.Background(), 99, book.ID, dto.UpdateBookRequest{Title: &newTitle}); !errors.Is(err, ErrBookNotOwner) {
		t.Fatalf("Update(non-owner) error = %v, want ErrBookNotOwner", err)
	}
	if _, err := service.PutOnShelf(context.Background(), 10, book.ID); !errors.Is(err, ErrBookImageRequired) {
		t.Fatalf("PutOnShelf(no image) error = %v, want ErrBookImageRequired", err)
	}
	repository.addImage(book.ID, 1)
	if _, err := service.PutOnShelf(context.Background(), 99, book.ID); !errors.Is(err, ErrBookNotOwner) {
		t.Fatalf("PutOnShelf(non-owner) error = %v, want ErrBookNotOwner", err)
	}
	if _, err := service.PutOnShelf(context.Background(), 10, book.ID); !errors.Is(err, ErrBookContactRequired) {
		t.Fatalf("PutOnShelf(no contact) error = %v, want ErrBookContactRequired", err)
	}
	if _, err := service.UploadWechatQRCode(context.Background(), 10, book.ID, strings.NewReader("image")); err != nil {
		t.Fatalf("UploadWechatQRCode() error = %v", err)
	}
	if _, err := service.PutOnShelf(context.Background(), 10, book.ID); err != nil {
		t.Fatalf("PutOnShelf() error = %v", err)
	}
	if repository.books[book.ID].Status != model.BookStatusAvailable {
		t.Fatalf("book status = %q, want available", repository.books[book.ID].Status)
	}
	imageID := repository.images[book.ID][0].ID
	if _, err := service.TakeOffShelf(context.Background(), 99, book.ID); !errors.Is(err, ErrBookNotOwner) {
		t.Fatalf("TakeOffShelf(non-owner) error = %v, want ErrBookNotOwner", err)
	}
	if _, err := service.UploadImage(context.Background(), 99, book.ID, strings.NewReader("image")); !errors.Is(err, ErrBookNotOwner) {
		t.Fatalf("UploadImage(non-owner) error = %v, want ErrBookNotOwner", err)
	}
	if err := service.DeleteImage(context.Background(), 99, book.ID, imageID); !errors.Is(err, ErrBookNotOwner) {
		t.Fatalf("DeleteImage(non-owner) error = %v, want ErrBookNotOwner", err)
	}
	if _, err := service.ReorderImages(context.Background(), 99, book.ID, []uint64{imageID}); !errors.Is(err, ErrBookNotOwner) {
		t.Fatalf("ReorderImages(non-owner) error = %v, want ErrBookNotOwner", err)
	}
	if err := service.DeleteImage(context.Background(), 10, book.ID, imageID); !errors.Is(err, ErrBookImageRequired) {
		t.Fatalf("DeleteImage(last available image) error = %v, want ErrBookImageRequired", err)
	}

	repository.books[book.ID].Status = model.BookStatusBlocked
	if _, err := service.Update(context.Background(), 10, book.ID, dto.UpdateBookRequest{Title: &newTitle}); !errors.Is(err, ErrBookStateConflict) {
		t.Fatalf("Update(blocked) error = %v, want ErrBookStateConflict", err)
	}
}

func TestBookUploadImageLimitIsSafeUnderConcurrency(t *testing.T) {
	repository := newFakeBookRepository()
	book := repository.addBook(10, model.BookStatusOffShelf)
	for index := 1; index <= 5; index++ {
		repository.addImage(book.ID, uint16(index))
	}
	service := newTestBookService(repository)

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := service.UploadImage(context.Background(), 10, book.ID, strings.NewReader("image"))
			results <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes := 0
	limitErrors := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBookImageLimitReached):
			limitErrors++
		default:
			t.Fatalf("UploadImage() unexpected error = %v", err)
		}
	}
	if successes != 1 || limitErrors != 1 || len(repository.images[book.ID]) != 6 {
		t.Fatalf("uploads success/limit/count = %d/%d/%d, want 1/1/6", successes, limitErrors, len(repository.images[book.ID]))
	}
}

func TestBookUploadCompensatesFileWhenDatabaseWriteFails(t *testing.T) {
	repository := newFakeBookRepository()
	book := repository.addBook(10, model.BookStatusOffShelf)
	repository.createImageErr = errors.New("database write failed")
	images := &fakeImageStorage{}
	service := NewBook(repository, images, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := service.UploadImage(context.Background(), 10, book.ID, strings.NewReader("image")); err == nil {
		t.Fatal("UploadImage() error = nil, want database error")
	}
	images.mutex.Lock()
	defer images.mutex.Unlock()
	if len(images.deleted) != 1 || !strings.HasPrefix(images.deleted[0], "/uploads/books/generated-") {
		t.Fatalf("compensating deletes = %+v, want committed file deletion", images.deleted)
	}
}

func TestBookContactUploadRequiresImagesOwnerAndMutableStatus(t *testing.T) {
	for _, test := range []struct {
		name      string
		sellerID  uint64
		status    model.BookStatus
		withImage bool
		want      error
	}{
		{"images first", 10, model.BookStatusOffShelf, false, ErrBookImageRequired},
		{"owner only", 99, model.BookStatusOffShelf, true, ErrBookNotOwner},
		{"reserved", 10, model.BookStatusReserved, true, ErrBookStateConflict},
		{"sold", 10, model.BookStatusSold, true, ErrBookStateConflict},
		{"blocked", 10, model.BookStatusBlocked, true, ErrBookStateConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeBookRepository()
			book := repo.addBook(10, test.status)
			if test.withImage {
				repo.addImage(book.ID, 1)
			}
			_, err := newTestBookService(repo).UploadWechatQRCode(context.Background(), test.sellerID, book.ID, strings.NewReader("image"))
			if !errors.Is(err, test.want) {
				t.Fatalf("upload error = %v, want %v", err, test.want)
			}
			if book.WechatQRCodeURL != nil {
				t.Fatal("rejected upload changed contact")
			}
		})
	}
}

func TestBookContactReplacementAndDatabaseFailureCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			repo := newFakeBookRepository()
			book := repo.addBook(10, model.BookStatusAvailable)
			repo.addImage(book.ID, 1)
			book.WechatQRCodeURL = testContactURL()
			oldURL := *book.WechatQRCodeURL
			if fail {
				repo.updateContactErr = errors.New("database unavailable")
			}
			files := &fakeImageStorage{}
			svc := NewBook(repo, files, slog.New(slog.NewTextHandler(io.Discard, nil)))
			result, err := svc.UploadWechatQRCode(context.Background(), 10, book.ID, strings.NewReader("image"))
			if fail {
				if !errors.Is(err, repo.updateContactErr) || *book.WechatQRCodeURL != oldURL ||
					len(files.deleted) != 1 || files.deleted[0] == oldURL {
					t.Fatalf("failed replacement result/error/contact/deletes = %+v/%v/%v/%v", result, err, book.WechatQRCodeURL, files.deleted)
				}
			} else {
				if err != nil || result.WechatQRCodeURL == oldURL || *book.WechatQRCodeURL != result.WechatQRCodeURL ||
					len(files.deleted) != 1 || files.deleted[0] != oldURL {
					t.Fatalf("replacement result/error/deletes = %+v/%v/%v", result, err, files.deleted)
				}
				public, err := svc.GetPublic(context.Background(), book.ID)
				if err != nil || public.WechatQRCodeURL == nil || *public.WechatQRCodeURL != result.WechatQRCodeURL {
					t.Fatalf("public contact = %+v/%v", public, err)
				}
			}
			if len(repo.images[book.ID]) != 1 {
				t.Fatal("contact upload changed product images")
			}
		})
	}
}

func TestBookContactRejectsInvalidImageContent(t *testing.T) {
	repo := newFakeBookRepository()
	book := repo.addBook(10, model.BookStatusOffShelf)
	repo.addImage(book.ID, 1)
	files, err := storage.NewLocalImages(config.UploadConfig{Directory: t.TempDir(), PublicPrefix: "/uploads/", MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewBook(repo, files, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := svc.UploadWechatQRCode(context.Background(), 10, book.ID, strings.NewReader("not a PNG")); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("invalid image error = %v, want ErrInvalidImage", err)
	}
	if book.WechatQRCodeURL != nil {
		t.Fatal("invalid image changed contact")
	}
}

func TestBookImageReorderRequiresCompleteUniqueOwnedSet(t *testing.T) {
	repository := newFakeBookRepository()
	book := repository.addBook(10, model.BookStatusOffShelf)
	first := repository.addImage(book.ID, 1)
	second := repository.addImage(book.ID, 2)
	service := newTestBookService(repository)

	if _, err := service.ReorderImages(context.Background(), 10, book.ID, []uint64{first.ID, first.ID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("ReorderImages(duplicate) error = %v, want ErrValidation", err)
	}
	images, err := service.ReorderImages(context.Background(), 10, book.ID, []uint64{second.ID, first.ID})
	if err != nil {
		t.Fatalf("ReorderImages() error = %v", err)
	}
	if images[0].ID != second.ID || images[0].SortOrder != 1 || images[1].ID != first.ID || images[1].SortOrder != 2 {
		t.Fatalf("ReorderImages() = %+v, want reversed compact order", images)
	}
}

func TestBookImageMutationsRejectFrozenStatuses(t *testing.T) {
	for _, status := range []model.BookStatus{model.BookStatusReserved, model.BookStatusSold, model.BookStatusBlocked} {
		t.Run(string(status), func(t *testing.T) {
			repository := newFakeBookRepository()
			book := repository.addBook(10, status)
			image := repository.addImage(book.ID, 1)
			service := newTestBookService(repository)
			if _, err := service.UploadImage(context.Background(), 10, book.ID, strings.NewReader("image")); !errors.Is(err, ErrBookStateConflict) {
				t.Fatalf("UploadImage() error = %v, want ErrBookStateConflict", err)
			}
			if err := service.DeleteImage(context.Background(), 10, book.ID, image.ID); !errors.Is(err, ErrBookStateConflict) {
				t.Fatalf("DeleteImage() error = %v, want ErrBookStateConflict", err)
			}
			if _, err := service.ReorderImages(context.Background(), 10, book.ID, []uint64{image.ID}); !errors.Is(err, ErrBookStateConflict) {
				t.Fatalf("ReorderImages() error = %v, want ErrBookStateConflict", err)
			}
		})
	}
}

func TestBookAdminBlockAndUnblockUsesCurrentDatabaseRole(t *testing.T) {
	repository := newFakeBookRepository()
	book := repository.addBook(10, model.BookStatusAvailable)
	service := newTestBookService(repository)

	ordinaryUser := &model.User{ID: 1, Role: model.UserRoleUser, Status: model.UserStatusActive}
	if _, err := service.Block(context.Background(), ordinaryUser, book.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Block(user) error = %v, want ErrForbidden", err)
	}
	administrator := &model.User{ID: 2, Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if _, err := service.Block(context.Background(), administrator, book.ID); err != nil {
		t.Fatalf("Block(admin) error = %v", err)
	}
	if repository.books[book.ID].Status != model.BookStatusBlocked {
		t.Fatalf("status = %q, want blocked", repository.books[book.ID].Status)
	}
	if _, err := service.Unblock(context.Background(), administrator, book.ID); err != nil {
		t.Fatalf("Unblock(admin) error = %v", err)
	}
	if repository.books[book.ID].Status != model.BookStatusOffShelf {
		t.Fatalf("status = %q, want off_shelf", repository.books[book.ID].Status)
	}
	repository.books[book.ID].Status = model.BookStatusReserved
	if _, err := service.Block(context.Background(), administrator, book.ID); !errors.Is(err, ErrBookStateConflict) {
		t.Fatalf("Block(reserved) error = %v, want ErrBookStateConflict", err)
	}
}

func TestBookListQueryValidationAndNormalization(t *testing.T) {
	repository := newFakeBookRepository()
	service := newTestBookService(repository)

	response, err := service.ListPublic(context.Background(), dto.BookListQuery{Condition: " good "})
	if err != nil {
		t.Fatalf("ListPublic() error = %v", err)
	}
	if repository.lastListQuery.Condition != "good" || repository.lastListQuery.Sort != "newest" ||
		response.Pagination.Page != 1 || response.Pagination.PageSize != 20 {
		t.Fatalf("normalized query/response = %+v/%+v", repository.lastListQuery, response.Pagination)
	}
	if _, err := service.ListPublic(context.Background(), dto.BookListQuery{Sort: "unsupported"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("ListPublic(unsupported sort) error = %v, want ErrValidation", err)
	}
}

func newTestBookService(repository *fakeBookRepository) *Book {
	return NewBook(repository, &fakeImageStorage{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

type fakeBookRepository struct {
	transactionMutex sync.Mutex
	books            map[uint64]*model.Book
	images           map[uint64][]model.BookImage
	nextBookID       uint64
	nextImageID      uint64
	lastListQuery    dto.BookListQuery
	createImageErr   error
	updateContactErr error
}

func newFakeBookRepository() *fakeBookRepository {
	return &fakeBookRepository{
		books:       make(map[uint64]*model.Book),
		images:      make(map[uint64][]model.BookImage),
		nextBookID:  1,
		nextImageID: 1,
	}
}

func (r *fakeBookRepository) WithinTransaction(_ context.Context, operation func(repository.BookStore) error) error {
	r.transactionMutex.Lock()
	defer r.transactionMutex.Unlock()
	return operation(r)
}

func (r *fakeBookRepository) Create(_ context.Context, book *model.Book) error {
	book.ID = r.nextBookID
	r.nextBookID++
	book.CreatedAt = time.Now().UTC()
	book.UpdatedAt = book.CreatedAt
	r.books[book.ID] = book
	return nil
}

func (r *fakeBookRepository) FindByID(_ context.Context, id uint64) (*model.Book, error) {
	book, ok := r.books[id]
	if !ok {
		return nil, repository.ErrBookNotFound
	}
	return book, nil
}

func (r *fakeBookRepository) FindByIDForUpdate(ctx context.Context, id uint64) (*model.Book, error) {
	return r.FindByID(ctx, id)
}

func (r *fakeBookRepository) FindPublicByID(ctx context.Context, id uint64) (*model.Book, []model.BookImage, error) {
	book, err := r.FindByID(ctx, id)
	if err != nil || book.Status != model.BookStatusAvailable {
		return nil, nil, repository.ErrBookNotFound
	}
	images, _ := r.ListImages(ctx, id)
	return book, images, nil
}

func (r *fakeBookRepository) ListPublic(_ context.Context, query dto.BookListQuery) ([]repository.BookListRecord, int64, error) {
	r.lastListQuery = query
	return []repository.BookListRecord{}, 0, nil
}

func (r *fakeBookRepository) ListBySeller(_ context.Context, _ uint64, _, _ int) ([]repository.BookListRecord, int64, error) {
	return []repository.BookListRecord{}, 0, nil
}

func (r *fakeBookRepository) UpdateDetails(_ context.Context, book *model.Book) error {
	r.books[book.ID] = book
	return nil
}

func (r *fakeBookRepository) UpdateStatus(_ context.Context, bookID uint64, status model.BookStatus) error {
	r.books[bookID].Status = status
	return nil
}

func (r *fakeBookRepository) ListImages(_ context.Context, bookID uint64) ([]model.BookImage, error) {
	images := append([]model.BookImage(nil), r.images[bookID]...)
	sort.Slice(images, func(left, right int) bool {
		if images[left].SortOrder == images[right].SortOrder {
			return images[left].ID < images[right].ID
		}
		return images[left].SortOrder < images[right].SortOrder
	})
	return images, nil
}

func (r *fakeBookRepository) CountImages(_ context.Context, bookID uint64) (int64, error) {
	return int64(len(r.images[bookID])), nil
}

func (r *fakeBookRepository) CreateImage(_ context.Context, image *model.BookImage) error {
	if r.createImageErr != nil {
		return r.createImageErr
	}
	image.ID = r.nextImageID
	r.nextImageID++
	image.CreatedAt = time.Now().UTC()
	r.images[image.BookID] = append(r.images[image.BookID], *image)
	return nil
}

func (r *fakeBookRepository) FindImageByID(_ context.Context, bookID, imageID uint64) (*model.BookImage, error) {
	for _, image := range r.images[bookID] {
		if image.ID == imageID {
			copy := image
			return &copy, nil
		}
	}
	return nil, repository.ErrBookImageNotFound
}

func (r *fakeBookRepository) DeleteImage(_ context.Context, imageID uint64) error {
	for bookID, images := range r.images {
		for index, image := range images {
			if image.ID == imageID {
				r.images[bookID] = append(images[:index], images[index+1:]...)
				return nil
			}
		}
	}
	return repository.ErrBookImageNotFound
}

func (r *fakeBookRepository) ReplaceImageOrder(_ context.Context, bookID uint64, imageIDs []uint64) error {
	byID := make(map[uint64]model.BookImage, len(r.images[bookID]))
	for _, image := range r.images[bookID] {
		byID[image.ID] = image
	}
	reordered := make([]model.BookImage, 0, len(imageIDs))
	for index, imageID := range imageIDs {
		image, ok := byID[imageID]
		if !ok {
			return repository.ErrBookImageNotFound
		}
		image.SortOrder = uint16(index + 1)
		reordered = append(reordered, image)
	}
	r.images[bookID] = reordered
	return nil
}

func (r *fakeBookRepository) addBook(sellerID uint64, status model.BookStatus) *model.Book {
	book := &model.Book{
		SellerID:  sellerID,
		Title:     "Book",
		Price:     "10.00",
		Condition: model.BookConditionGood,
		Status:    status,
	}
	_ = r.Create(context.Background(), book)
	return book
}

func (r *fakeBookRepository) addImage(bookID uint64, sortOrder uint16) model.BookImage {
	image := &model.BookImage{BookID: bookID, ImageURL: "/uploads/books/test.png", SortOrder: sortOrder}
	_ = r.CreateImage(context.Background(), image)
	return *image
}

type fakeImageStorage struct {
	mutex   sync.Mutex
	next    int
	deleted []string
}

func (s *fakeImageStorage) Stage(context.Context, io.Reader) (*storage.StagedImage, error) {
	return &storage.StagedImage{}, nil
}

func (s *fakeImageStorage) Commit(context.Context, *storage.StagedImage) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.next++
	return "/uploads/books/generated-" + strconv.Itoa(s.next) + ".png", nil
}

func (*fakeImageStorage) Discard(*storage.StagedImage) error { return nil }

func (s *fakeImageStorage) Delete(imageURL string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.deleted = append(s.deleted, imageURL)
	return nil
}

func testContactURL() *string {
	value := "/uploads/books/wechat.png"
	return &value
}

func (r *fakeBookRepository) UpdateWechatQRCode(_ context.Context, bookID uint64, imageURL string) error {
	if r.updateContactErr != nil {
		return r.updateContactErr
	}
	r.books[bookID].WechatQRCodeURL = &imageURL
	return nil
}
