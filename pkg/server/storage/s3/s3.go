package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"go.uber.org/zap"
)

// Client S3 客户端封装
type Client struct {
	client           *s3.Client
	bucket           string
	presignExpire    time.Duration
	logger           *zap.Logger
	cloudfrontDomain string // CloudFront 分发域名
}

// NewClient 创建 S3 客户端
func NewClient(accessKeyID, secretAccessKey, region, bucket, endpoint string, presignExpire int, cloudfrontDomain string, logger *zap.Logger) (*Client, error) {
	// 配置 AWS 凭证
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// 创建 S3 客户端
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		// 如果提供了自定义端点（如 MinIO），则使用自定义端点
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			// 对于自定义 S3 兼容服务，需要启用路径风格寻址
			o.UsePathStyle = true
		}
	})

	// 设置预签名 URL 过期时间，默认 1 小时
	expire := time.Duration(presignExpire) * time.Second
	if expire == 0 {
		expire = time.Hour
	}

	return &Client{
		client:           client,
		bucket:           bucket,
		presignExpire:    expire,
		logger:           logger,
		cloudfrontDomain: cloudfrontDomain,
	}, nil
}

// UploadFile 上传文件到 S3
func (c *Client) UploadFile(ctx context.Context, key string, file *multipart.FileHeader, acl string) (string, error) {
	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer func() { _ = src.Close() }() // 只读上传文件，关闭失败不影响结果

	// 读取文件内容到内存
	buffer := make([]byte, file.Size)
	_, err = src.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file content: %w", err)
	}

	// 上传到 S3（不使用 ACL，使用 bucket 策略控制访问）
	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(buffer),
		ContentType: aws.String(file.Header.Get("Content-Type")),
	})
	if err != nil {
		c.logger.Error("failed to upload file to S3",
			zap.String("key", key),
			zap.String("bucket", c.bucket),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to upload file to S3: %w", err)
	}

	// 返回文件的完整 URL
	// 如果配置了 CloudFront 域名，则使用 CloudFront URL，否则使用 S3 原始 URL
	var fileURL string
	if c.cloudfrontDomain != "" {
		fileURL = fmt.Sprintf("https://%s/%s", c.cloudfrontDomain, key)
	} else {
		fileURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", c.bucket, c.client.Options().Region, key)
	}
	c.logger.Info("file uploaded successfully",
		zap.String("key", key),
		zap.String("url", fileURL),
	)

	return fileURL, nil
}

// UploadFileFromBytes 从字节数组上传文件到 S3
func (c *Client) UploadFileFromBytes(ctx context.Context, key string, data []byte, contentType string, acl string) (string, error) {
	// 上传到 S3（不使用 ACL，使用 bucket 策略控制访问）
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		c.logger.Error("failed to upload file to S3",
			zap.String("key", key),
			zap.String("bucket", c.bucket),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to upload file to S3: %w", err)
	}

	// 返回文件的完整 URL
	// 如果配置了 CloudFront 域名，则使用 CloudFront URL，否则使用 S3 原始 URL
	var fileURL string
	if c.cloudfrontDomain != "" {
		fileURL = fmt.Sprintf("https://%s/%s", c.cloudfrontDomain, key)
	} else {
		fileURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", c.bucket, c.client.Options().Region, key)
	}
	c.logger.Info("file uploaded successfully",
		zap.String("key", key),
		zap.String("url", fileURL),
	)

	return fileURL, nil
}

// UploadFileFromReader 从 io.Reader 上传文件到 S3
func (c *Client) UploadFileFromReader(ctx context.Context, key string, reader io.Reader, contentType, filename string, acl string) (string, error) {
	// 上传到 S3（不使用 ACL，使用 bucket 策略控制访问）
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        reader,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		c.logger.Error("failed to upload file to S3",
			zap.String("key", key),
			zap.String("bucket", c.bucket),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to upload file to S3: %w", err)
	}

	// 返回文件的完整 URL
	// 如果配置了 CloudFront 域名，则使用 CloudFront URL，否则使用 S3 原始 URL
	var fileURL string
	if c.cloudfrontDomain != "" {
		fileURL = fmt.Sprintf("https://%s/%s", c.cloudfrontDomain, key)
	} else {
		fileURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", c.bucket, c.client.Options().Region, key)
	}
	c.logger.Info("file uploaded successfully",
		zap.String("key", key),
		zap.String("url", fileURL),
	)

	return fileURL, nil
}

