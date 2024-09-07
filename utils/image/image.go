package image

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"github.com/disintegration/imaging"
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
