package login

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

type headlessQRCodeResponse struct {
	Qrcode string `json:"qrcode"`
	Token  string `json:"token"`
}

func decodeQRCodePNGBase64(encoded string) ([]byte, error) {
	trimmed := strings.TrimSpace(encoded)

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmtErrorf("decode qrcode base64: %w", err)
	}

	// 这里会先用 PNG 解码器做一次校验，确保内容确实是可用二维码图片。
	if _, err := png.Decode(bytesNewReader(decoded)); err != nil {
		return nil, fmtErrorf("decode qrcode png: %w", err)
	}

	return decoded, nil
}

func saveQRCodePNGBase64(encoded string) (string, error) {
	decoded, err := decodeQRCodePNGBase64(encoded)
	if err != nil {
		return "", err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmtErrorf("get working directory: %w", err)
	}

	path := filepath.Join(cwd, "dreamina-login-qr.png")

	if err := os.Remove(path); err != nil && !isNotExist(err) {
		return "", fmtErrorf("remove existing qrcode png: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return "", fmtErrorf("open qrcode png file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(decoded); err != nil {
		return "", fmtErrorf("write qrcode png file: %w", err)
	}

	return path, nil
}

func renderQRCodeTerminalBase64(encoded string) (string, error) {
	decoded, err := decodeQRCodePNGBase64(encoded)
	if err != nil {
		return "", err
	}
	img, err := png.Decode(bytesNewReader(decoded))
	if err != nil {
		return "", fmtErrorf("decode qrcode png: %w", err)
	}
	return renderQRCodeTerminal(img), nil
}

func renderQRCodeTerminal(img image.Image) string {
	if img == nil {
		return ""
	}
	bounds := cropQRCodeBounds(img.Bounds(), img)
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return ""
	}

	scale := maxInt(1, maxInt(width, height)/64)
	var lines []string
	for y := bounds.Min.Y; y < bounds.Max.Y; y += scale {
		var line strings.Builder
		for x := bounds.Min.X; x < bounds.Max.X; x += scale {
			if isDarkQRCodeCell(img, x, y, scale) {
				line.WriteString("##")
			} else {
				line.WriteString("  ")
			}
		}
		text := strings.TrimRight(line.String(), " ")
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func cropQRCodeBounds(bounds image.Rectangle, img image.Image) image.Rectangle {
	minX := bounds.Max.X
	minY := bounds.Max.Y
	maxX := bounds.Min.X
	maxY := bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !isDarkPixel(img, x, y) {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y+1 > maxY {
				maxY = y + 1
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return bounds
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func isDarkQRCodeCell(img image.Image, x int, y int, scale int) bool {
	if scale <= 1 {
		return isDarkPixel(img, x, y)
	}
	dark := 0
	total := 0
	for yy := y; yy < y+scale; yy++ {
		for xx := x; xx < x+scale; xx++ {
			if !image.Pt(xx, yy).In(img.Bounds()) {
				continue
			}
			total++
			if isDarkPixel(img, xx, yy) {
				dark++
			}
		}
	}
	if total == 0 {
		return false
	}
	return dark*2 >= total
}

func isDarkPixel(img image.Image, x int, y int) bool {
	if img == nil || !image.Pt(x, y).In(img.Bounds()) {
		return false
	}
	r, g, b, _ := img.At(x, y).RGBA()
	luma := (299*r + 587*g + 114*b) / 1000
	return luma < 0x8000
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func printHeadlessQRCode(termQR string, pngPath string, token string, manualImportURL string) {
	// 无头登录模式下同时输出终端二维码、PNG 保存路径和手动导入提示。
	termQR = strings.TrimSpace(termQR)
	pngPath = strings.TrimSpace(pngPath)
	token = strings.TrimSpace(token)
	manualImportURL = strings.TrimSpace(manualImportURL)
	if termQR != "" {
		fmt.Fprintf(os.Stdout, "Scan the QR code below to continue login:\n%s\n", termQR)
	}
	if pngPath != "" {
		fmt.Fprintf(os.Stdout, "QR code image saved to %s\n", pngPath)
	}
	if token != "" && manualImportURL != "" {
		fmt.Fprintf(os.Stdout, "If automatic callback does not complete, use token=%s and manually fetch the login JSON from:\n%s\n", token, manualImportURL)
	}
}
