package main

import (
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

type SymbolHub struct {
	symbol        string
	clients       map[*Client]bool
	mu            sync.Mutex
	binanceConn   *websocket.Conn
	register      chan *Client
	unregister    chan *Client
	started       bool
	klineInterval string
}

var hubs = make(map[string]*SymbolHub)
var hubsMu sync.Mutex

func getOrCreateHub(symbol string, interval string) *SymbolHub {
	key := symbol + "_" + interval
	hubsMu.Lock()
	defer hubsMu.Unlock()

	if hub, ok := hubs[key]; ok {
		return hub
	}

	hub := &SymbolHub{
		symbol:        symbol,
		klineInterval: interval,
		clients:       make(map[*Client]bool),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
	}
	hubs[key] = hub
	go hub.run()
	return hub
}

func (hub *SymbolHub) run() {
	go hub.connectBinance()

	for {
		select {
		case client := <-hub.register:
			hub.mu.Lock()
			hub.clients[client] = true
			hub.mu.Unlock()

		case client := <-hub.unregister:
			hub.mu.Lock()
			if _, ok := hub.clients[client]; ok {
				delete(hub.clients, client)
				close(client.send)
			}
			hub.mu.Unlock()
		}
	}
}

func (hub *SymbolHub) connectBinance() {
	symbolLower := strings.ToLower(hub.symbol)
	streams := []string{
		symbolLower + "@ticker",
		symbolLower + "@depth20@100ms",
		symbolLower + "@trade",
		symbolLower + "@kline_" + hub.klineInterval,
	}
	url := "wss://stream.binance.com:9443/stream?streams=" + strings.Join(streams, "/")

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Println("Error connecting to Binance:", err)
		return
	}
	hub.binanceConn = conn

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Binance read error:", err)
			return
		}

		hub.mu.Lock()
		for client := range hub.clients {
			select {
			case client.send <- msg:
			default:
				close(client.send)
				delete(hub.clients, client)
			}
		}
		hub.mu.Unlock()
	}
}

func proxyWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	query := r.URL.Query()
	symbol := query.Get("symbol")
	if symbol == "" {
		symbol = "btcusdt"
	}
	interval := query.Get("interval")
	if interval == "" {
		interval = "1m"
	}

	hub := getOrCreateHub(symbol, interval)
	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}
	hub.register <- client

	go func() {
		for msg := range client.send {
			err := client.conn.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				break
			}
		}
		client.conn.Close()
	}()

	for {
		_, _, err := client.conn.ReadMessage()
		if err != nil {
			break
		}
	}

	hub.unregister <- client
}

func main() {
	http.HandleFunc("/ws", proxyWebSocket)

	log.Println("WebSocket proxy server started on :8888")
	err := http.ListenAndServe(":8888", nil)
	if err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}
