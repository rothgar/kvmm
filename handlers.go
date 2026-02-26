package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Handlers wraps the config and provides HTTP handlers
type Handlers struct {
	config *Config
}

// NewHandlers creates a new Handlers instance
func NewHandlers(cfg *Config) *Handlers {
	return &Handlers{config: cfg}
}

// ListDevices returns all devices (GET /api/devices)
func (h *Handlers) ListDevices(w http.ResponseWriter, r *http.Request) {
	devices := h.config.GetDevices()

	// Check for thumbnail existence (explicit or auto-generated) and set the field
	for i := range devices {
		if _, exists := h.config.GetThumbnailPath(devices[i].ID); exists {
			// Set a non-empty value so frontend knows a thumbnail is available
			if devices[i].Thumbnail == "" {
				devices[i].Thumbnail = devices[i].ID + ".jpg"
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

// CreateDevice adds a new device (POST /api/devices)
func (h *Handlers) CreateDevice(w http.ResponseWriter, r *http.Request) {
	var input DeviceWithAuth
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if input.Host == "" {
		http.Error(w, "Host is required", http.StatusBadRequest)
		return
	}

	device, err := h.config.AddDevice(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(device)
}

// UpdateDevice updates an existing device (PUT /api/devices/{id})
func (h *Handlers) UpdateDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	if id == "" {
		http.Error(w, "Device ID required", http.StatusBadRequest)
		return
	}

	var input DeviceWithAuth
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if input.Host == "" {
		http.Error(w, "Host is required", http.StatusBadRequest)
		return
	}

	device, err := h.config.UpdateDevice(id, input)
	if err != nil {
		if err.Error() == "device not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)
}

// DeleteDevice removes a device (DELETE /api/devices/{id})
func (h *Handlers) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	if id == "" {
		http.Error(w, "Device ID required", http.StatusBadRequest)
		return
	}

	if err := h.config.DeleteDevice(id); err != nil {
		if err.Error() == "device not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GoToDevice redirects to the KVM device (GET /go/{id})
func (h *Handlers) GoToDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/go/")
	if id == "" {
		http.Error(w, "Device ID required", http.StatusBadRequest)
		return
	}

	device, found := h.config.GetDevice(id)
	if !found {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	// Build redirect URL
	var redirectURL string
	if device.Username != "" && device.Password != "" {
		// URL with Basic Auth credentials embedded
		redirectURL = fmt.Sprintf("http://%s:%s@%s/",
			url.QueryEscape(device.Username),
			url.QueryEscape(device.Password),
			device.Host)
	} else {
		// Simple URL without credentials
		redirectURL = fmt.Sprintf("http://%s/", device.Host)
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// DevicesHandler routes /api/devices requests
func (h *Handlers) DevicesHandler(w http.ResponseWriter, r *http.Request) {
	// Check if there's an ID in the path
	path := strings.TrimPrefix(r.URL.Path, "/api/devices")
	hasID := len(path) > 1 && path[0] == '/'

	switch r.Method {
	case http.MethodGet:
		if hasID {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.ListDevices(w, r)
	case http.MethodPost:
		if hasID {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.CreateDevice(w, r)
	case http.MethodPut:
		if !hasID {
			http.Error(w, "Device ID required", http.StatusBadRequest)
			return
		}
		h.UpdateDevice(w, r)
	case http.MethodDelete:
		if !hasID {
			http.Error(w, "Device ID required", http.StatusBadRequest)
			return
		}
		h.DeleteDevice(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ThumbnailHandler handles thumbnail operations (GET/POST/DELETE /api/devices/{id}/thumbnail)
func (h *Handlers) ThumbnailHandler(w http.ResponseWriter, r *http.Request) {
	// Extract device ID from path: /api/devices/{id}/thumbnail
	path := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	path = strings.TrimSuffix(path, "/thumbnail")
	id := path

	if id == "" {
		http.Error(w, "Device ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.UploadThumbnail(w, r, id)
	case http.MethodDelete:
		h.DeleteThumbnail(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// UploadThumbnail handles thumbnail upload (file or URL)
func (h *Handlers) UploadThumbnail(w http.ResponseWriter, r *http.Request, id string) {
	// Check if device exists
	if _, found := h.config.GetDevice(id); !found {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	contentType := r.Header.Get("Content-Type")

	// Handle JSON with URL
	if strings.HasPrefix(contentType, "application/json") {
		var input struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if input.URL == "" {
			http.Error(w, "URL is required", http.StatusBadRequest)
			return
		}

		// Fetch image from URL
		data, err := h.fetchImageFromURL(input.URL)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch image: %v", err), http.StatusBadRequest)
			return
		}

		// Process and resize thumbnail
		processed, err := ProcessThumbnail(data)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to process image: %v", err), http.StatusBadRequest)
			return
		}

		if err := h.config.SetThumbnail(id, processed, ".jpg"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// Handle multipart file upload
	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Limit upload size to 10MB (before processing)
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "File too large (max 10MB)", http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("thumbnail")
		if err != nil {
			http.Error(w, "No file provided", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Validate file type by extension
		filename := strings.ToLower(header.Filename)
		ext := ""
		if idx := strings.LastIndex(filename, "."); idx != -1 {
			ext = filename[idx:]
		}
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
			http.Error(w, "Invalid file type (allowed: jpg, png, gif, webp)", http.StatusBadRequest)
			return
		}

		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Failed to read file", http.StatusInternalServerError)
			return
		}

		// Process and resize thumbnail
		processed, err := ProcessThumbnail(data)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to process image: %v", err), http.StatusBadRequest)
			return
		}

		if err := h.config.SetThumbnail(id, processed, ".jpg"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Invalid content type", http.StatusBadRequest)
}

// fetchImageFromURL downloads an image from a URL
func (h *Handlers) fetchImageFromURL(imageURL string) ([]byte, error) {
	// Validate URL
	parsed, err := url.Parse(imageURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("only HTTP/HTTPS URLs allowed")
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(imageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	// Limit response size to 10MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Validate that the data is actually an image
	if err := ValidateImageData(data); err != nil {
		return nil, fmt.Errorf("URL did not return a valid image")
	}

	return data, nil
}

// DeleteThumbnail removes a device's thumbnail
func (h *Handlers) DeleteThumbnail(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.config.DeleteThumbnail(id); err != nil {
		if err.Error() == "device not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ServeThumbnail serves a thumbnail image (GET /thumbnails/{id})
func (h *Handlers) ServeThumbnail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/thumbnails/")
	// Remove any extension from the ID
	if idx := strings.LastIndex(id, "."); idx != -1 {
		id = id[:idx]
	}
	log.Printf("ServeThumbnail: request for device %s (path: %s)", id, r.URL.Path)

	thumbPath, found := h.config.GetThumbnailPath(id)
	if !found {
		log.Printf("ServeThumbnail: thumbnail not found for device %s", id)
		http.NotFound(w, r)
		return
	}

	log.Printf("ServeThumbnail: serving %s", thumbPath)
	http.ServeFile(w, r, thumbPath)
}

// DeviceStatus represents the reachability status of a device
type DeviceStatus struct {
	ID        string `json:"id"`
	Reachable bool   `json:"reachable"`
}

// CheckDevicesStatus returns reachability status for all devices (GET /api/status)
func (h *Handlers) CheckDevicesStatus(w http.ResponseWriter, r *http.Request) {
	devices := h.config.GetDevices()
	statuses := make([]DeviceStatus, len(devices))

	var wg sync.WaitGroup
	for i, device := range devices {
		wg.Add(1)
		go func(idx int, d Device) {
			defer wg.Done()
			statuses[idx] = DeviceStatus{
				ID:        d.ID,
				Reachable: checkHostReachable(d.Host),
			}
		}(i, device)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statuses)
}

// checkHostReachable tests if a host is reachable via HTTP or TCP
func checkHostReachable(host string) bool {
	// Add default port if not specified
	if !strings.Contains(host, ":") {
		host = host + ":80"
	}

	// Try TCP connection with short timeout
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
