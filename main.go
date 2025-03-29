package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	State string `json:"state"`
    Progress PrinterProgress `json:"progress"`
}

func readFileContentsTrimmed(path string) (string) {
    data, err := ioutil.ReadFile(path)
    if err != nil {
        panic(err)
    }
    return strings.TrimSpace(string(data))
}

var username        = "hakierspejs"
var password        = readFileContentsTrimmed("secrets/http-password.txt")
var streamURLenderD = "https://opti3d.siedziba.hs-ldz.pl/ender-d/webcam/?action=stream"
var ENDER_D_API_KEY = readFileContentsTrimmed("secrets/ender-d-api-key.txt")

var matrixUserID    = readFileContentsTrimmed("secrets/matrix-user-id.txt")
var matrixUsername  = readFileContentsTrimmed("secrets/matrix-username.txt")
var matrixPassword  = readFileContentsTrimmed("secrets/matrix-password.txt")
var matrixRoomID    = readFileContentsTrimmed("secrets/matrix-room-id.txt")

func main() {
	//go monitorPrinterState()
	http.HandleFunc("/ender-d/cam/", enderDHandler)
	http.HandleFunc("/ender-d/status/", enderDStatusHandler)
	http.HandleFunc("/ender-d/", enderDView)
	http.HandleFunc("/lights/off/", lightsOff)
	http.HandleFunc("/lights/on/", lightsOn)
	fmt.Println("Server is listening on port 5000...")
	if err := http.ListenAndServe("0.0.0.0:5000", nil); err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func getPrinterState(printer string) (PrinterResponse, error) {
	// Read API key from file
	apiKey := ENDER_D_API_KEY

	// Create HTTP request
	req, err := http.NewRequest("GET", "https://opti3d.siedziba.hs-ldz.pl/" + printer + "/api/job", nil)
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
	defer resp.Body.Close()

	// Read response body
	body, err := ioutil.ReadAll(resp.Body)
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

func sendMsg(msg string) error {
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

func sendImage(imagePath string) error {
	client, roomID, err := getClient()
	if err != nil {
		return err
	}

	// Read the image file
	file, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("failed to open image file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	fileData, err := ioutil.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file data: %w", err)
	}

	// Get MIME type based on file extension
	mimeType := "image/jpeg" // Default to JPEG
	ext := strings.ToLower(filepath.Ext(imagePath))
	switch ext {
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	}

	// Upload the image to the Matrix media server
	ctx := context.Background()
	resp, err := client.UploadMedia(ctx, mautrix.ReqUploadMedia{
		ContentBytes: fileData,
		ContentType:  mimeType,
		FileName:     filepath.Base(imagePath),
	})
	if err != nil {
		return fmt.Errorf("failed to upload image: %w", err)
	}

	// Convert fileInfo.Size() from int64 to int for FileInfo.Size
	fileSize := int(fileInfo.Size())

	// Send a message with the image
	fileName := filepath.Base(imagePath)
	sendResp, err := client.SendMessageEvent(ctx, roomID, event.EventMessage, &event.MessageEventContent{
		MsgType: event.MsgImage,
		Body:    fileName, // Fallback text for clients that can't display images
		URL:     resp.ContentURI.CUString(),
		Info: &event.FileInfo{
			Size:     fileSize, // Now using int type
			MimeType: mimeType,
			// You could add width and height here if you wanted to parse the image
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send image message: %w", err)
	}

	log.Printf("sent image message(%s) => %s", fileName, sendResp.EventID)
	return nil
}

func monitorPrinterState() error {
	log.Println("Starting printer state monitoring...")

	// Get initial state
	resp, err := getPrinterState("ender-d")
	if err != nil {
		return fmt.Errorf("initial state check failed: %w", err)
	}
    currentState := resp.State

	log.Printf("Initial printer state: %s", currentState)

	// Send initial notification
	err = sendMsg(fmt.Sprintf("Printer monitoring started. Current state: %s", currentState))
	if err != nil {
		return fmt.Errorf("failed to send initial notification: %w", err)
	}

	// Poll for state changes every 30 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		resp, err := getPrinterState("ender-d")
		if err != nil {
			log.Printf("Error checking printer state: %v", err)
			continue
		}
        newState := resp.State

		// If state changed, send notification
		if newState != currentState {
			log.Printf("Printer state changed: %s -> %s", currentState, newState)

			message := fmt.Sprintf("🖨️ Printer state changed: %s -> %s", currentState, newState)
			err = sendMsg(message)
			if err != nil {
				log.Printf("Error sending notification: %v", err)
			}

			// Update current state
			currentState = newState
		}
	}

	return nil
}

func enderDHandler(w http.ResponseWriter, r *http.Request) {
	// Uwierzytelnienie Basic Auth
	auth := r.Header.Get("Authorization")
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	if auth != expectedAuth {
		w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Żądanie strumienia wideo od zewnętrznego serwera
	req, err := http.NewRequest("GET", streamURLenderD, nil)
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
	defer resp.Body.Close()

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

func enderDStatusHandler(w http.ResponseWriter, r *http.Request) {

	auth := r.Header.Get("Authorization")
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	if auth != expectedAuth {
		w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

    resp, err := getPrinterState("ender-d")
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Ustaw nagłówki odpowiedzi
    w.Header().Set("Content-Type", "application/json")

    // Zwróć stan drukarki jako JSON
    json.NewEncoder(w).Encode(resp)
}


func enderDView(w http.ResponseWriter, r *http.Request) {

    auth := r.Header.Get("Authorization")
    expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

    if auth != expectedAuth {
        w.Header().Set("WWW-Authenticate", `Basic realm="Login Required"`)
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    html := `
    <!DOCTYPE html>
<html lang="pl">
<head>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta charset="UTF-8">
    <title>Podgląd i Status</title>
    <style>
    body {
    margin: 0;
    font-family: Arial, sans-serif;
}

.container {
    display: flex;
    flex-direction: column;
    height: 100vh;
}

.preview {
    flex: 1;
    background: #000;
    height: 50vh; /* Zajmuje połowę wysokości ekranu */
}

.status {
    flex: 1;
    background: #f0f0f0;
    padding: 10px;
    box-sizing: border-box;
    height: 50vh; /* Zajmuje połowę wysokości ekranu */
}

.status p {
    margin: 0;
}

img {
    width: 100%;
    height: 100%;
    object-fit: contain; /* Upewnij się, że obrazek wypełnia całą dostępną przestrzeń */
}



@media (min-aspect-ratio: 1/1) {
    .container {
        flex-direction: row; /* Zmieniamy kierunek flex na poziomy, gdy szerokość > wysokość */
    }
    .preview,
    .status {
        height: 100%; /* Dopasowujemy wysokość do pełnej wysokości ekranu */
        width: 50vw;  /* Połowa szerokości ekranu, gdy układ jest poziomy */
    }
}

    </style>
</head>
<body>
    <div class="container">
        <div class="preview">
            <img src="/ender-d/cam/" width="100%" height="100%" alt="Podgląd" />
        </div>
        <div class="status">
            <p id="status">Status ładowania...</p>
            <input type="checkbox" onclick="triggerLights()">Włącz/Wyłącz światła</input>
        </div>
    </div>

    <script>
    function fetchStatus() {
        fetch('/ender-d/status/')
            .then(response => response.json())
            .then(data => {
                const completion = data['progress']['completion'];
                document.getElementById('status').innerHTML = ` + "`" + `
                    <p>Postęp drukowania: ${(completion).toFixed(2)}%</p>
                ` + "`" + `;
            })
            .catch(error => {
                document.getElementById('status').innerHTML = '<p>Błąd podczas pobierania statusu.</p>';
            });
    }

    function triggerLights() {
        // pobierz stan świateł z checkboxa
        const lightsOn = document.querySelector('input[type="checkbox"]').checked;
        if (lightsOn) {
            fetch('/lights/on/', { method: 'POST' }).catch(console.error);
        } else {
            fetch('/lights/off/', { method: 'POST' }).catch(console.error);
        }
    }

    fetchStatus();
    setInterval(fetchStatus, 5000);
    </script>

</body>
</html>
`

    w.Header().Set("Content-Type", "text/html")
    w.Write([]byte(html))
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
    defer resp.Body.Close()

    w.WriteHeader(http.StatusOK)

    return
}

func lightsOn(w http.ResponseWriter, r *http.Request) {
    lightsSet(w, r, "On")
}

func lightsOff(w http.ResponseWriter, r *http.Request) {
    lightsSet(w, r, "Off")
}
