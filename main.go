package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"time"

	"github.com/gorilla/websocket"
)

// Config represents the persistent CLI settings
type Config struct {
	InspectorID   string `json:"inspectorId"`
	LocalEndpoint string `json:"localEndpoint"`
}

// WebhookPayload represents the structure of the data received from WebSocket.
// Now, the Body field is an object.
type WebhookPayload struct {
	ID      string                 `json:"id"`
	Headers map[string]string      `json:"headers"`
	Body    map[string]interface{} `json:"body"`
	Method  string                 `json:"method"`
	Query   map[string]string      `json:"query"`
}

func main() {
	fmt.Println("=== Webhook Inspector Client ===")

	// Check if a configuration file already exists
	configFile := "config.json"
	var config Config

	if _, err := os.Stat(configFile); err == nil {
		// Configuration file exists, load data
		data, err := os.ReadFile(configFile)
		if err != nil {
			log.Fatal("Error reading configuration file:", err)
		}
		if err := json.Unmarshal(data, &config); err != nil {
			log.Fatal("Error parsing configuration file:", err)
		}
		fmt.Println("Using configured WebhookInspectorId and endpoint:")
		fmt.Println("  WebhookInspectorId:", config.InspectorID)
		fmt.Println("  Local Endpoint:", config.LocalEndpoint)
	} else {
		// File does not exist: ask the user for input and save it
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Enter WebhookInspectorId: ")
		id, _ := reader.ReadString('\n')
		config.InspectorID = strings.TrimSpace(id)
		fmt.Print("Enter the local endpoint URL to forward webhooks: ")
		endpoint, _ := reader.ReadString('\n')
		config.LocalEndpoint = strings.TrimSpace(endpoint)

		// Save configuration to file
		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			log.Fatal("Error creating configuration file:", err)
		}
		if err := os.WriteFile(configFile, data, 0644); err != nil {
			log.Fatal("Error saving configuration file:", err)
		}
		fmt.Println("Configuration saved in", configFile)
	}

	// Webhook Inspector WebSocket URL
	webhookInspectorWS := "ws://ws.webhookinspector.com/ws"

	// Canal para controlar a reconexão
	done := make(chan struct{})

	// Capture termination signals (Ctrl+C)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	// Create a separate goroutine for connection management
	go func() {
		for {
			connectAndListen(webhookInspectorWS, &config, done)

			select {
			case <-done:
				return
			default:
				fmt.Println("Connection lost. Reconnecting in 5 seconds...")
				time.Sleep(5 * time.Second)
			}
		}
	}()

	// Wait for interrupt signal
	<-interrupt
	fmt.Println("\nReceived interrupt signal. Closing...")
	close(done)
	// Give some time for graceful shutdown
	time.Sleep(1 * time.Second)
	os.Exit(0)
}

func connectAndListen(webhookInspectorWS string, config *Config, done chan struct{}) {
	conn, _, err := websocket.DefaultDialer.Dial(webhookInspectorWS, nil)
	if err != nil {
		log.Println("Error connecting to WebSocket:", err)
		return
	}
	defer conn.Close()
	fmt.Println("Connected! Listening for events...")

	// Configurar ping/pong
	conn.SetPingHandler(func(string) error {
		return conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(10*time.Second))
	})

	// Enviar ping periodicamente
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Loop para ler mensagens
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Error reading message:", err)
			return
		}

		var payload WebhookPayload
		if err := json.Unmarshal(message, &payload); err != nil {
			fmt.Println("Error decoding JSON:", err)
			continue
		}

		// Filter only requests with the corresponding id
		if payload.ID != config.InspectorID {
			fmt.Println("Webhook received with different id. Ignoring.")
			continue
		}

		// Forward the request to the local endpoint
		fmt.Println("Forwarding webhook to:", config.LocalEndpoint)
		forwardWebhook(config.LocalEndpoint, payload)
	}
}

// forwardWebhook forwards the webhook to the local endpoint,
// adding query params to the URL if present.
func forwardWebhook(urlStr string, payload WebhookPayload) {
	// Parse the URL
	u, err := url.Parse(urlStr)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return
	}

	// If there are query params, add them to the URL
	if payload.Query != nil && len(payload.Query) > 0 {
		q := u.Query()
		for key, value := range payload.Query {
			q.Set(key, value)
		}
		u.RawQuery = q.Encode()
	}

	// Convert the body (object) to JSON
	bodyBytes, err := json.Marshal(payload.Body)
	if err != nil {
		fmt.Println("Error converting body to JSON:", err)
		return
	}
	reqBody := bytes.NewBuffer(bodyBytes)

	req, err := http.NewRequest(payload.Method, u.String(), reqBody)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}

	// Add original headers
	for key, value := range payload.Headers {
		req.Header.Set(key, value)
	}

	// If the body is a JSON object, set the Content-Type header
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error forwarding webhook:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Webhook successfully forwarded. Status:", resp.Status)
}
