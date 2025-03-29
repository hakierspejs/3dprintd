package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type PrinterProgress struct {
	Completion float64 `json:"completion"`
}

type PrinterResponse struct {
	State    string          `json:"state"`
	Progress PrinterProgress `json:"progress"`
}

func readFileContentsTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(data))
}

var username = "hakierspejs"
var password = readFileContentsTrimmed("secrets/http-password.txt")

var matrixUserID = readFileContentsTrimmed("secrets/matrix-user-id.txt")
var matrixUsername = readFileContentsTrimmed("secrets/matrix-username.txt")
var matrixPassword = readFileContentsTrimmed("secrets/matrix-password.txt")
var matrixRoomID = readFileContentsTrimmed("secrets/matrix-room-id.txt")

func main() {
	go monitorPrinterState("ender-d")
	go monitorPrinterState("ender-c")

	http.HandleFunc("/", indexHandler)

	http.HandleFunc("/ender-d/cam/", webcamHandler("ender-d"))
	http.HandleFunc("/ender-d/status/", printersStatusHandler("ender-d"))
	http.HandleFunc("/ender-d/cancel/", cancelPrintJob("ender-d"))
	http.HandleFunc("/ender-d/", viewHandler("ender-d"))

	http.HandleFunc("/ender-c/cam/", webcamHandler("ender-c"))
	http.HandleFunc("/ender-c/status/", printersStatusHandler("ender-c"))
	http.HandleFunc("/ender-c/cancel/", cancelPrintJob("ender-c"))
	http.HandleFunc("/ender-c/", viewHandler("ender-c"))

	http.HandleFunc("/lights/off/", lightsOff)
	http.HandleFunc("/lights/on/", lightsOn)
	fmt.Println("Server is listening on port 5000...")
	if err := http.ListenAndServe("0.0.0.0:5000", nil); err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	// Check basic auth
	auth := r.Header.Get("Authorization")
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	if auth != expectedAuth {
		w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Print a list with links to supported printers
	_, _ = fmt.Fprintf(w, "<h1>Supported printers:</h1>")
	_, _ = fmt.Fprintf(w, "<ul>")
	_, _ = fmt.Fprintf(w, "<li><a href=\"/ender-d/\">Ender D</a></li>")
	_, _ = fmt.Fprintf(w, "<li><a href=\"/ender-c/\">Ender C</a></li>")
	_, _ = fmt.Fprintf(w, "</ul>")
}

func getPrinterState(printer string) (PrinterResponse, error) {
	// Read API key from file
	apiKey := readFileContentsTrimmed("secrets/" + printer + "-api-key.txt")

	// Create HTTP request
	req, err := http.NewRequest("GET", "https://opti3d.siedziba.hs-ldz.pl/"+printer+"/api/job", nil)
	if err != nil {
		return PrinterResponse{}, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add API key header
	req.Header.Add("X-Api-Key", apiKey)

	// Create HTTP client that skips SSL verification
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return PrinterResponse{}, fmt.Errorf("failed to execute HTTP request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return PrinterResponse{}, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse JSON
	var printerResp PrinterResponse
	if err := json.Unmarshal(body, &printerResp); err != nil {
		return PrinterResponse{}, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return printerResp, nil
}

// curl -k --json '{"command": "cancel"}' 'https://localhost/ender-c/api/job' -H "X-Api-Key: `cat ~/.octo-ender-c-api`"
func cancelPrintJob(printer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read API key from file
		apiKey := readFileContentsTrimmed("secrets/" + printer + "-api-key.txt")

		// Create HTTP request
		req, err := http.NewRequest("POST", "https://opti3d.siedziba.hs-ldz.pl/"+printer+"/api/job", strings.NewReader(`{"command": "cancel"}`))
		if err != nil {
			log.Printf("failed to create HTTP request: %v", err)
		}

		// Add API key header
		req.Header.Add("X-Api-Key", apiKey)
		req.Header.Add("Content-Type", "application/json")

		// Create HTTP client that skips SSL verification
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}

		// Make the request
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("failed to execute HTTP request: %v", err)
		}

		defer func() {
			_ = resp.Body.Close()
		}()

		// Read response body
		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("failed to read response body: %v", err)
		}
		msg := fmt.Sprintf("Cancel button clicked. Response: %s", respBytes)
		log.Println(msg)
		sendMatrixMsg(msg)

	}
}

