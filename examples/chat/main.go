package main

import (
	gowebsocket "go-websocket"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()
	m := gowebsocket.New()

	r.GET("/", func(c *gin.Context) {
		http.ServeFile(c.Writer, c.Request, "index.html")
	})

	r.GET("/ws", func(c *gin.Context) {
		m.HandleRequest(c.Writer, c.Request)
	})

	m.HandleMessage(func(s *gowebsocket.Session, msg []byte) {
		m.Broadcast(msg)
	})

	r.Run(":5000")
}
