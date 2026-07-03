package fakesmtp

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

// Server is a minimal SMTP server that accepts mail for load testing.
type Server struct {
	Addr      string
	Delay     time.Duration
	Accepted  atomic.Int64
	listener  net.Listener
}

func Start(addr string, delay time.Duration) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{Addr: ln.Addr().String(), Delay: delay, listener: ln}
	go s.serve()
	return s, nil
}

func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	if s.Delay > 0 {
		defer time.Sleep(s.Delay)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	write := func(line string) {
		_, _ = fmt.Fprintf(w, "%s\r\n", line)
		_ = w.Flush()
	}

	write("220 loadtest.local ESMTP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			write("250-loadtest.local")
			write("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH"):
			write("235 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM"):
			write("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			write("250 OK")
		case upper == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>")
			for {
				part, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(part) == "." {
					break
				}
			}
			s.Accepted.Add(1)
			write("250 OK queued")
		case strings.HasPrefix(upper, "QUIT"):
			write("221 Bye")
			return
		default:
			write("250 OK")
		}
	}
}
