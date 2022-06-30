package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/DaniilSokolyuk/gop2pt"
)

var peers sync.Map

func main() {
	p2pt := gop2pt.New("p2chatgeneral", []string{"wss://tracker.btorrent.xyz/announce"})
	listener, _ := p2pt.Start()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error", err)
			continue
		}

		go onConn(conn)
	}

	time.Sleep(time.Second * 500)
}

func onConn(conn net.Conn) {
	if _, loaded := peers.LoadOrStore(conn.RemoteAddr().String(), conn); loaded {
		conn.Close()
		return
	}

	fmt.Println("onConn", conn.RemoteAddr())
	for {
		bytes := make([]byte, 16000)
		i, _ := conn.Read(bytes)
		if i == 0 {
			fmt.Println("disconected")
			conn.Close()
			break
		}

		fmt.Println("MESSAGE", i, string(bytes[:i]), conn.RemoteAddr())
	}
}
