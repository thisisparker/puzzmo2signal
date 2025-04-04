package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

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

// Create a handler factory function that takes the flag value
func makeWebhookHandler(preserveMarkdown bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow POST method
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get Signal group ID from environment variable
		signalGroup := os.Getenv("SIGNAL_GROUP_ID")

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

		// Send message via signal-cli
		signalPhone := os.Getenv("SIGNAL_PHONE")
		if signalPhone == "" {
			log.Printf("SIGNAL_PHONE not configured")
			return
		}

		cmd := exec.Command("signal-cli", "-u", signalPhone, "send", "-g", signalGroup, "-m", finalMessage)
		if err := cmd.Run(); err != nil {
			log.Printf("Error sending Signal message: %v", err)
			return
		}
	}
}

func main() {
	// Verify required environment variables
	requiredEnvVars := []string{"TS_HOSTNAME", "TS_AUTHKEY", "SIGNAL_PHONE", "SIGNAL_GROUP_ID"}
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
