package main

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"time"
)

type Server struct {
	quiting chan interface{}
	wg      sync.WaitGroup
	ls      net.Listener
}

func NewServer(addr string) (*Server, error) {
	q := make(chan interface{})
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		close(q)
		return nil, err
	}
	return &Server{ls: ls, quiting: q}, nil
}

func (this *Server) Start() {
	this.wg.Add(1)
	go func() {
		defer this.wg.Done()
		for {
			conn, err := this.ls.Accept()
			select {
			case <-this.quiting:
				log.Println("Quit accept loop.")
				if conn != nil {
					conn.Close()
				}
				return
			default:
				//
			}
			if err != nil {
				log.Println("Accept error:", err)
				if conn != nil {
					conn.Close()
				}
			}
			log.Println("Accept conn:", conn)
			if conn != nil {
				this.wg.Add(1)
				go this.handleClient(conn)
			}
		}
	}()
}

func getRealServerAddr(uri uint32) string {
	return "127.0.0.1:3770"
}

func (this *Server) handleClient(client net.Conn) {
	defer this.wg.Done()
	defer client.Close()

	connMap := make(map[string]net.Conn)
	buf := make([]byte, 1024)

	defer func() {
		for _, conn := range connMap {
			conn.Close()
		}
	}()

	qch := make(chan interface{})
	defer close(qch)
	go func() {
		for {
			select {
			case <-qch:
				return
			case <-this.quiting:
				client.Close()
				for _, conn := range connMap {
					conn.Close()
				}
				return
			}
		}
	}()

	for {
		remote, err := this.copyFromClientToRemote(connMap, buf, client)
		if err != nil {
			return
		}
		err = this.copyFromRemoteToClient(buf, remote, client)
		if err != nil {
			return
		}
	}
}

func (*Server) copyFromClientToRemote(connMap map[string]net.Conn, buf []byte, client net.Conn) (net.Conn, error) {

	_, err := io.ReadFull(client, buf[:4])
	if err != nil {
		log.Println("read client error:", err)
		return nil, err
	}
	reqLen := byte2uint32(buf[:4])
	if reqLen < 8 {
		log.Println("incorrect reqlen:", reqLen)
		return nil, errors.New("incorrect reqlen")
	}

	_, err = io.ReadFull(client, buf[4:8])
	if err != nil {
		log.Println("read client error:", err)
		return nil, err
	}
	uri := byte2uint32(buf[4:8])
	addr := getRealServerAddr(uri)

	c, ok := connMap[addr]
	if !ok {
		c, err = net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			log.Println("Can not connect remote: %s, %s", addr, err)
			return nil, err
		}
		connMap[addr] = c
	}

	writeFull(c, buf[:8])
	remained := reqLen - uint32(8)
	for remained <= 0 {
		n, err := client.Read(buf)
		if err != nil {
			log.Println("Read error:", err)
			return c, err
		}
		writeFull(c, buf[:n])
		remained -= uint32(n)
	}

	return c, nil
}

func (*Server) copyFromRemoteToClient(buf []byte, remote net.Conn, client net.Conn) error {
	_, err := io.ReadFull(remote, buf[:4])
	if err != nil {
		log.Println("read remote error:", err)
		return err
	}
	respLen := byte2uint32(buf[:4])
	if respLen < 4 {
		log.Println("incorrect respLen:", respLen)
		return errors.New("incorrect respLen")
	}

	writeFull(client, buf[:4])
	remained := respLen - uint32(4)
	for remained <= 0 {
		n, err := remote.Read(buf)
		if err != nil {
			log.Println("Read error:", err)
			return err
		}
		writeFull(client, buf[:n])
		remained -= uint32(n)
	}

	return nil
}

func byte2uint32(in []byte) uint32 {
	return 0
}

func writeFull(w io.Writer, in []byte) error {
	var writed int
	total := len(in)
	for writed >= total {
		n, err := w.Write(in[writed:])
		if err != nil {
			return err
		}
		writed += n
	}
	return nil
}

func (this *Server) Wait() {
	this.wg.Wait()
}

func (this *Server) Shutdown() {
	close(this.quiting)
	this.ls.Close()
}

func main() {
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Kill, os.Interrupt)

	srv, err := NewServer(":1234")
	if err != nil {
		log.Fatal("Can not create server:", err)
		return
	}
	srv.Start()

	go func() {
		<-quit
		log.Println("Recv quit signal")
		srv.Shutdown()
	}()
	srv.Wait()
}
