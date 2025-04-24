package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/huantt/plaintext-extractor"
	"tailscale.com/tsnet"
)

// WebhookConfig stores the webhook path
type WebhookConfig struct {
	Path string `json:"path"`
}

// generateSecurePath creates a random 32-byte hex string
func generateSecurePath() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// getWebhookPath returns the webhook path, creating a new one if needed
func getWebhookPath() (string, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %v", err)
	}

	configDir := filepath.Join(userConfigDir, "puzzmo2signal")
	configFile := filepath.Join(configDir, "webhook_config.json")

	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %v", err)
	}

	// Try to read existing config
	data, err := os.ReadFile(configFile)
	if err == nil {
		var config WebhookConfig
		if err := json.Unmarshal(data, &config); err == nil && config.Path != "" {
			return config.Path, nil
		}
	}

	// Generate new path if needed
	path, err := generateSecurePath()
	if err != nil {
		return "", err
	}

	// Save the new path
	config := WebhookConfig{Path: path}
	data, err = json.Marshal(config)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return "", err
	}

	return path, nil
}

// DiscordWebhook represents the structure of a Discord webhook payload
type DiscordWebhook struct {
	Content string `json:"content"`
	// Add other fields as needed
}

// SignalAPIPayload represents the structure of the Signal API request
type SignalAPIPayload struct {
	Number     string   `json:"number"`
	Message    string   `json:"message"`
	Recipients []string `json:"recipients"`
}

// Create a handler factory function that takes the flag value
func makeWebhookHandler(preserveMarkdown bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow POST method
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get Signal configuration from environment variables
		signalGroup := os.Getenv("SIGNAL_GROUP_ID")
		signalPhone := os.Getenv("SIGNAL_PHONE")
		signalAPIURL := os.Getenv("SIGNAL_API_URL")

		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Send 200 response immediately after successful read
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Webhook received"))

		var message string
		// Try to parse as Discord webhook first
		var discordPayload DiscordWebhook
		if err := json.Unmarshal(body, &discordPayload); err == nil && discordPayload.Content != "" {
			message = discordPayload.Content
		} else {
			log.Printf("Invalid webhook format")
			return
		}

		log.Printf("Message received: %s", message)

		finalMessage := message
		if !preserveMarkdown {
			extractor := plaintext.NewMarkdownExtractor()
			plaintextMessagePtr, err := extractor.PlainText(message)
			if err != nil {
				log.Printf("Error extracting plaintext message: %v", err)
				return
			}
			finalMessage = *plaintextMessagePtr
		}

		// Prepare Signal API payload
		signalPayload := SignalAPIPayload{
			Number:     signalPhone,
			Message:    finalMessage,
			Recipients: []string{signalGroup},
		}

		// Ensure URL has a scheme
		apiURL := signalAPIURL
		if !strings.HasPrefix(apiURL, "http://") && !strings.HasPrefix(apiURL, "https://") {
			apiURL = "http://" + apiURL
		}

		// Create request body using json.NewEncoder
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(signalPayload); err != nil {
			log.Printf("Error encoding request body: %v", err)
			return
		}

		// Create full request URL
		fullURL := apiURL + "/v2/send"
		log.Printf("Making request to: %s", fullURL)

		// Send POST request to Signal API
		req, err := http.NewRequest("POST", fullURL, &buf)
		if err != nil {
			log.Printf("Error creating request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error sending Signal message: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Read and log error response body
			respBody, _ := io.ReadAll(resp.Body)
			log.Printf("Signal API returned non-200 status: %d, response: %s", resp.StatusCode, string(respBody))
			return
		}
	}
}

func main() {
	// Verify required environment variables
	requiredEnvVars := []string{"TS_HOSTNAME", "TS_AUTHKEY", "SIGNAL_PHONE", "SIGNAL_GROUP_ID", "SIGNAL_API_URL"}
	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			log.Fatalf("%s environment variable is required", envVar)
		}
	}

	preserveMarkdown := flag.Bool("preserve-markdown", false, "Preserve markdown in the message")
	flag.Parse()

	// Get or create webhook path
	webhookPath, err := getWebhookPath()
	if err != nil {
		log.Fatalf("Failed to setup webhook path: %v", err)
	}

	// Create a new tsnet Server
	s := &tsnet.Server{
		Hostname: os.Getenv("TS_HOSTNAME"), // e.g., "puzzmo-webhook"
	}
	defer s.Close()

	// Start the Funnel listener
	ln, err := s.ListenFunnel("tcp", ":443")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	// Set up the webhook handler
	mux := http.NewServeMux()
	mux.HandleFunc("/"+webhookPath, makeWebhookHandler(*preserveMarkdown))

	log.Printf("Server starting with Tailscale Funnel enabled")
	log.Printf("Listening on: https://%v/%s", s.CertDomains()[0], webhookPath)
	if err := http.Serve(ln, mux); err != nil {
		log.Fatal(err)
	}
}
