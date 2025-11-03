package httpx

import (
  "net/http"
  "os"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/golang-jwt/jwt/v5"
)

func AuthRequired() gin.HandlerFunc {
  secret := os.Getenv("JWT_SECRET")
  return func(c *gin.Context) {
    h := c.GetHeader("Authorization")
    if !strings.HasPrefix(h, "Bearer ") {
      c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":"missing bearer token"})
      return
    }
    tokenStr := strings.TrimPrefix(h, "Bearer ")
    _, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) { return []byte(secret), nil })
    if err != nil {
      c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":"invalid token"})
      return
    }
    c.Next()
  }
}
