package main

import (
	"net/http"
	"strconv"
	"time"

	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
	"github.com/labstack/echo/v4"

	"github.com/labstack/echo/v4/middleware"
	"github.com/wonderivan/logger"
)

type syslogCounts struct {
	ReceivedCount     int64
	ReceivedCached    string
	WriteToKafkaCount int64
}

var socketioServer *socketio.Server

// Easier to get running with CORS. Thanks for help @Vindexus and @erkie
var allowOriginFunc = func(r *http.Request) bool {
	return true
}

func socketiorun() {

	socketioServer = socketio.NewServer(&engineio.Options{
		Transports: []transport.Transport{
			&polling.Transport{
				CheckOrigin: allowOriginFunc,
			},
			&websocket.Transport{
				CheckOrigin: allowOriginFunc,
			},
		}})
	// 连接事件
	socketioServer.OnConnect("/", func(s socketio.Conn) error {
		s.SetContext("")
		s.Join("syslogroom")
		logger.Info("连接：", s.RemoteAddr().String())
		return nil
	})

	socketioServer.OnEvent("/", "getconf", func(s socketio.Conn, msg string) {
		s.Emit("getconf", conf)
	})
	// 出错事件
	socketioServer.OnError("/", func(s socketio.Conn, e error) {
		logger.Error("socket.io Error:", e)
	})
	// 断开连接事件
	socketioServer.OnDisconnect("/", func(s socketio.Conn, reason string) {
		s.Leave("syslogroom")
		logger.Info("断开连接：", s.RemoteAddr().String(), reason)
	})

	go startsocketio()
	defer socketioServer.Close()

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Static("/", "static")
	e.Any("/socket.io/", func(context echo.Context) error {
		socketioServer.ServeHTTP(context.Response(), context.Request())
		return nil
	})

	e.Logger.Fatal(e.Start(conf.HttpAddr))
}

//
func startsocketio() {
	err := socketioServer.Serve()
	if err != nil {
		logger.Error(err)
	}
}

//
func BroadcastMonitor() {
	var t syslogCounts
	// 启动消息广播
	for {
		t.ReceivedCount = Receivedcount
		t.ReceivedCached = intToStr(len(channelRecv)) + "/" + intToStr(cap(channelRecv))
		t.WriteToKafkaCount = WriteToKafkaCount
		if socketioServer != nil {
			socketioServer.BroadcastToRoom("/", "syslogroom", "syslogCounts", t)
		}
		time.Sleep(1 * time.Second)
	}
}

//
func int64ToStr(i int64) string {
	return strconv.FormatInt(int64(i), 10)
}

//
func intToStr(i int) string {
	return strconv.FormatInt(int64(i), 10)
}
