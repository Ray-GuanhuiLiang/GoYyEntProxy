// GoYyEntProxy服务是用于解决在开发联调时把某些EntProxy的接口转发到特定的服务器。
// 这个问题产生的原因主要是由于目前Daemon是全局配置的，修改了特定的uri指向的进程名会影响到其他的测试人员。

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
	conf    *Config
}

func NewServer(conf *Config) (*Server, error) {
	q := make(chan interface{})
	ls, err := net.Listen("tcp", conf.Server.Bind)
	if err != nil {
		close(q)
		return nil, err
	}
	log.Println("Create server", conf.Server.Bind)
	return &Server{ls: ls, quiting: q, conf: conf}, nil
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

func (this *Server) getRealServerAddr(uri int64) (redir string) {
	major := uint8(uri % 256)
	minor := uint32(uri / 256)
	defer func() {
		log.Printf("major %d minor %d redir %s\n", major, minor, redir)
	}()

	// 第一次循环搜索完全匹配大类和小类的
	for _, sc := range this.conf.Proxy.Specific {
		if major == sc.Major && minor == sc.Minor {
			redir = sc.Redir
			return
		}
	}
	// 第二次循环搜索完全匹配大类，小类没有设置的
	for _, sc := range this.conf.Proxy.Specific {
		if major == sc.Major && sc.Minor == 0 {
			redir = sc.Redir
			return
		}
	}
	// 返回默认值
	redir = this.conf.Proxy.Default
	return
}

func (this *Server) handleClient(client net.Conn) {
	defer this.wg.Done()
	defer client.Close()

	connMap := make(map[string]net.Conn)
	buf := make([]byte, 8)

	defer func() {
		for _, conn := range connMap {
			log.Println("close remote connect", conn.RemoteAddr())
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

func (this *Server) copyFromClientToRemote(connMap map[string]net.Conn, buf []byte, client net.Conn) (net.Conn, error) {

	_, err := io.ReadFull(client, buf[:4])
	if err != nil {
		log.Println("read client error:", err)
		return nil, err
	}
	reqLen := byte2int64(buf[:4])
	log.Println("Req len", reqLen)
	if reqLen < 8 {
		log.Println("incorrect reqlen:", reqLen)
		return nil, errors.New("incorrect reqlen")
	}

	_, err = io.ReadFull(client, buf[4:8])
	if err != nil {
		log.Println("read client error:", err)
		return nil, err
	}
	uri := byte2int64(buf[4:8])
	log.Println("Req uri", uri)
	addr := this.getRealServerAddr(uri)
	log.Println("Remote addr", addr)

	c, ok := connMap[addr]
	if !ok {
		c, err = net.DialTimeout("tcp", addr, time.Duration(this.conf.Proxy.ConnectTimeout)*time.Millisecond)
		if err != nil {
			log.Printf("Can not connect remote: %s, %s\n", addr, err)
			return nil, err
		}
		log.Println("Connected to remote", addr, c)
		connMap[addr] = c
	}

	_, err = c.Write(buf[:8])
	if err != nil {
		log.Println("Can not write to remote:", err)
		return nil, err
	}
	remained := reqLen - 8
	log.Println("read from client remaind", remained)
	copied, err := io.CopyN(c, client, remained)
	if err != nil {
		log.Println("Can not copy data from client to remote:", err)
		return nil, err
	}
	log.Println("finish copy from client to remote", copied)

	return c, nil
}

func (*Server) copyFromRemoteToClient(buf []byte, remote net.Conn, client net.Conn) error {
	_, err := io.ReadFull(remote, buf[:4])
	if err != nil {
		log.Println("read remote error:", err)
		return err
	}
	respLen := byte2int64(buf[:4])
	log.Println("resp len", respLen)
	if respLen < 4 {
		log.Println("incorrect respLen:", respLen)
		return errors.New("incorrect respLen")
	}

	_, err = client.Write(buf[:4])
	if err != nil {
		log.Println("Can not write to client:", err)
		return err
	}

	remained := respLen - 4
	copied, err := io.CopyN(client, remote, remained)
	log.Println("finish copy from remote to client", copied)

	if copied == remained {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("Unexpcect EOF from remote")
}

func byte2int64(in []byte) int64 {
	return int64(in[0]) + int64(in[1])<<8 + int64(in[2])<<16 + int64(in[3])<<24
}

func (this *Server) Wait() {
	this.wg.Wait()
}

func (this *Server) Shutdown() {
	close(this.quiting)
	this.ls.Close()
}

func main() {
	conf, err := NewConfig("app.yaml")
	if err != nil {
		log.Fatal("Can not load config", err)
		return
	}
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Kill, os.Interrupt)

	srv, err := NewServer(conf)
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
