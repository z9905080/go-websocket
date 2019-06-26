package main

import (
	gowebsocket "go-websocket"
	"net/http"

	"github.com/labstack/echo"
	"github.com/labstack/echo/engine/standard"
	"github.com/labstack/echo/middleware"
)

func main() {
	e := echo.New()
	m := gowebsocket.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.GET("/", func(c echo.Context) error {
		http.ServeFile(c.Response().(*standard.Response).ResponseWriter, c.Request().(*standard.Request).Request, "index.html")
		return nil
	})

	e.GET("/ws", func(c echo.Context) error {
		m.HandleRequest(c.Response().(*standard.Response).ResponseWriter, c.Request().(*standard.Request).Request)
		return nil
	})

	m.HandleMessage(func(s *gowebsocket.Session, msg []byte) {
		m.Broadcast(msg)
	})

	e.Run(standard.New(":5000"))
}