func getClient() (*mautrix.Client, id.RoomID, error) {
	// Read password from file
	// Create a new Matrix client
	client, err := mautrix.NewClient("https://matrix.org", id.UserID(matrixUserID), "")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Matrix client: %w", err)
	}

	// Login with password
	ctx := context.Background()
	resp, err := client.Login(ctx, &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: matrixUsername,
		},
		Password: matrixPassword,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to login: %w", err)
	}

	// Set the access token from the login response
	client.AccessToken = resp.AccessToken

	return client, id.RoomID(matrixRoomID), nil
}

func sendMatrixMsg(msg string) error {
	client, roomID, err := getClient()
	if err != nil {
		return err
	}

	// Send the message
	ctx := context.Background()
	sendResp, err := client.SendMessageEvent(ctx, roomID, event.EventMessage, &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    msg,
	})
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	log.Printf("sent text message(msg=%q) => %s", msg, sendResp.EventID)
	return nil
}

func monitorPrinterState(printer string) {
	log.Println("Starting printer state monitoring...")

	// Get initial state
	resp, err := getPrinterState(printer)
	if err != nil {
		log.Printf("initial state check failed: %v", err)
	}
	currentState := resp.State

	log.Printf("Initial printer state: %s", currentState)

	// Poll for state changes every 30 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		resp, err := getPrinterState(printer)
		if err != nil {
			log.Printf("Error checking printer state: %v", err)
			continue
		}
		newState := resp.State

		// If state changed, send notification
		if newState != currentState {
			log.Printf("Printer %s state changed: %s -> %s", printer, currentState, newState)

			message := fmt.Sprintf("🖨️ Printer %s state changed: %s -> %s", printer, currentState, newState)
			err = sendMatrixMsg(message)
			if err != nil {
				log.Printf("Error sending notification: %v", err)
			}

			// Update current state
			currentState = newState
		}
	}
}

func webcamHandler(printer string) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		// Uwierzytelnienie Basic Auth
		auth := r.Header.Get("Authorization")
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

		if auth != expectedAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var streamURL = "https://opti3d.siedziba.hs-ldz.pl/" + printer + "/webcam/?action=stream"

		// Żądanie strumienia wideo od zewnętrznego serwera
		req, err := http.NewRequest("GET", streamURL, nil)
		if err != nil {
			http.Error(w, "Error creating request", http.StatusInternalServerError)
			return
		}

		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))

		// Wyłącz weryfikację SSL (tylko w przypadku problemów z certyfikatami SSL)
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		// Przekaż nagłówki odpowiedzi do klienta
		for k, v := range resp.Header {
			w.Header()[k] = v
		}

		// Ustaw rodzaj treści na multipart
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))

		// Przewód treści strumienia do klienta
		if _, err = io.Copy(w, resp.Body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func printersStatusHandler(printer string) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

		if auth != expectedAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		resp, err := getPrinterState(printer)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Ustaw nagłówki odpowiedzi
		w.Header().Set("Content-Type", "application/json")

		// Zwróć stan drukarki jako JSON
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func viewHandler(printer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

		if auth != expectedAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		tmpl, err := template.ParseFiles("view_template.html")
		if err != nil {
			http.Error(w, "Could not load template", http.StatusInternalServerError)
			return
		}

		data := struct {
			Printer string
		}{
			Printer: printer,
		}

		w.Header().Set("Content-Type", "text/html")
		_ = tmpl.Execute(w, data)
	}
}

func lightsSet(w http.ResponseWriter, r *http.Request, power string) {
	auth := r.Header.Get("Authorization")
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	if auth != expectedAuth {
		w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	url := "http://10.14.22.148/cm?cmnd=Power%20" + power
	//url := "http://localhost/cm?cmnd=Power%20" + power
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	w.WriteHeader(http.StatusOK)
}

func lightsOn(w http.ResponseWriter, r *http.Request) {
	lightsSet(w, r, "On")
}

func lightsOff(w http.ResponseWriter, r *http.Request) {
	lightsSet(w, r, "Off")
}
