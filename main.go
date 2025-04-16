package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Helper function to find and extract price from a specific node (td)
// It prioritizes the second price found within spans, assuming it's the discount.
func extractPriceFromCell(tdNode *html.Node) (string, bool) {
	var prices []string
	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		// Look inside span elements
		if n.Type == html.ElementNode && n.Data == "span" {
			text := strings.TrimSpace(extractText(n))
			// Check if the text likely contains a price (starts with $ and has a digit)
			if strings.HasPrefix(text, "$") && strings.ContainsAny(text, "0123456789") {
				cleanedPrice := strings.TrimSpace(strings.TrimPrefix(text, "$"))
				// Further validation: Ensure it's parseable as a float
				if _, err := strconv.ParseFloat(cleanedPrice, 64); err == nil {
					prices = append(prices, cleanedPrice)
				}
			}
		}
		// Traverse children, but stop if we go into nested tables etc.
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data != "table" && c.Data != "tr" && c.Data != "td" {
				traverse(c)
			} else if c.Type == html.TextNode { // Also capture direct text nodes that might be prices
				text := strings.TrimSpace(c.Data)
				if strings.HasPrefix(text, "$") && strings.ContainsAny(text, "0123456789") {
					cleanedPrice := strings.TrimSpace(strings.TrimPrefix(text, "$"))
					if _, err := strconv.ParseFloat(cleanedPrice, 64); err == nil {
						prices = append(prices, cleanedPrice)
					}
				}
			}
		}
	}

	traverse(tdNode)

	if len(prices) > 1 {
		log.Printf("Found multiple prices: %v, using the last one (discounted).", prices)
		return prices[len(prices)-1], true // Return the last price found (likely the discount)
	} else if len(prices) == 1 {
		return prices[0], true // Return the single price found
	}

	return "", false // No valid price found
}

// --- Helper function to traverse HTML nodes ---
func findPriceTableData(node *html.Node) (string, bool) {
	if node.Type == html.ElementNode && node.Data == "table" {
		isPriceTable := false
		for _, attr := range node.Attr {
			if attr.Key == "class" && strings.Contains(attr.Val, "priceTable") { // [cite: 310]
				isPriceTable = true
				break
			}
		}
		if isPriceTable {
			var result strings.Builder
			var traverse func(*html.Node)
			traverse = func(n *html.Node) {
				if n.Type == html.ElementNode && n.Data == "tr" {
					var tier, price string
					isStandardPackagingRow := false
					cellCount := 0
					var priceCellNode *html.Node // Store the node for the price cell

					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode && c.Data == "td" {
							cellCount++
							cellText := strings.TrimSpace(extractText(c)) // Trim spaces early

							if cellCount == 1 { // First cell: Quantity Tier [cite: 311, 312]
								tier = strings.ReplaceAll(cellText, ",", "")          // Remove commas
								if strings.Contains(cellText, "Standard Packaging") { // Detect the "Standard Packaging" row [cite: 312]
									isStandardPackagingRow = true
									break // Stop processing this row
								}
								// Ensure tier ends with '+' if it's a tier row
								if !strings.HasSuffix(tier, "+") && tier != "" {
									// Check if it actually contains digits before adding '+'
									containsDigit := false
									for _, r := range tier {
										if r >= '0' && r <= '9' {
											containsDigit = true
											break
										}
									}
									if containsDigit {
										tier += "+"
									} else {
										tier = "" // Not a valid tier if no digits
									}
								}
							} else if cellCount == 2 { // Second cell: Unit Price [cite: 311]
								priceCellNode = c // Store the node to parse prices from
							}
						}
					}

					// Process the price cell after finding it
					if !isStandardPackagingRow && tier != "" && priceCellNode != nil {
						extractedPrice, priceFound := extractPriceFromCell(priceCellNode)
						if priceFound {
							price = extractedPrice
							// Append in the format expected by Apps Script: "QTY+US$PRICE "
							result.WriteString(tier)
							result.WriteString("US$") // Apps Script looks for 'US' then extracts the number
							result.WriteString(price)
							result.WriteString(" ") // Add a space separator
						} else {
							log.Printf("Warning: Tier '%s' found but no valid price extracted from its cell.", tier)
						}
					}
				}
				// Recursively traverse child nodes
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					traverse(c)
				}
			}
			traverse(node)
			formattedData := strings.TrimSpace(result.String())
			if formattedData == "" && isPriceTable {
				log.Println("Warning: Price table found, but no valid tier/price data extracted.")
				return "", false // Indicate not found if result is empty but table was there
			}
			return formattedData, true // Return the formatted string
		}
	}

	// Recursively check child nodes
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if data, found := findPriceTableData(c); found {
			return data, true
		}
	}

	return "", false
}

