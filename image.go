package main

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/gif" // Register gif decoder
	"image/jpeg"
	_ "image/png" // Register png decoder
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Register webp decoder
)

const (
	maxThumbnailWidth  = 400
	maxThumbnailHeight = 300
	jpegQuality        = 85
)

// ProcessThumbnail decodes, resizes, and re-encodes an image as JPEG
func ProcessThumbnail(data []byte) ([]byte, error) {
	// Decode the image
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	_ = format // We know the format but will output as JPEG

	// Get original dimensions
	bounds := img.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()

	// Calculate new dimensions maintaining aspect ratio
	newWidth, newHeight := calculateDimensions(origWidth, origHeight, maxThumbnailWidth, maxThumbnailHeight)

	// Only resize if the image is larger than the max dimensions
	var resized image.Image
	if newWidth < origWidth || newHeight < origHeight {
		// Create a new RGBA image for the resized result
		dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

		// Use high-quality resampling
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
		resized = dst
	} else {
		resized = img
	}

	// Encode as JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("failed to encode thumbnail: %w", err)
	}

	return buf.Bytes(), nil
}

// calculateDimensions calculates new dimensions maintaining aspect ratio
func calculateDimensions(origWidth, origHeight, maxWidth, maxHeight int) (int, int) {
	if origWidth <= maxWidth && origHeight <= maxHeight {
		return origWidth, origHeight
	}

	// Calculate scale factors for both dimensions
	widthRatio := float64(maxWidth) / float64(origWidth)
	heightRatio := float64(maxHeight) / float64(origHeight)

	// Use the smaller ratio to ensure the image fits within bounds
	ratio := widthRatio
	if heightRatio < widthRatio {
		ratio = heightRatio
	}

	newWidth := int(float64(origWidth) * ratio)
	newHeight := int(float64(origHeight) * ratio)

	// Ensure minimum dimensions of 1
	if newWidth < 1 {
		newWidth = 1
	}
	if newHeight < 1 {
		newHeight = 1
	}

	return newWidth, newHeight
}

// ValidateImageData checks if the data is a valid image
func ValidateImageData(data []byte) error {
	_, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid image data: %w", err)
	}
	return nil
}

// GeneratePatternThumbnail creates a unique geometric pattern based on the seed string
func GeneratePatternThumbnail(seed string) ([]byte, error) {
	const width = 400
	const height = 300

	// Generate hash from seed for deterministic colors
	h := fnv.New64a()
	h.Write([]byte(seed))
	hash := h.Sum64()

	// Generate colors from hash
	hue1 := float64(hash%360) / 360.0
	hue2 := float64((hash/360)%360) / 360.0
	hue3 := float64((hash/129600)%360) / 360.0

	color1 := hslToRGB(hue1, 0.6, 0.4)
	color2 := hslToRGB(hue2, 0.5, 0.5)
	color3 := hslToRGB(hue3, 0.7, 0.6)

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Pattern type based on hash
	patternType := hash % 4

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var c color.RGBA

			switch patternType {
			case 0: // Diagonal stripes
				stripe := (x + y) / 20
				if stripe%3 == 0 {
					c = color1
				} else if stripe%3 == 1 {
					c = color2
				} else {
					c = color3
				}
			case 1: // Circles
				cx, cy := width/2, height/2
				dx, dy := float64(x-cx), float64(y-cy)
				dist := math.Sqrt(dx*dx + dy*dy)
				ring := int(dist) / 30
				if ring%3 == 0 {
					c = color1
				} else if ring%3 == 1 {
					c = color2
				} else {
					c = color3
				}
			case 2: // Grid
				cellX, cellY := x/40, y/40
				if (cellX+cellY)%2 == 0 {
					c = color1
				} else if (cellX*cellY)%3 == 0 {
					c = color2
				} else {
					c = color3
				}
			case 3: // Waves
				wave := math.Sin(float64(x)/30.0+float64(hash%100)) * 20
				yOffset := float64(y) - float64(height)/2 + wave
				band := int(math.Abs(yOffset)) / 25
				if band%3 == 0 {
					c = color1
				} else if band%3 == 1 {
					c = color2
				} else {
					c = color3
				}
			}

			img.SetRGBA(x, y, c)
		}
	}

	// Encode as JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("failed to encode pattern: %w", err)
	}

	return buf.Bytes(), nil
}

// hslToRGB converts HSL color to RGB
func hslToRGB(h, s, l float64) color.RGBA {
	var r, g, b float64

	if s == 0 {
		r, g, b = l, l, l
	} else {
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q
		r = hueToRGB(p, q, h+1.0/3.0)
		g = hueToRGB(p, q, h)
		b = hueToRGB(p, q, h-1.0/3.0)
	}

	return color.RGBA{
		R: uint8(r * 255),
		G: uint8(g * 255),
		B: uint8(b * 255),
		A: 255,
	}
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	if t < 1.0/6.0 {
		return p + (q-p)*6*t
	}
	if t < 1.0/2.0 {
		return q
	}
	if t < 2.0/3.0 {
		return p + (q-p)*(2.0/3.0-t)*6
	}
	return p
}

