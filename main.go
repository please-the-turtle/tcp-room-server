package main

import (
	"github.com/please-the-turtle/tcp-room-server/server"
)

func main() {
	config := server.DefaultServerConfig()
	s := server.NewServer(config)
	s.Listen()
}
