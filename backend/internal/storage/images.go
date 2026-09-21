package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

const maxImagePixels = 20_000_000

var (
	ErrImageTooLarge        = errors.New("image is too large")
	ErrUnsupportedImageType = errors.New("unsupported image type")
	ErrInvalidImage         = errors.New("invalid image")
	ErrUnsafeImagePath      = errors.New("unsafe image path")
)

type StagedImage struct {
	temporaryPath string
	extension     string
}

type LocalImages struct {
	rootDirectory string
	tempDirectory string
	bookDirectory string
	publicPrefix  string
	maxBytes      int64
}

func NewLocalImages(cfg config.UploadConfig) (*LocalImages, error) {
	rootDirectory, err := filepath.Abs(cfg.Directory)
	if err != nil {
		return nil, fmt.Errorf("resolve upload directory: %w", err)
	}
	storage := &LocalImages{
		rootDirectory: rootDirectory,
		tempDirectory: filepath.Join(rootDirectory, ".tmp"),
		bookDirectory: filepath.Join(rootDirectory, "books"),
		publicPrefix:  cfg.PublicPrefix,
		maxBytes:      cfg.MaxBytes,
	}
	for _, directory := range []string{storage.rootDirectory, storage.tempDirectory, storage.bookDirectory} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create image directory: %w", err)
		}
	}
	return storage, nil
}

func (s *LocalImages) Stage(ctx context.Context, source io.Reader) (*StagedImage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	temporaryFile, err := os.CreateTemp(s.tempDirectory, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary image: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	keepFile := false
	defer func() {
		_ = temporaryFile.Close()
		if !keepFile {
			_ = os.Remove(temporaryPath)
		}
	}()

	written, err := io.Copy(temporaryFile, io.LimitReader(source, s.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("write temporary image: %w", err)
	}
	if written == 0 {
		return nil, ErrInvalidImage
	}
	if written > s.maxBytes {
		return nil, ErrImageTooLarge
	}
	if err := temporaryFile.Sync(); err != nil {
		return nil, fmt.Errorf("sync temporary image: %w", err)
	}

	extension, err := validateImageFile(temporaryFile)
	if err != nil {
		return nil, err
	}
	if err := temporaryFile.Close(); err != nil {
		return nil, fmt.Errorf("close temporary image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keepFile = true
	return &StagedImage{temporaryPath: temporaryPath, extension: extension}, nil
}

func (s *LocalImages) Commit(ctx context.Context, staged *StagedImage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if staged == nil || filepath.Dir(staged.temporaryPath) != s.tempDirectory {
		return "", ErrUnsafeImagePath
	}
	identifier, err := randomImageName()
	if err != nil {
		return "", fmt.Errorf("generate image name: %w", err)
	}
	filename := identifier + staged.extension
	finalPath := filepath.Join(s.bookDirectory, filename)
	if err := os.Rename(staged.temporaryPath, finalPath); err != nil {
		return "", fmt.Errorf("commit image: %w", err)
	}
	if err := os.Chmod(finalPath, 0o644); err != nil {
		_ = os.Remove(finalPath)
		return "", fmt.Errorf("set image permissions: %w", err)
	}
	staged.temporaryPath = ""
	return s.publicPrefix + "books/" + filename, nil
}

func (s *LocalImages) Discard(staged *StagedImage) error {
	if staged == nil || staged.temporaryPath == "" {
		return nil
	}
	if filepath.Dir(staged.temporaryPath) != s.tempDirectory {
		return ErrUnsafeImagePath
	}
	err := os.Remove(staged.temporaryPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *LocalImages) Delete(imageURL string) error {
	expectedPrefix := s.publicPrefix + "books/"
	if !strings.HasPrefix(imageURL, expectedPrefix) {
		return ErrUnsafeImagePath
	}
	filename := strings.TrimPrefix(imageURL, expectedPrefix)
	if filename == "" || filepath.Base(filename) != filename {
		return ErrUnsafeImagePath
	}
	err := os.Remove(filepath.Join(s.bookDirectory, filename))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func validateImageFile(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek image: %w", err)
	}
	header := make([]byte, 512)
	read, err := file.Read(header)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read image header: %w", err)
	}
	mimeType := http.DetectContentType(header[:read])
	extension := ""
	switch mimeType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	default:
		return "", ErrUnsupportedImageType
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek image: %w", err)
	}
	configuration, format, err := image.DecodeConfig(file)
	if err != nil || (mimeType == "image/jpeg" && format != "jpeg") ||
		(mimeType == "image/png" && format != "png") {
		return "", ErrInvalidImage
	}
	if configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width)*int64(configuration.Height) > maxImagePixels {
		return "", ErrInvalidImage
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek image: %w", err)
	}
	if _, decodedFormat, err := image.Decode(file); err != nil || decodedFormat != format {
		return "", ErrInvalidImage
	}
	return extension, nil
}

func randomImageName() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
