package main

import (
	"fmt"
	"net"
	"time"

	"github.com/DaniilSokolyuk/gop2pt"
)

func main() {
	p2pt := gop2pt.New("p2chatgeneral", []string{"wss://tracker.btorrent.xyz/announce"})
	listener, _ := p2pt.Start()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error", err)
			continue
		}

		onConn(conn)
	}

	time.Sleep(time.Second * 500)
}

func onConn(conn net.Conn) {
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
