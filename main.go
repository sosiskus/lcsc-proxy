package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

// handleLCSCSearch handles requests to /search-lcsc
func handleLCSCSearch(w http.ResponseWriter, r *http.Request) {
	// Log the incoming request
	log.Printf("Received request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	// --- 1. Extract the 'part' query parameter ---
	queryValues := r.URL.Query()
	part := queryValues.Get("part") // Get the value of the 'part' parameter

	// Check if the 'part' parameter is provided
	if part == "" {
		log.Println("Error: 'part' query parameter is missing")
		http.Error(w, "'part' query parameter is required", http.StatusBadRequest) // Send 400 Bad Request
		return
	}

	log.Printf("Searching for part: %s", part)

	// --- 2. Construct the LCSC search URL ---
	// LCSC search URL format: https://www.lcsc.com/search?q=<encoded_part_name>
	lcscSearchURL := fmt.Sprintf("https://www.lcsc.com/search?q=%s", url.QueryEscape(part))
	log.Printf("Constructed LCSC URL: %s", lcscSearchURL)

	// --- 3. Make the request to LCSC ---
	// Create a new HTTP client
	client := &http.Client{}

	// Create a new GET request to the LCSC URL
	req, err := http.NewRequest("GET", lcscSearchURL, nil)
	if err != nil {
		log.Printf("Error creating request to LCSC: %v", err)
		http.Error(w, "Failed to create request to LCSC", http.StatusInternalServerError)
		return
	}

	// It's good practice to mimic browser headers to avoid potential blocking
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error fetching data from LCSC: %v", err)
		http.Error(w, "Failed to fetch data from LCSC", http.StatusBadGateway) // Send 502 Bad Gateway
		return
	}
	// Ensure the response body is closed when the function returns
	defer resp.Body.Close()

	log.Printf("LCSC response status: %s", resp.Status)

	// --- 4. Forward the LCSC response back to the client ---

	// Copy headers from LCSC response to our response
	// Important headers like Content-Type will be copied
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set the status code from the LCSC response
	w.WriteHeader(resp.StatusCode)

	// Copy the response body from LCSC to our response writer
	// io.Copy efficiently streams the data
	bytesCopied, err := io.Copy(w, resp.Body)
	if err != nil {
		// Log error if copying fails mid-stream, but we can't send an HTTP error anymore
		log.Printf("Error copying response body from LCSC: %v", err)
		return // Stop processing
	}

	log.Printf("Successfully forwarded %d bytes from LCSC for part: %s", bytesCopied, part)
}

func main() {
	// --- Server Setup ---
	// Register the handler function for the /search-lcsc path
	http.HandleFunc("/search-lcsc", handleLCSCSearch)

	// Define the server address and port
	// It will listen on all available network interfaces on port 3567
	listenAddr := ":3567"
	log.Printf("Starting proxy server on %s", listenAddr)

	// Start the HTTP server
	// ListenAndServe blocks until the server is stopped (e.g., by an error or signal)
	err := http.ListenAndServe(listenAddr, nil)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err) // Log fatal error if server fails to start
	}
}