// DownloadFile 从 S3 下载文件
func (c *Client) DownloadFile(ctx context.Context, key string) ([]byte, string, error) {
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		c.logger.Error("failed to download file from S3",
			zap.String("key", key),
			zap.String("bucket", c.bucket),
			zap.Error(err),
		)
		return nil, "", fmt.Errorf("failed to download file from S3: %w", err)
	}
	defer func() { _ = result.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	// 读取文件内容
	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file content: %w", err)
	}

	// 获取内容类型
	contentType := aws.ToString(result.ContentType)

	c.logger.Info("file downloaded successfully",
		zap.String("key", key),
		zap.Int("size", len(data)),
	)

	return data, contentType, nil
}

// DeleteFile 从 S3 删除文件
func (c *Client) DeleteFile(ctx context.Context, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		c.logger.Error("failed to delete file from S3",
			zap.String("key", key),
			zap.String("bucket", c.bucket),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete file from S3: %w", err)
	}

	c.logger.Info("file deleted successfully",
		zap.String("key", key),
	)

	return nil
}

// DeleteMultipleFiles 批量删除文件
func (c *Client) DeleteMultipleFiles(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	// 构建删除对象列表
	var objects []types.ObjectIdentifier
	for _, key := range keys {
		objects = append(objects, types.ObjectIdentifier{
			Key: aws.String(key),
		})
	}

	_, err := c.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(c.bucket),
		Delete: &types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true), // 安静模式，不返回删除结果
		},
	})
	if err != nil {
		c.logger.Error("failed to delete multiple files from S3",
			zap.String("bucket", c.bucket),
			zap.Int("count", len(keys)),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete multiple files from S3: %w", err)
	}

	c.logger.Info("multiple files deleted successfully",
		zap.String("bucket", c.bucket),
		zap.Int("count", len(keys)),
	)

	return nil
}

// GetPresignedURL 生成预签名 URL（用于临时访问私有文件）
func (c *Client) GetPresignedURL(ctx context.Context, key string) (string, error) {
	presignClient := s3.NewPresignClient(c.client)

	presignedResult, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(c.presignExpire))
	if err != nil {
		c.logger.Error("failed to generate presigned URL",
			zap.String("key", key),
			zap.String("bucket", c.bucket),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	c.logger.Info("presigned URL generated",
		zap.String("key", key),
		zap.Duration("expires_in", c.presignExpire),
	)

	return presignedResult.URL, nil
}

// CheckFileExists 检查文件是否存在
func (c *Client) CheckFileExists(ctx context.Context, key string) (bool, error) {
	_, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		// 检查错误类型，如果是 NotFound 则返回 false
		var notFound *types.NotFound
		if strings.Contains(err.Error(), "NotFound") || err.Error() == notFound.Error() {
			return false, nil
		}
		return false, fmt.Errorf("failed to check file existence: %w", err)
	}

	return true, nil
}

// ListFiles 列出指定前缀的所有文件
func (c *Client) ListFiles(ctx context.Context, prefix string, maxKeys int32) ([]string, error) {
	result, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(maxKeys),
	})
	if err != nil {
		c.logger.Error("failed to list files from S3",
			zap.String("prefix", prefix),
			zap.String("bucket", c.bucket),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to list files from S3: %w", err)
	}

	var keys []string
	for _, obj := range result.Contents {
		keys = append(keys, aws.ToString(obj.Key))
	}

	return keys, nil
}

// GenerateKey 生成 S3 对象键名
// 支持自定义路径和文件名
func GenerateKey(basePath string, filename string) string {
	// 清理文件名，移除特殊字符
	filename = filepath.Base(filename)
	filename = strings.ReplaceAll(filename, " ", "_")
	filename = strings.ReplaceAll(filename, "..", "")

	// 生成带时间戳的路径
	now := time.Now()
	datePath := now.Format("2006/01/02")

	// 组合完整路径
	var fullPath string
	if basePath != "" {
		fullPath = fmt.Sprintf("%s/%s/%s", strings.TrimPrefix(basePath, "/"), datePath, filename)
	} else {
		fullPath = fmt.Sprintf("%s/%s", datePath, filename)
	}

	// 确保路径使用正斜杠
	fullPath = filepath.ToSlash(fullPath)

	return fullPath
}

