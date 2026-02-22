package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"
)

const defaultServer = "http://localhost:8080"

// CLIDevice represents a device from the API
type CLIDevice struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Alias     string `json:"alias"`
	Username  string `json:"username"`
	Thumbnail string `json:"thumbnail"`
}

// CLIDeviceStatus represents device status from the API
type CLIDeviceStatus struct {
	ID        string `json:"id"`
	Reachable bool   `json:"reachable"`
}

func getServer() string {
	// Priority: environment variable > config file > default
	if server := os.Getenv("KVMM_SERVER"); server != "" {
		return server
	}

	if server := readConfigFile(); server != "" {
		return server
	}

	return defaultServer
}

func readConfigFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	configPath := filepath.Join(home, ".config", "kvmm.conf")
	file, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse key=value or just value
		if strings.HasPrefix(line, "server=") || strings.HasPrefix(line, "server =") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
		// If no key, treat first non-comment line as server URL
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line
		}
	}

	return ""
}

func printCLIUsage() {
	fmt.Println(`KVMM - KVM Manager

Usage:
  kvmm                  List all devices (alias for 'kvmm list')
  kvmm list             List all devices with status
  kvmm <alias>          Open device by alias or hostname
  kvmm server           Start the web server
  kvmm help             Show this help

Server Options:
  kvmm server -config <path>    Config file (default: config.toml)
  kvmm server -port <port>      Override port from config

Configuration:
  ~/.config/kvmm.conf   Client config file (server URL)
  KVMM_SERVER           Environment variable (overrides config file)

Config file format (~/.config/kvmm.conf):
  server = http://192.168.1.50:8080

Examples:
  kvmm list
  kvmm "Server Room"
  kvmm 192.168.1.100
  kvmm server -config /etc/kvmm/config.toml`)
}

func runList() {
	server := getServer()
	devices, err := fetchDevices(server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	statuses, _ := fetchStatuses(server)
	statusMap := make(map[string]bool)
	for _, s := range statuses {
		statusMap[s.ID] = s.Reachable
	}

	if len(devices) == 0 {
		fmt.Println("No devices configured")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tALIAS\tHOST\tAUTH")
	fmt.Fprintln(w, "------\t-----\t----\t----")

	for _, d := range devices {
		status := "?"
		if reachable, ok := statusMap[d.ID]; ok {
			if reachable {
				status = "●"
			} else {
				status = "○"
			}
		}

		alias := d.Alias
		if alias == "" {
			alias = "-"
		}

		auth := "no"
		if d.Username != "" {
			auth = "yes"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", status, alias, d.Host, auth)
	}
	w.Flush()

	fmt.Println()
	fmt.Println("● = online, ○ = offline")
}

func runOpen(query string) {
	server := getServer()
	devices, err := fetchDevices(server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	query = strings.ToLower(query)

	// First try exact match (case insensitive)
	for i, d := range devices {
		if strings.ToLower(d.Alias) == query || strings.ToLower(d.Host) == query {
			openDeviceInBrowser(server, &devices[i])
			return
		}
	}

	// Collect all partial matches
	var matches []CLIDevice
	for _, d := range devices {
		if strings.Contains(strings.ToLower(d.Alias), query) ||
			strings.Contains(strings.ToLower(d.Host), query) {
			matches = append(matches, d)
		}
	}

	switch len(matches) {
	case 0:
		fmt.Fprintf(os.Stderr, "No device found matching: %s\n", query)
		fmt.Fprintln(os.Stderr, "Use 'kvmm list' to see available devices")
		os.Exit(1)
	case 1:
		openDeviceInBrowser(server, &matches[0])
	default:
		fmt.Fprintf(os.Stderr, "Multiple devices match '%s':\n\n", query)
		w := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ALIAS\tHOST")
		fmt.Fprintln(w, "-----\t----")
		for _, d := range matches {
			alias := d.Alias
			if alias == "" {
				alias = "-"
			}
			fmt.Fprintf(w, "%s\t%s\n", alias, d.Host)
		}
		w.Flush()
		fmt.Fprintln(os.Stderr, "\nBe more specific.")
		os.Exit(1)
	}
}

func openDeviceInBrowser(server string, device *CLIDevice) {
	url := fmt.Sprintf("%s/go/%s", server, device.ID)
	name := device.Alias
	if name == "" {
		name = device.Host
	}

	fmt.Printf("Opening %s (%s)...\n", name, device.Host)

	if err := openBrowser(url); err != nil {
		fmt.Printf("Open this URL in your browser: %s\n", url)
	}
}

func fetchDevices(server string) ([]CLIDevice, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(server + "/api/devices")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var devices []CLIDevice
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return devices, nil
}

func fetchStatuses(server string) ([]CLIDeviceStatus, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(server + "/api/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var statuses []CLIDeviceStatus
	json.NewDecoder(resp.Body).Decode(&statuses)
	return statuses, nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux, freebsd, etc.
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}
