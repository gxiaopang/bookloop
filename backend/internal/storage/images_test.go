package storage

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

func TestLocalImagesStageCommitAndDeletePNG(t *testing.T) {
	directory := t.TempDir()
	images, err := NewLocalImages(config.UploadConfig{
		Directory:    directory,
		PublicPrefix: "/uploads/",
		MaxBytes:     1024 * 1024,
	})
	if err != nil {
		t.Fatalf("NewLocalImages() error = %v", err)
	}

	staged, err := images.Stage(context.Background(), validPNG(t))
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	imageURL, err := images.Commit(context.Background(), staged)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if !strings.HasPrefix(imageURL, "/uploads/books/") || !strings.HasSuffix(imageURL, ".png") {
		t.Fatalf("image URL = %q, want generated PNG URL", imageURL)
	}
	filename := strings.TrimPrefix(imageURL, "/uploads/books/")
	if _, err := os.Stat(filepath.Join(directory, "books", filename)); err != nil {
		t.Fatalf("committed image does not exist: %v", err)
	}
	if err := images.Delete(imageURL); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "books", filename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted image stat error = %v, want os.ErrNotExist", err)
	}
}

func TestLocalImagesRejectsInvalidUnsupportedAndOversizedFiles(t *testing.T) {
	images, err := NewLocalImages(config.UploadConfig{
		Directory:    t.TempDir(),
		PublicPrefix: "/uploads/",
		MaxBytes:     128,
	})
	if err != nil {
		t.Fatalf("NewLocalImages() error = %v", err)
	}

	if _, err := images.Stage(context.Background(), strings.NewReader("not an image")); !errors.Is(err, ErrUnsupportedImageType) {
		t.Fatalf("Stage(text) error = %v, want ErrUnsupportedImageType", err)
	}
	truncatedPNG := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 20)...)
	if _, err := images.Stage(context.Background(), bytes.NewReader(truncatedPNG)); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("Stage(truncated PNG) error = %v, want ErrInvalidImage", err)
	}
	if _, err := images.Stage(context.Background(), bytes.NewReader(make([]byte, 129))); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("Stage(oversized) error = %v, want ErrImageTooLarge", err)
	}
}

func TestLocalImagesRejectsUnsafeDeletePath(t *testing.T) {
	images, err := NewLocalImages(config.UploadConfig{
		Directory:    t.TempDir(),
		PublicPrefix: "/uploads/",
		MaxBytes:     1024,
	})
	if err != nil {
		t.Fatalf("NewLocalImages() error = %v", err)
	}

	for _, imageURL := range []string{
		"/other/books/file.png",
		"/uploads/books/../secret",
		"/uploads/books/nested/file.png",
	} {
		if err := images.Delete(imageURL); !errors.Is(err, ErrUnsafeImagePath) {
			t.Fatalf("Delete(%q) error = %v, want ErrUnsafeImagePath", imageURL, err)
		}
	}
}

func validPNG(t *testing.T) *bytes.Reader {
	t.Helper()
	var buffer bytes.Buffer
	file := image.NewRGBA(image.Rect(0, 0, 2, 2))
	file.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buffer, file); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return bytes.NewReader(buffer.Bytes())
}
