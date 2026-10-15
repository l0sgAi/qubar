// Package application 提供 storage 领域的应用服务层。
//
// 职责：用例编排（文件校验、路径生成、调用 ObjectStorage），不关心 HTTP 层细节。
//
// 原来散在 controller/upload.go 里的 5 个方法（UploadImage / UploadPostImages /
// UploadVideo / DeleteFile / GetPresignedURL）的业务逻辑集中在本层。
package application

import (
	"context"
	"fmt"
	"mime/multipart"

	"github.com/google/uuid"

	"interestBar/pkg/domains/storage/domain"
	infra "interestBar/pkg/domains/storage/infrastructure"
	"interestBar/pkg/logger"

	"go.uber.org/zap"
)

// FileVO 单文件上传结果。
type FileVO struct {
	URL string `json:"url"`
}

// UploadedImageVO 多文件上传的单条结果。
type UploadedImageVO struct {
	URL      string `json:"url"`
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

// UploadedImagesVO 多文件上传的聚合结果。
type UploadedImagesVO struct {
	Uploaded int               `json:"uploaded"`
	Total    int               `json:"total"`
	Images   []UploadedImageVO `json:"images"`
}

// FileWithKeyVO 含 key 的文件响应（用于 video 上传）。
type FileWithKeyVO struct {
	URL      string `json:"url"`
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

// PresignedURLVO 预签名 URL 响应。
type PresignedURLVO struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

// FileKind 上传文件类型，决定校验规则与 S3 路径。
type FileKind int

const (
	KindImage     FileKind = iota // 单图（用户头像/通用图片）
	KindPostImage                 // 帖子多图
	KindVideo                     // 视频
)

// 允许的扩展名与最大体积（与旧 upload controller 完全一致）。
var (
	imageExts     = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg"}
	imageMaxSize  = int64(10 * 1024 * 1024) // 10MB
	postImageExts = []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}
	postImageSize = int64(10 * 1024 * 1024) // 10MB
	videoExts     = []string{".mp4", ".avi", ".mov", ".mkv", ".webm"}
	videoMaxSize  = int64(500 * 1024 * 1024) // 500MB
)

// StorageService 是 storage 领域的应用服务接口。
type StorageService interface {
	// UploadImage 上传单张图片（用户路径 uploads/<userID>/）。
	UploadImage(ctx context.Context, userID string, file *multipart.FileHeader) (FileVO, error)
	// UploadPostImages 批量上传帖子图片（路径 posts/<userID>/），最多 9 张。
	UploadPostImages(ctx context.Context, userID string, files []*multipart.FileHeader) (UploadedImagesVO, error)
	// UploadVideo 上传视频（路径 videos/）。
	UploadVideo(ctx context.Context, file *multipart.FileHeader) (FileWithKeyVO, error)
	// DeleteFile 按 key 删除文件。
	DeleteFile(ctx context.Context, key string) error
	// PresignedURL 生成 key 的预签名 URL。
	PresignedURL(ctx context.Context, key string) (PresignedURLVO, error)
}

type storageServiceImpl struct {
	storage domain.ObjectStorage
}

// NewStorageService 构造一个 StorageService。
func NewStorageService(storage domain.ObjectStorage) StorageService {
	return &storageServiceImpl{storage: storage}
}

// UploadImage 上传单张图片到 S3。
func (s *storageServiceImpl) UploadImage(ctx context.Context, userID string, file *multipart.FileHeader) (FileVO, error) {
	if err := validateUserID(userID); err != nil {
		return FileVO{}, err
	}
	if err := infra.ValidateFile(file, imageExts, imageMaxSize); err != nil {
		logger.Log.Error("file validation failed", zap.Error(err))
		return FileVO{}, fmt.Errorf("file validation failed: %w", err)
	}

	key := infra.GenerateKey(fmt.Sprintf("uploads/%s", userID), file.Filename)

	if !s.storage.Available() {
		logger.Log.Error("S3 client is not initialized")
		return FileVO{}, fmt.Errorf("s3 service unavailable")
	}

	url, err := s.storage.Upload(ctx, key, file, "public-read")
	if err != nil {
		logger.Log.Error("failed to upload file to S3", zap.Error(err))
		return FileVO{}, fmt.Errorf("failed to upload file")
	}

	logger.Log.Info("File uploaded successfully",
		zap.String("user_id", userID),
		zap.String("url", url),
	)

	return FileVO{URL: url}, nil
}

// UploadPostImages 批量上传帖子图片（最多 9 张）。
func (s *storageServiceImpl) UploadPostImages(ctx context.Context, userID string, files []*multipart.FileHeader) (UploadedImagesVO, error) {
	if err := validateUserID(userID); err != nil {
		return UploadedImagesVO{}, err
	}
	if len(files) == 0 {
		return UploadedImagesVO{}, fmt.Errorf("no files uploaded")
	}
	if len(files) > 9 {
		return UploadedImagesVO{}, fmt.Errorf("maximum 9 images allowed")
	}

	if !s.storage.Available() {
		logger.Log.Error("S3 client is not initialized")
		return UploadedImagesVO{}, fmt.Errorf("s3 service unavailable")
	}

	uploaded := make([]UploadedImageVO, 0, len(files))
	for _, file := range files {
		if err := infra.ValidateFile(file, postImageExts, postImageSize); err != nil {
			logger.Log.Error("file validation failed",
				zap.String("filename", file.Filename),
				zap.Error(err),
			)
			continue
		}

		key := infra.GenerateKey(fmt.Sprintf("posts/%s", userID), file.Filename)
		url, err := s.storage.Upload(ctx, key, file, "public-read")
		if err != nil {
			logger.Log.Error("failed to upload file to S3",
				zap.String("filename", file.Filename),
				zap.Error(err),
			)
			continue
		}

		uploaded = append(uploaded, UploadedImageVO{
			URL:      url,
			Key:      key,
			Filename: file.Filename,
			Size:     file.Size,
		})
	}

	if len(uploaded) == 0 {
		return UploadedImagesVO{}, fmt.Errorf("failed to upload any files")
	}

	return UploadedImagesVO{
		Uploaded: len(uploaded),
		Total:    len(files),
		Images:   uploaded,
	}, nil
}

// UploadVideo 上传视频。
func (s *storageServiceImpl) UploadVideo(ctx context.Context, file *multipart.FileHeader) (FileWithKeyVO, error) {
	if err := infra.ValidateFile(file, videoExts, videoMaxSize); err != nil {
		logger.Log.Error("file validation failed", zap.Error(err))
		return FileWithKeyVO{}, fmt.Errorf("file validation failed: %w", err)
	}

	key := infra.GenerateKey("videos", file.Filename)

	if !s.storage.Available() {
		logger.Log.Error("S3 client is not initialized")
		return FileWithKeyVO{}, fmt.Errorf("s3 service unavailable")
	}

	url, err := s.storage.Upload(ctx, key, file, "public-read")
	if err != nil {
		logger.Log.Error("failed to upload file to S3", zap.Error(err))
		return FileWithKeyVO{}, fmt.Errorf("failed to upload file")
	}

	return FileWithKeyVO{
		URL:      url,
		Key:      key,
		Filename: file.Filename,
		Size:     file.Size,
	}, nil
}

// DeleteFile 按 key 删除文件。
func (s *storageServiceImpl) DeleteFile(ctx context.Context, key string) error {
	if !s.storage.Available() {
		logger.Log.Error("S3 client is not initialized")
		return fmt.Errorf("s3 service unavailable")
	}
	if err := s.storage.Delete(ctx, key); err != nil {
		logger.Log.Error("failed to delete file from S3",
			zap.String("key", key),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete file")
	}
	return nil
}

// PresignedURL 生成预签名 URL。
func (s *storageServiceImpl) PresignedURL(ctx context.Context, key string) (PresignedURLVO, error) {
	if !s.storage.Available() {
		logger.Log.Error("S3 client is not initialized")
		return PresignedURLVO{}, fmt.Errorf("s3 service unavailable")
	}
	url, err := s.storage.PresignedURL(ctx, key)
	if err != nil {
		logger.Log.Error("failed to generate presigned URL",
			zap.String("key", key),
			zap.Error(err),
		)
		return PresignedURLVO{}, fmt.Errorf("failed to generate presigned URL")
	}
	return PresignedURLVO{URL: url, Key: key}, nil
}

// validateUserID 校验 userID 为合法 UUID，防止路径遍历或非预期字符注入 S3 key。
//
// 上层 handler 已从登录态 token 解析得到 userID，正常应为 UUIDv7 字符串；
// 这里再加一道防御，避免任何调用方误传含 "../"、"/" 等字符的值。
func validateUserID(userID string) error {
	if userID == "" {
		return fmt.Errorf("userID is required")
	}
	if _, err := uuid.Parse(userID); err != nil {
		return fmt.Errorf("invalid userID: %w", err)
	}
	return nil
}
