package router

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/handler"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/model"
)

func New(
	logger *slog.Logger,
	health *handler.Health,
	auth *handler.Auth,
	books *handler.Book,
	favorites *handler.Favorite,
	orders *handler.Order,
	notifications *handler.Notification,
	messagingStatus *handler.MessagingStatus,
	authentication *middleware.Authentication,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.Use(
		middleware.RequestID(),
		middleware.AccessLog(logger),
		middleware.Recovery(logger),
	)

	engine.GET("/healthz", health.Liveness)
	engine.GET("/readyz", health.Readiness)

	api := engine.Group("/api/v1")
	api.POST("/auth/register", auth.Register)
	api.POST("/auth/login", auth.Login)
	api.GET("/books", books.List)
	api.GET("/books/:book_id", books.Get)

	authenticated := api.Group("")
	authenticated.Use(authentication.RequireUser())
	authenticated.GET("/users/me", auth.CurrentUser)
	authenticated.GET("/users/me/books", books.ListMine)
	authenticated.GET("/users/me/books/:book_id", books.GetMine)
	authenticated.POST("/books", books.Create)
	authenticated.PATCH("/books/:book_id", books.Update)
	authenticated.POST("/books/:book_id/images", books.UploadImage)
	authenticated.PUT("/books/:book_id/wechat-qr-code", books.UploadWechatQRCode)
	authenticated.DELETE("/books/:book_id/images/:image_id", books.DeleteImage)
	authenticated.PUT("/books/:book_id/images/order", books.ReorderImages)
	authenticated.POST("/books/:book_id/off-shelf", books.TakeOffShelf)
	authenticated.POST("/books/:book_id/on-shelf", books.PutOnShelf)
	authenticated.GET("/favorites", favorites.List)
	authenticated.PUT("/favorites/:book_id", favorites.Add)
	authenticated.DELETE("/favorites/:book_id", favorites.Remove)
	authenticated.POST("/orders", orders.Create)
	authenticated.GET("/orders", orders.List)
	authenticated.GET("/orders/:order_no", orders.Get)
	authenticated.POST("/orders/:order_no/cancel", orders.Cancel)
	authenticated.POST("/orders/:order_no/complete", orders.Complete)
	authenticated.GET("/notifications", notifications.List)
	authenticated.PUT("/notifications/read-all", notifications.MarkAllRead)
	authenticated.PUT("/notifications/:notification_id/read", notifications.MarkRead)

	admin := api.Group("/admin")
	admin.Use(authentication.RequireUser(), middleware.RequireRole(model.UserRoleAdmin))
	admin.POST("/books/:book_id/block", books.Block)
	admin.POST("/books/:book_id/unblock", books.Unblock)
	admin.GET("/orders", orders.ListAll)
	admin.GET("/messaging/status", messagingStatus.Get)

	engine.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, dto.ErrorResponse(
			"NOT_FOUND",
			"route not found",
			middleware.RequestIDFromContext(c),
		))
	})
	engine.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, dto.ErrorResponse(
			"METHOD_NOT_ALLOWED",
			"method not allowed",
			middleware.RequestIDFromContext(c),
		))
	})

	return engine
}
