package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/model"
)

type authUserStore interface {
	FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
}

func AuthMiddleware(authService auth.AuthService, userStore authUserStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
				"code":  "AUTH_FAILED",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authorization header format",
				"code":  "AUTH_FAILED",
			})
			return
		}

		tokenString := parts[1]
		claims, err := authService.ValidateToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
				"code":  "AUTH_FAILED",
			})
			return
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
				"code":  "AUTH_FAILED",
			})
			return
		}

		user, err := userStore.FindByID(c.Request.Context(), userID)
		if err != nil || user == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
				"code":  "AUTH_FAILED",
			})
			return
		}

		c.Set("user_id", claims.Subject)
		c.Set("student_id", claims.StudentID)
		c.Set("role", user.Role)
		c.Set("current_user", user)
		c.Next()
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := currentUser(c)
		if !ok || !user.IsAdmin() || !user.IsActive() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "admin access required",
				"code":  "FORBIDDEN",
			})
			return
		}
		c.Next()
	}
}

func RequireActiveUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := currentUser(c)
		if !ok || !user.IsActive() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "account is not active",
				"code":  "FORBIDDEN",
			})
			return
		}
		c.Next()
	}
}

func currentUser(c *gin.Context) (*model.User, bool) {
	value, ok := c.Get("current_user")
	if !ok {
		return nil, false
	}

	user, ok := value.(*model.User)
	return user, ok
}
