package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/google/uuid"
)

// Device represents a KVM device configuration
type Device struct {
	ID        string `toml:"id" json:"id"`
	Host      string `toml:"host" json:"host"`
	Alias     string `toml:"alias,omitempty" json:"alias,omitempty"`
	Username  string `toml:"username,omitempty" json:"username,omitempty"`
	Password  string `toml:"password,omitempty" json:"-"` // Hidden from JSON output
	Thumbnail string `toml:"thumbnail,omitempty" json:"thumbnail,omitempty"`
}

// DeviceWithAuth is used for creating/updating devices (includes password in JSON)
type DeviceWithAuth struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Alias    string `json:"alias,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	Port       int    `toml:"port"`
	ConfigFile string `toml:"config_file"`
}

// Config represents the complete application configuration
type Config struct {
	Server  ServerConfig `toml:"server"`
	Devices []Device     `toml:"devices"`

	mu       sync.RWMutex
	filePath string
}

// LoadConfig reads configuration from a TOML file
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:       8080,
			ConfigFile: path,
		},
		filePath: path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create default config if file doesn't exist
			return cfg, cfg.Save()
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.filePath = path

	// Ensure all devices have IDs
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == "" {
			cfg.Devices[i].ID = uuid.New().String()
		}
	}

	// Generate pattern thumbnails for devices without thumbnails
	cfg.GenerateMissingThumbnails()

	return cfg, nil
}

// GenerateMissingThumbnails creates pattern thumbnails for devices that don't have one
func (c *Config) GenerateMissingThumbnails() {
	for _, device := range c.Devices {
		if device.Thumbnail == "" {
			seed := device.ID + device.Host + device.Alias
			if pattern, err := GeneratePatternThumbnail(seed); err == nil {
				c.SetThumbnail(device.ID, pattern, ".jpg")
			}
		}
	}
}

// Save writes the configuration to the TOML file atomically
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Write to temporary file first
	tmpFile := c.filePath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("creating temp config file: %w", err)
	}

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(c); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("closing temp config file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, c.filePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("renaming config file: %w", err)
	}

	return nil
}

// GetDevices returns a copy of all devices
func (c *Config) GetDevices() []Device {
	c.mu.RLock()
	defer c.mu.RUnlock()

	devices := make([]Device, len(c.Devices))
	copy(devices, c.Devices)
	return devices
}

// GetDevice returns a device by ID
func (c *Config) GetDevice(id string) (Device, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, d := range c.Devices {
		if d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}

// AddDevice adds a new device and saves the config
func (c *Config) AddDevice(d DeviceWithAuth) (Device, error) {
	c.mu.Lock()
	device := Device{
		ID:       uuid.New().String(),
		Host:     d.Host,
		Alias:    d.Alias,
		Username: d.Username,
		Password: d.Password,
	}
	c.Devices = append(c.Devices, device)
	c.mu.Unlock()

	if err := c.Save(); err != nil {
		// Rollback
		c.mu.Lock()
		c.Devices = c.Devices[:len(c.Devices)-1]
		c.mu.Unlock()
		return Device{}, err
	}

	// Generate a pattern thumbnail for the new device
	seed := device.ID + device.Host + device.Alias
	if pattern, err := GeneratePatternThumbnail(seed); err == nil {
		c.SetThumbnail(device.ID, pattern, ".jpg")
		// Re-fetch device to get updated thumbnail field
		if updated, found := c.GetDevice(device.ID); found {
			device = updated
		}
	}

	return device, nil
}

// UpdateDevice updates an existing device and saves the config
func (c *Config) UpdateDevice(id string, d DeviceWithAuth) (Device, error) {
	c.mu.Lock()
	var oldDevice Device
	var found bool
	var idx int

	for i, dev := range c.Devices {
		if dev.ID == id {
			oldDevice = dev
			found = true
			idx = i
			break
		}
	}

	if !found {
		c.mu.Unlock()
		return Device{}, fmt.Errorf("device not found")
	}

	updated := Device{
		ID:        id,
		Host:      d.Host,
		Alias:     d.Alias,
		Username:  d.Username,
		Password:  d.Password,
		Thumbnail: oldDevice.Thumbnail, // Preserve existing thumbnail
	}
	c.Devices[idx] = updated
	c.mu.Unlock()

	if err := c.Save(); err != nil {
		// Rollback
		c.mu.Lock()
		c.Devices[idx] = oldDevice
		c.mu.Unlock()
		return Device{}, err
	}

	return updated, nil
}

// DeleteDevice removes a device and saves the config
func (c *Config) DeleteDevice(id string) error {
	c.mu.Lock()
	var oldDevices []Device
	var found bool

	for i, d := range c.Devices {
		if d.ID == id {
			oldDevices = make([]Device, len(c.Devices))
			copy(oldDevices, c.Devices)
			c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		c.mu.Unlock()
		return fmt.Errorf("device not found")
	}
	c.mu.Unlock()

	if err := c.Save(); err != nil {
		// Rollback
		c.mu.Lock()
		c.Devices = oldDevices
		c.mu.Unlock()
		return err
	}

	return nil
}

// GetConfigDir returns the directory containing the config file
func (c *Config) GetConfigDir() string {
	return filepath.Dir(c.filePath)
}

// GetThumbnailDir returns the path to the thumbnails directory
func (c *Config) GetThumbnailDir() string {
	return filepath.Join(c.GetConfigDir(), "thumbnails")
}

// EnsureThumbnailDir creates the thumbnails directory if it doesn't exist
func (c *Config) EnsureThumbnailDir() error {
	return os.MkdirAll(c.GetThumbnailDir(), 0755)
}

// SetThumbnail saves a thumbnail for a device and updates config
func (c *Config) SetThumbnail(id string, data []byte, ext string) error {
	if err := c.EnsureThumbnailDir(); err != nil {
		return fmt.Errorf("creating thumbnail dir: %w", err)
	}

	c.mu.Lock()
	var idx int = -1
	for i, d := range c.Devices {
		if d.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		c.mu.Unlock()
		return fmt.Errorf("device not found")
	}

	// Delete old thumbnail if exists
	oldThumb := c.Devices[idx].Thumbnail
	if oldThumb != "" {
		os.Remove(filepath.Join(c.GetThumbnailDir(), oldThumb))
	}

	// Save new thumbnail
	filename := id + ext
	thumbPath := filepath.Join(c.GetThumbnailDir(), filename)
	if err := os.WriteFile(thumbPath, data, 0644); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("saving thumbnail: %w", err)
	}

	c.Devices[idx].Thumbnail = filename
	c.mu.Unlock()

	return c.Save()
}

// DeleteThumbnail removes a device's thumbnail
func (c *Config) DeleteThumbnail(id string) error {
	c.mu.Lock()
	var idx int = -1
	for i, d := range c.Devices {
		if d.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		c.mu.Unlock()
		return fmt.Errorf("device not found")
	}

	if c.Devices[idx].Thumbnail != "" {
		os.Remove(filepath.Join(c.GetThumbnailDir(), c.Devices[idx].Thumbnail))
		c.Devices[idx].Thumbnail = ""
	}
	c.mu.Unlock()

	return c.Save()
}

// GetThumbnailPath returns the full path to a device's thumbnail
func (c *Config) GetThumbnailPath(id string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, d := range c.Devices {
		if d.ID == id && d.Thumbnail != "" {
			return filepath.Join(c.GetThumbnailDir(), d.Thumbnail), true
		}
	}
	return "", false
}