// GenerateKeyWithUUID 生成带 UUID 的 S3 对象键名
// 确保文件名唯一性,按日期组织目录结构
// 目录结构: {basePath}/{year}/{month}/{day}/{filename}_{timestamp}_{random}.ext
func GenerateKeyWithUUID(basePath string, filename string) string {
	// 获取文件扩展名
	ext := filepath.Ext(filename)

	// 生成唯一标识(时间戳 + 随机字符串)
	timestamp := time.Now().Unix()
	randomStr := randomString(8)

	// 清理文件名
	baseName := strings.TrimSuffix(filepath.Base(filename), ext)
	baseName = strings.ReplaceAll(baseName, " ", "_")
	baseName = strings.ReplaceAll(baseName, "..", "")

	// 生成按日期分层的路径结构
	now := time.Now()
	datePath := fmt.Sprintf("%d/%02d/%02d", now.Year(), now.Month(), now.Day())

	// 组合完整路径: {basePath}/{year}/{month}/{day}/{filename}_{timestamp}_{random}.ext
	var fullPath string
	if basePath != "" {
		fullPath = fmt.Sprintf("%s/%s/%s_%d_%s%s",
			strings.TrimPrefix(basePath, "/"),
			datePath,
			baseName,
			timestamp,
			randomStr,
			ext)
	} else {
		fullPath = fmt.Sprintf("%s/%s_%d_%s%s", datePath, baseName, timestamp, randomStr, ext)
	}

	// 确保路径使用正斜杠
	fullPath = filepath.ToSlash(fullPath)

	return fullPath
}

// GetContentType 根据文件名获取 Content-Type
func GetContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".mp4":
		return "video/mp4"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".md":
		return "text/markdown"
	default:
		return "application/octet-stream"
	}
}

// IsImageFile 判断是否为图片文件
func IsImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	imageExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".svg":  true,
		".webp": true,
		".bmp":  true,
		".ico":  true,
	}
	return imageExts[ext]
}

// IsVideoFile 判断是否为视频文件
func IsVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	videoExts := map[string]bool{
		".mp4":  true,
		".avi":  true,
		".mov":  true,
		".wmv":  true,
		".flv":  true,
		".mkv":  true,
		".webm": true,
	}
	return videoExts[ext]
}

// ValidateFile 验证文件类型和大小
func ValidateFile(file *multipart.FileHeader, allowedExts []string, maxSize int64) error {
	// 检查文件大小
	if file.Size > maxSize {
		return fmt.Errorf("file size %d exceeds maximum allowed size %d", file.Size, maxSize)
	}

	// 检查文件扩展名
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if len(allowedExts) > 0 {
		allowed := false
		for _, allowedExt := range allowedExts {
			if ext == allowedExt {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("file extension %s is not allowed", ext)
		}
	}

	return nil
}

// randomString 生成随机字符串
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

// GetPublicURL 获取文件的公共 URL（适用于公开访问的文件）
func (c *Client) GetPublicURL(key string) string {
	// 如果配置了 CloudFront 域名，则使用 CloudFront URL，否则使用 S3 原始 URL
	if c.cloudfrontDomain != "" {
		return fmt.Sprintf("https://%s/%s", c.cloudfrontDomain, key)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", c.bucket, c.client.Options().Region, key)
}

// CopyFile 复制文件（在同一存储桶内）
func (c *Client) CopyFile(ctx context.Context, sourceKey, destKey string) error {
	_, err := c.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(c.bucket),
		CopySource: aws.String(c.bucket + "/" + sourceKey),
		Key:        aws.String(destKey),
	})
	if err != nil {
		c.logger.Error("failed to copy file in S3",
			zap.String("source", sourceKey),
			zap.String("destination", destKey),
			zap.Error(err),
		)
		return fmt.Errorf("failed to copy file in S3: %w", err)
	}

	c.logger.Info("file copied successfully",
		zap.String("source", sourceKey),
		zap.String("destination", destKey),
	)

	return nil
}
