package image

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"github.com/disintegration/imaging"
	"github.com/nfnt/resize"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/url"
	"strings"
)

func Base64Decode(data string) (error, []byte) {
	dist, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return err, nil
	}
	return nil, dist
}

func Base64Encode(src []byte) (error, string) {
	dist := base64.StdEncoding.EncodeToString(src)
	return nil, dist
}

func ImageRescale(scale float32, inputFile, outputFile string) {

	// 打开原始图片
	src, err := imaging.Open(inputFile)
	if err != nil {
		log.Fatalf("Open input file failed: %v", err)
	}

	// 设置缩放比例，例如缩小到原来的一半
	width := float32(src.Bounds().Dx()) * scale
	height := float32(src.Bounds().Dy()) * scale

	// 缩放图片
	dst := imaging.Resize(src, int(width), int(height), imaging.Lanczos)

	// 保存缩放后的图片
	err = imaging.Save(dst, outputFile)
	if err != nil {
		log.Fatalf("Save output file failed: %v", err)
	}
}
func CheckImageFormat(data []byte) string {
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8 {
		return "JPEG"
	}
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e && data[3] == 0x47 &&
		data[4] == 0x0d && data[5] == 0x0a && data[6] == 0x1a && data[7] == 0x0a {
		return "PNG"
	}
	if len(data) >= 4 && data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return "GIF"
	}
	return "Unknown"
}
func ImageResizeBytes(data []byte, width, height int) []byte {
	var img image.Image
	var err error
	// 判断图像格式并解码
	imageFormat := CheckImageFormat(data)
	if imageFormat == "JPEG" {
		img, err = jpeg.Decode(bytes.NewReader(data))
	} else if imageFormat == "PNG" {
		img, err = png.Decode(bytes.NewReader(data))
	} else {
		log.Fatal("Unsupported image format")
	}
	if err != nil {
		log.Fatal(err)
	}

	// 调整图像尺寸
	resized := resize.Resize(uint(width), uint(height), img, resize.Lanczos3)

	// 将调整后的图像编码回字节切片
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 100})
	if err != nil {
		log.Fatal(err)
	}

	return buf.Bytes()
}

func ImageResize(width, height int, inputFile, outputFile string) {

	// 打开原始图片
	src, err := imaging.Open(inputFile)
	if err != nil {
		log.Fatalf("Open input file failed: %v", err)
	}

	// 缩放图片
	dst := imaging.Resize(src, int(width), int(height), imaging.Lanczos)

	// 保存缩放后的图片
	err = imaging.Save(dst, outputFile)
	if err != nil {
		log.Fatalf("Save output file failed: %v", err)
	}
}

func GetImageNameFromURL(u string) string {
	parsedURL, err := url.Parse(u)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return ""
	}

	path := parsedURL.Path
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.Contains(parts[i], ".") && (strings.HasSuffix(parts[i], ".jpg") || strings.HasSuffix(parts[i], ".png") || strings.HasSuffix(parts[i], ".gif") || strings.HasSuffix(parts[i], ".jpeg") || strings.HasSuffix(parts[i], ".bmp")) {
			return parts[i]
		}
	}
	return ""
}

func GenHash(byteArray []byte) string {

	reader := bytes.NewReader(byteArray)

	// 创建一个新的 sha256 哈希对象
	hash := md5.New()

	// 将文件内容写入哈希对象
	if _, err := io.Copy(hash, reader); err != nil {
		fmt.Printf("Error hashing file: %v", err)
		return ""
	}

	// 获取哈希值
	hashValue := hash.Sum(nil)

	return string(hashValue)
}
