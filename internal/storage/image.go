package storage

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

const (
	ThumbMaxWidth  = 400
	PreviewMaxWidth = 100
	ThumbQuality   = 85
	PreviewQuality = 40
	FullQuality    = 92
)

type ImageStorage struct {
	BaseDir string
}

func NewImageStorage(baseDir string) *ImageStorage {
	return &ImageStorage{BaseDir: baseDir}
}

// SaveWithVariants saves the uploaded image and auto-generates thumb + preview.
// Returns the shared filename used across all three directories.
func (s *ImageStorage) SaveWithVariants(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read upload: %w", err)
	}

	// Decode image (supports JPEG, PNG, GIF, WebP via registered decoders)
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	filename := uuid.New().String() + ".webp"

	// Ensure directories exist
	for _, dir := range []string{"full", "thumb", "preview"} {
		os.MkdirAll(filepath.Join(s.BaseDir, dir), 0755)
	}

	// 1. Save full - re-encode as high-quality WebP
	if err := saveWebP(filepath.Join(s.BaseDir, "full", filename), img, FullQuality); err != nil {
		return "", fmt.Errorf("failed to save full: %w", err)
	}

	// 2. Generate & save thumb (max 400px width)
	thumb := imaging.Resize(img, ThumbMaxWidth, 0, imaging.Lanczos)
	if err := saveWebP(filepath.Join(s.BaseDir, "thumb", filename), thumb, ThumbQuality); err != nil {
		s.DeleteAll(filename)
		return "", fmt.Errorf("failed to save thumb: %w", err)
	}

	// 3. Generate & save preview (max 100px width, low quality)
	preview := imaging.Resize(img, PreviewMaxWidth, 0, imaging.Lanczos)
	if err := saveWebP(filepath.Join(s.BaseDir, "preview", filename), preview, PreviewQuality); err != nil {
		s.DeleteAll(filename)
		return "", fmt.Errorf("failed to save preview: %w", err)
	}

	return filename, nil
}

// DeleteAll removes all three variants of an image.
func (s *ImageStorage) DeleteAll(filename string) {
	for _, dir := range []string{"full", "thumb", "preview"} {
		os.Remove(filepath.Join(s.BaseDir, dir, filename))
	}
}

func (s *ImageStorage) GetPath(imageType, filename string) string {
	return filepath.Join(s.BaseDir, imageType, filename)
}

// ServeImage streams an image file to the writer (plaintext).
func (s *ImageStorage) ServeImage(w io.Writer, imageType, filename string) error {
	path := s.GetPath(imageType, filename)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}

// ReadFile reads the full contents of an image file.
func (s *ImageStorage) ReadFile(imageType, filename string) ([]byte, error) {
	path := s.GetPath(imageType, filename)
	return os.ReadFile(path)
}

func saveWebP(path string, img image.Image, quality int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return webp.Encode(f, img, &webp.Options{Lossless: false, Quality: float32(quality)})
}
