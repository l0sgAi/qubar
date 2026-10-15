package http

import (
	"interestBar/pkg/domains/storage/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 storage 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 controller/upload.go 中已注册 + 未注册方法保持一致）：
//
//	POST   /upload/image         单图上传（需登录）
//	POST   /upload/post-images   帖子多图上传（需登录）
//	POST   /upload/video         视频上传（需登录）
//	DELETE /upload/delete        删除文件（需登录）
//	GET    /upload/presign       预签名 URL（需登录）
//
// 注意：旧 routers.go 只注册了 /upload/image；其余四个方法原本在
// upload.go 里定义但未挂载。迁移时一齐挂出，保持"代码即接口"原则——
// 避免日后排查"为什么 swagger 有但接口 404"。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.StorageService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	up := rg.Group("/upload", authCheck)
	{
		up.POST("/image", h.UploadImage)
		up.POST("/post-images", h.UploadPostImages)
		up.POST("/video", h.UploadVideo)
		up.DELETE("/delete", h.DeleteFile)
		up.GET("/presign", h.PresignedURL)
	}
}
