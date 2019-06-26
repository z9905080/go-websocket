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

	r.GET("/channel/:name", func(c *gin.Context) {
		http.ServeFile(c.Writer, c.Request, "chan.html")
	})

	r.GET("/channel/:name/ws", func(c *gin.Context) {
		m.HandleRequest(c.Writer, c.Request)
	})

	m.HandleMessage(func(s *gowebsocket.Session, msg []byte) {
		m.BroadcastFilter(msg, func(q *gowebsocket.Session) bool {
			return q.Request.URL.Path == s.Request.URL.Path
		})
	})

	r.Run(":5000")
}