// --- Helper function to extract all text from a node and its children ---
func extractText(node *html.Node) string {
	if node.Type == html.TextNode {
		// Replace non-breaking spaces and trim regular spaces
		return strings.TrimSpace(strings.ReplaceAll(node.Data, "\u00A0", " "))
	}
	if node.Type != html.ElementNode {
		return ""
	}
	var text strings.Builder
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		text.WriteString(extractText(c))
	}
	// Trim spaces from the combined text of children
	return strings.TrimSpace(text.String())
}

// handleLCSCSearch handles requests to /search-lcsc
func handleLCSCSearch(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	queryValues := r.URL.Query()
	part := queryValues.Get("part")

	if part == "" {
		log.Println("Error: 'part' query parameter is missing")
		http.Error(w, "'part' query parameter is required", http.StatusBadRequest)
		return
	}
	log.Printf("Searching for part: %s", part)

	// Use the product detail page URL format
	// Assumes 'part' is an LCSC Part # like C85934 or C138011 (from image)
	lcscProductURL := fmt.Sprintf("https://www.lcsc.com/product-detail/%s.html", url.QueryEscape(part))
	log.Printf("Constructed LCSC URL: %s", lcscProductURL)

	client := &http.Client{}
	req, err := http.NewRequest("GET", lcscProductURL, nil)
	if err != nil {
		log.Printf("Error creating request to LCSC: %v", err)
		http.Error(w, "Failed to create request to LCSC", http.StatusInternalServerError)
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error fetching data from LCSC: %v", err)
		http.Error(w, "Failed to fetch data from LCSC", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("LCSC response status: %s", resp.Status)

	if resp.StatusCode != http.StatusOK {
		log.Printf("LCSC returned non-OK status: %d", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body) // Read body to potentially log it
		log.Printf("LCSC Response Body: %s", string(bodyBytes))
		http.Error(w, fmt.Sprintf("LCSC returned status %d", resp.StatusCode), resp.StatusCode)
		return
	}

	// --- Parse the HTML response ---
	doc, err := html.Parse(resp.Body)
	if err != nil {
		log.Printf("Error parsing HTML from LCSC: %v", err)
		http.Error(w, "Failed to parse response from LCSC", http.StatusInternalServerError)
		return
	}

	// --- Find and format the price data ---
	priceData, found := findPriceTableData(doc)
	if !found || priceData == "" { // Also check if priceData is empty
		log.Printf("Could not find price table data for part: %s", part)
		http.Error(w, "Price data not found or empty", http.StatusNotFound)
		return
	}

	log.Printf("Extracted price data for %s: %s", part, priceData)

	// --- Send the formatted price data back to the Apps Script client ---
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err = fmt.Fprint(w, priceData)
	if err != nil {
		log.Printf("Error writing response to client: %v", err)
	}

	log.Printf("Successfully sent formatted price data for part: %s", part)
}

func main() {
	http.HandleFunc("/search-lcsc", handleLCSCSearch)
	listenAddr := ":3666"
	log.Printf("Starting enhanced proxy server (with discount handling) on %s", listenAddr)
	err := http.ListenAndServe(listenAddr, nil)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
