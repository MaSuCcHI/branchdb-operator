package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/keisuke/zfs-db-k8s/internal/interface/api"
)

func TestWSHub_Publishはクライアントにイベントをブロードキャストする(t *testing.T) {
	hub := api.NewWSHub()
	go hub.Run()

	// Serve the hub via a test HTTP server.
	router := api.NewWSRouter(hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Connect a WebSocket client.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	// Give the hub a moment to register the client.
	time.Sleep(50 * time.Millisecond)

	// Publish an event.
	hub.Publish("test-event", map[string]string{"key": "value"})

	// Read the message from the WebSocket.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ws message: %v", err)
	}
	if !strings.Contains(string(msg), "test-event") {
		t.Errorf("got message %q, expected it to contain 'test-event'", string(msg))
	}
}

func TestWSHub_接続が切れたときパニックしない(t *testing.T) {
	hub := api.NewWSHub()
	go hub.Run()

	router := api.NewWSRouter(hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}

	// Close the connection without clean handshake (simulate client disconnect).
	conn.Close()

	// Publish should not panic after client disconnects.
	time.Sleep(100 * time.Millisecond)
	hub.Publish("after-disconnect", nil)
}

func TestWSHub_Publishはバッファフル時にパニックしない(t *testing.T) {
	hub := api.NewWSHub()
	go hub.Run()

	// Publish many messages rapidly to fill the buffer and trigger the default case.
	for i := 0; i < 300; i++ {
		hub.Publish("flood", i)
	}
	// If we get here without panic, the test passes.
}

func TestWSHub_メッセージ受信後に接続を閉じてもパニックしない(t *testing.T) {
	hub := api.NewWSHub()
	go hub.Run()

	router := api.NewWSRouter(hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}

	// Allow hub to register client.
	time.Sleep(50 * time.Millisecond)

	// Publish multiple messages to exercise writePump.
	for i := 0; i < 5; i++ {
		hub.Publish("multi", i)
	}

	// Read all messages.
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	// Send a close message (exercises readPump cleanup).
	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()
}

func TestWSHub_クライアント送信バッファ満杯時に接続を切断しても安全(t *testing.T) {
	hub := api.NewWSHub()
	go hub.Run()

	router := api.NewWSRouter(hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	time.Sleep(30 * time.Millisecond)

	// writePump が TCP バックプレッシャーでブロックする前に c.send (cap=256) を
	// 溢れさせるため、大量かつ高頻度でメッセージを送る。
	// クライアント側は一切 ReadMessage しないので OS の受信バッファが満杯になり、
	// サーバ側 WriteMessage がブロック → writePump が止まる → c.send が満杯 → default: が発火。
	large := strings.Repeat("x", 4096)
	for i := 0; i < 512; i++ {
		hub.Publish("flood", large)
	}
	time.Sleep(200 * time.Millisecond)
}

func TestWSHub_PublishはJSONシリアライズ不能なデータでパニックしない(t *testing.T) {
	hub := api.NewWSHub()
	go hub.Run()
	// channel は json.Marshal できないのでエラーパスが通る
	hub.Publish("test", make(chan int))
}

func TestWSHub_ServeWSは非WebSocketリクエストを処理しない(t *testing.T) {
	hub := api.NewWSHub()
	go hub.Run()

	router := api.NewWSRouter(hub)

	// Plain HTTP request to /ws (not a WebSocket upgrade) should not panic.
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	// WebSocket upgrade fails on a non-upgraded request; it returns an error but shouldn't panic.
}
