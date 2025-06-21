package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Cho phép mọi origin (hoặc tự cấu hình theo yêu cầu)
		return true
	},
}

func proxyWebSocket(w http.ResponseWriter, r *http.Request) {
	// Nâng cấp HTTP -> WebSocket
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer clientConn.Close()

	// Đọc các tham số ví dụ ?symbol=btcusdt&interval=1m
	query := r.URL.Query()
	fmt.Print(query)
	symbol := query.Get("symbol")
	if symbol == "" {
		symbol = "btcusdt"
	}
	interval := query.Get("interval")
	if interval == "" {
		interval = "1m"
	}

	symbolLower := strings.ToLower(symbol)
	streams := []string{
		symbolLower + "@ticker",
		symbolLower + "@depth20@100ms",
		symbolLower + "@trade",
		symbolLower + "@kline_" + interval,
	}
	url := "wss://stream.binance.com:9443/stream?streams=" + strings.Join(streams, "/")

	// Kết nối đến Binance
	binanceConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Println("Error connecting to Binance:", err)
		return
	}
	defer binanceConn.Close()

	// Chuyển tiếp dữ liệu từ Binance về client
	go func() {
		for {
			_, msg, err := binanceConn.ReadMessage()
			if err != nil {
				log.Println("Binance read error:", err)
				clientConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			clientConn.WriteMessage(websocket.TextMessage, msg)
		}
	}()

	// (Tuỳ chọn) Nhận dữ liệu từ client gửi đi Binance
	for {
		_, msg, err := clientConn.ReadMessage()
		if err != nil {
			log.Println("Client read error:", err)
			return
		}
		binanceConn.WriteMessage(websocket.TextMessage, msg)
	}
}

func main() {
	http.HandleFunc("/ws", proxyWebSocket)

	log.Println("WebSocket proxy server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}
