package main

import (
	"context"
	"fmt"
	hardware "g0-monitor/internal"
	"g0-monitor/views"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/a-h/templ"

	"nhooyr.io/websocket"
)

type Server struct {
	subscriberMessageBuffer int
	mux                     http.ServeMux
	subscribers             map[*Subscriber]struct{}
	subscribersMutex        sync.Mutex
}

type Subscriber struct {
	msgs chan []byte
}

func StartServer() *Server {
	fs := http.FileServer(http.Dir("./static"))

	s := &Server{
		subscriberMessageBuffer: 10,
		subscribers:             make(map[*Subscriber]struct{}),
	}

	s.mux.Handle("/", templ.Handler(views.Index()))
	s.mux.Handle("/static/", http.StripPrefix("/static/", fs))
	s.mux.HandleFunc("/ws", s.subscribeHandler)
	return s
}

func (s *Server) subscribeHandler(writer http.ResponseWriter, req *http.Request) {
	err := s.subscribe(req.Context(), writer, req)
	if err != nil {
		log.Fatalln(err)
		return
	}
}

func (s *Server) subscribe(ctx context.Context, writer http.ResponseWriter, req *http.Request) error {
	var c *websocket.Conn

	subscriber := Subscriber{
		msgs: make(chan []byte, s.subscriberMessageBuffer),
	}

	s.addSubscriber(&subscriber)

	c, err := websocket.Accept(writer, req, nil)
	if err != nil {
		return err
	}

	defer c.CloseNow()

	ctx = c.CloseRead(ctx)
	for {
		select {
		case msg := <-subscriber.msgs:
			ctx, cancel := context.WithTimeout(ctx, time.Second)

			defer cancel()

			err := c.Write(ctx, websocket.MessageText, msg)
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Server) addSubscriber(subscriber *Subscriber) {
	s.subscribersMutex.Lock()
	s.subscribers[subscriber] = struct{}{}
	s.subscribersMutex.Unlock()
}

func (s *Server) broadcast(msg []byte) {
	s.subscribersMutex.Lock()

	for subscriber := range s.subscribers {
		subscriber.msgs <- msg
	}

	s.subscribersMutex.Unlock()
}

func (cs *Server) publishMsg(msg []byte) {
	cs.subscribersMutex.Lock()
	defer cs.subscribersMutex.Unlock()

	for s := range cs.subscribers {
		s.msgs <- msg
	}
}

func main() {
	fmt.Println("booting system monitor...")

	srv := StartServer()

	go func(s *Server) {
		for {
			systemSection, err := hardware.GetSystemSection()
			if err != nil {
				fmt.Println(err)
				continue
			}
			diskSection, err := hardware.GetDiskSection()
			if err != nil {
				fmt.Println(err)
				continue
			}
			cpuSection, err := hardware.GetCpuSection()
			if err != nil {
				fmt.Println(err)
				continue
			}

			timeStamp := time.Now().Format("2006-01-02 15:04:05")
			msg := []byte(`
      			<div hx-swap-oob="innerHTML:#update-timestamp">
        		<p><i style="color: green" class="fa fa-circle"></i> ` + timeStamp + `</p>
      			</div>
      			<div hx-swap-oob="innerHTML:#system-data">` + systemSection + `</div>
      			<div hx-swap-oob="innerHTML:#cpu-data">` + diskSection + `</div>
      			<div hx-swap-oob="innerHTML:#disk-data">` + cpuSection + `</div>`)

			srv.broadcast(msg)

			time.Sleep(3 * time.Second)
		}
	}(srv)

	err := http.ListenAndServe(":3030", &srv.mux)
	if err != nil {
		log.Fatalln(err)
	}
}
