// Package location 提供基于本地QQWry.dat数据库的IP地理位置查询功能
//
// 数据库文件下载：
// 1. 纯真网络官方：http://www.cz88.net/ip/ 或 http://update.cz88.net/ip/qqwry.rar
// 2. GitHub自动更新：https://github.com/metowolf/qqwry.dat
// 3. 其他来源：https://github.com/lionsoul2014/ip2region
//
// 注意：纯真网络计划从2024年10月起停止维护QQWry.dat格式，建议关注官方公告
//
// 使用示例：
//
//	err := location.Init("qqwry.dat")  // 初始化数据库
//	info, err := location.GetLocationByIP("114.114.114.114")
//	fmt.Printf("省份: %s, 城市: %s\n", info.Province, info.City)
package location

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
)

// 注意：需要导入以下包进行GBK到UTF-8转换
// 如果vendor目录中没有，请运行: go mod vendor
// import (
// 	"golang.org/x/text/encoding/simplifiedchinese"
// 	"golang.org/x/text/transform"
// )

var (
	dbPath      string = "qqwry.dat" // 默认数据库文件路径
	dbFile      *os.File
	dbMutex     sync.RWMutex
	initialized bool
)

// LocationInfo 地理位置信息
type LocationInfo struct {
	Country  string `json:"country"`  // 国家
	Province string `json:"province"` // 省份
	City     string `json:"city"`     // 城市
	District string `json:"district"` // 区县
	ISP      string `json:"isp"`      // 运营商
	IP       string `json:"ip"`       // IP地址
}

// Init 初始化IP数据库
// dbFilePath: QQWry.dat数据库文件路径，如果为空则使用默认路径 "qqwry.dat"
func Init(dbFilePath string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if initialized && dbFile != nil {
		return nil
	}

	if dbFilePath == "" {
		dbFilePath = dbPath
	} else {
		dbPath = dbFilePath
	}

	file, err := os.OpenFile(dbFilePath, os.O_RDONLY, 0444)
	if err != nil {
		return fmt.Errorf("无法打开IP数据库文件 %s: %w\n提示: 请确保文件存在且路径正确", dbFilePath, err)
	}

	if dbFile != nil {
		dbFile.Close()
	}
	dbFile = file
	initialized = true

	// 验证文件是否真的打开成功
	if dbFile == nil {
		return fmt.Errorf("数据库文件打开失败，dbFile为nil")
	}

	return nil
}

// Close 关闭数据库文件
func Close() error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if dbFile != nil {
		err := dbFile.Close()
		dbFile = nil
		initialized = false
		return err
	}
	return nil
}

// SetDBPath 设置数据库文件路径
func SetDBPath(path string) {
	dbPath = path
}

// ipToLong 将IP地址转换为长整型
func ipToLong(ip string) (uint32, error) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0, fmt.Errorf("无效的IP地址格式: %s", ip)
	}

	var result uint32
	for i, part := range parts {
		var num uint32
		_, err := fmt.Sscanf(part, "%d", &num)
		if err != nil || num > 255 {
			return 0, fmt.Errorf("无效的IP地址: %s", ip)
		}
		result |= num << ((3 - i) * 8)
	}
	return result, nil
}

// readUint32 从文件中读取uint32（小端序）
func readUint32(offset int64) (uint32, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	if dbFile == nil {
		return 0, fmt.Errorf("数据库未初始化，请先调用 Init()")
	}

	buf := make([]byte, 4)
	_, err := dbFile.ReadAt(buf, offset)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf), nil
}

// readUint24 从文件中读取24位整数（小端序）
func readUint24(offset int64) (uint32, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	if dbFile == nil {
		return 0, fmt.Errorf("数据库未初始化，请先调用 Init()")
	}

	buf := make([]byte, 3)
	_, err := dbFile.ReadAt(buf, offset)
	if err != nil {
		return 0, err
	}
	return uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16, nil
}

// readString 从文件中读取字符串（GBK编码）
func readString(offset int64) (string, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	if dbFile == nil {
		return "", fmt.Errorf("数据库未初始化，请先调用 Init()")
	}

	var result []byte
	buf := make([]byte, 1)
	currentOffset := offset

	for {
		_, err := dbFile.ReadAt(buf, currentOffset)
		if err != nil {
			return "", err
		}
		if buf[0] == 0 {
			break
		}
		result = append(result, buf[0])
		currentOffset++
	}

	// 转换GBK到UTF-8
	utf8Str, err := gbkToUtf8(result)
	if err != nil {
		// 如果转换失败，返回错误（不返回乱码数据）
		// 用户需要按照错误提示更新vendor目录
		return "", fmt.Errorf("读取地理位置信息失败: %w\n提示: 请运行 'go mod vendor' 更新vendor目录以支持GBK编码转换", err)
	}
	return utf8Str, nil
}

// gbkToUtf8 GBK编码转UTF-8编码
//
// 重要提示：此函数需要 golang.org/x/text/encoding/simplifiedchinese 包
//
// 如果出现乱码，请按以下步骤修复：
// 1. 在项目根目录运行: go mod vendor
// 2. 取消上面 import 注释中的编码包导入
// 3. 将下面的注释代码取消注释，替换当前实现
func gbkToUtf8(gbk []byte) (string, error) {
	// 正确的实现（取消注释后使用）：
	// decoder := simplifiedchinese.GBK.NewDecoder()
	// utf8Bytes, _, err := transform.Bytes(decoder, gbk)
	// if err != nil {
	// 	return "", fmt.Errorf("GBK转UTF-8失败: %w", err)
	// }
	// return string(utf8Bytes), nil

	// 临时实现：返回错误提示
	return "", fmt.Errorf("GBK转UTF-8需要编码库支持\n" +
		"修复步骤：\n" +
		"1. 运行: go mod vendor\n" +
		"2. 取消 location.go 中 import 和 gbkToUtf8 函数的注释代码\n" +
		"3. 详细说明请查看: vendor/github.com/lijianjunljj/gocommon/utils/location/FIX_GBK_ENCODING.md")
}

// getRecordOffset 获取记录偏移量
func getRecordOffset(ipLong uint32) (int64, error) {
	// 读取索引区起始位置
	indexStart, err := readUint32(0)
	if err != nil {
		return 0, err
	}

	// 读取索引区结束位置
	indexEnd, err := readUint32(4)
	if err != nil {
		return 0, err
	}

	// 二分查找
	low := int64(indexStart)
	high := int64(indexEnd)
	var mid int64
	var midIP uint32

	for low <= high {
		mid = low + (high-low)/7*7 // 每条索引7字节
		if mid%7 != 0 {
			mid = mid - (mid % 7)
		}

		midIP, err = readUint32(mid)
		if err != nil {
			return 0, err
		}

		if ipLong < midIP {
			high = mid - 7
		} else {
			// 读取下一条记录的IP
			nextIP, err := readUint32(mid + 7)
			if err != nil {
				return 0, err
			}
			if ipLong < nextIP {
				// 找到匹配的记录
				recordOffset, err := readUint24(mid + 4)
				if err != nil {
					return 0, err
				}
				return int64(recordOffset), nil
			}
			low = mid + 7
		}
	}

	return 0, fmt.Errorf("未找到IP记录")
}

// parseLocation 解析地理位置信息
func parseLocation(offset int64) (string, string, error) {
	// 读取模式字节
	mode, err := readUint24(offset)
	if err != nil {
		return "", "", err
	}

	var countryOffset, areaOffset int64

	if mode == 0x01 || mode == 0x02 {
		// 重定向模式
		redirectOffset, err := readUint24(offset + 1)
		if err != nil {
			return "", "", err
		}

		if mode == 0x02 {
			// 国家信息重定向
			countryOffset = int64(redirectOffset) + 4
		} else {
			countryOffset = int64(redirectOffset)
		}

		// 读取地区信息
		areaMode, err := readUint24(countryOffset)
		if err != nil {
			return "", "", err
		}

		if areaMode == 0x02 {
			// 地区信息重定向
			areaRedirect, err := readUint24(countryOffset + 1)
			if err != nil {
				return "", "", err
			}
			areaOffset = int64(areaRedirect) + 4
		} else {
			areaOffset = countryOffset + 1
		}
	} else {
		// 直接模式
		countryOffset = offset
		areaOffset = offset + 1
		for {
			buf := make([]byte, 1)
			dbMutex.RLock()
			if dbFile == nil {
				dbMutex.RUnlock()
				return "", "", fmt.Errorf("数据库未初始化，请先调用 Init()")
			}
			_, err := dbFile.ReadAt(buf, areaOffset)
			dbMutex.RUnlock()
			if err != nil {
				return "", "", err
			}
			if buf[0] == 0 {
				break
			}
			areaOffset++
		}
		areaOffset++
	}

	// 读取国家信息
	country, err := readString(countryOffset)
	if err != nil {
		return "", "", err
	}

	// 读取地区信息
	area, err := readString(areaOffset)
	if err != nil {
		area = ""
	}

	return country, area, nil
}

// parseProvinceCity 解析省份和城市
func parseProvinceCity(location string) (province, city, district string) {
	location = strings.TrimSpace(location)
	if location == "" {
		return "", "", ""
	}

	// 移除常见的国家前缀
	location = strings.TrimPrefix(location, "CZ88.NET")
	location = strings.TrimPrefix(location, "IANA")
	location = strings.TrimSpace(location)

	// 常见的地址格式：省份 城市 区县 或 省份市 或 城市
	// 尝试解析省市区
	parts := strings.Fields(location)
	if len(parts) == 0 {
		return "", "", ""
	}

	// 省份关键词
	provinceKeywords := []string{"省", "自治区", "特别行政区", "市", "自治区"}
	cityKeywords := []string{"市", "县", "区", "自治州", "盟"}

	province = ""
	city = ""
	district = ""

	// 查找省份
	for i, part := range parts {
		for _, keyword := range provinceKeywords {
			if strings.HasSuffix(part, keyword) {
				province = strings.Join(parts[:i+1], "")
				if i+1 < len(parts) {
					remaining := strings.Join(parts[i+1:], "")
					// 继续查找城市
					for j, cityPart := range parts[i+1:] {
						for _, cityKeyword := range cityKeywords {
							if strings.HasSuffix(cityPart, cityKeyword) {
								city = strings.Join(parts[i+1:i+1+j+1], "")
								if i+1+j+1 < len(parts) {
									district = strings.Join(parts[i+1+j+1:], "")
								}
								return province, city, district
							}
						}
					}
					// 如果没有找到城市关键词，将剩余部分作为城市
					city = remaining
				}
				return province, city, district
			}
		}
	}

	// 如果没有找到省份关键词，尝试其他解析方式
	if len(parts) >= 2 {
		// 假设第一部分是省份，第二部分是城市
		province = parts[0]
		city = parts[1]
		if len(parts) > 2 {
			district = strings.Join(parts[2:], "")
		}
	} else if len(parts) == 1 {
		// 只有一个部分，可能是城市或省份
		if strings.Contains(parts[0], "市") {
			city = parts[0]
		} else {
			province = parts[0]
		}
	}

	return province, city, district
}

// GetLocationByIP 通过IP获取省市区县信息
// 使用本地QQWry.dat数据库
func GetLocationByIP(ip string) (*LocationInfo, error) {
	if ip == "" {
		return nil, fmt.Errorf("IP地址不能为空")
	}

	// 去除端口号（如果有）
	if idx := strings.Index(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}

	// 本地IP地址
	if ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "172.") {
		return &LocationInfo{
			Country:  "中国",
			Province: "本地",
			City:     "本地",
			District: "本地",
			ISP:      "内网",
			IP:       ip,
		}, nil
	}

	// 确保数据库已初始化
	dbMutex.RLock()
	isInit := initialized && dbFile != nil
	dbMutex.RUnlock()

	if !isInit {
		if err := Init(""); err != nil {
			return nil, fmt.Errorf("IP数据库未初始化: %w", err)
		}
		// 再次检查，确保初始化成功
		dbMutex.RLock()
		if dbFile == nil {
			dbMutex.RUnlock()
			return nil, fmt.Errorf("IP数据库初始化失败，dbFile为nil")
		}
		dbMutex.RUnlock()
	}

	// 转换IP为长整型
	ipLong, err := ipToLong(ip)
	if err != nil {
		return nil, err
	}

	// 获取记录偏移量
	recordOffset, err := getRecordOffset(ipLong)
	if err != nil {
		return nil, err
	}

	// 解析地理位置
	country, area, err := parseLocation(recordOffset)
	if err != nil {
		return nil, err
	}

	// 解析省市区
	province, city, district := parseProvinceCity(country)

	// 如果省份为空，使用country作为省份
	if province == "" {
		province = country
	}

	// 如果国家信息包含"中国"，设置为中国
	countryName := "中国"
	if !strings.Contains(country, "中国") && !strings.Contains(country, "CN") {
		// 尝试从country中提取国家名
		if strings.Contains(country, "省") || strings.Contains(country, "市") {
			countryName = "中国"
		} else {
			countryName = country
		}
	}

	return &LocationInfo{
		Country:  countryName,
		Province: province,
		City:     city,
		District: district,
		ISP:      area,
		IP:       ip,
	}, nil
}

// GetProvinceByIP 通过IP获取省份
func GetProvinceByIP(ip string) (string, error) {
	info, err := GetLocationByIP(ip)
	if err != nil {
		return "", err
	}
	return info.Province, nil
}

// GetCityByIP 通过IP获取城市
func GetCityByIP(ip string) (string, error) {
	info, err := GetLocationByIP(ip)
	if err != nil {
		return "", err
	}
	return info.City, nil
}

// GetDistrictByIP 通过IP获取区县
func GetDistrictByIP(ip string) (string, error) {
	info, err := GetLocationByIP(ip)
	if err != nil {
		return "", err
	}
	return info.District, nil
}

// GetFullLocationByIP 通过IP获取完整地理位置字符串
// 格式：国家 省份 城市 区县
func GetFullLocationByIP(ip string) (string, error) {
	info, err := GetLocationByIP(ip)
	if err != nil {
		return "", err
	}

	parts := []string{}
	if info.Country != "" {
		parts = append(parts, info.Country)
	}
	if info.Province != "" {
		parts = append(parts, info.Province)
	}
	if info.City != "" {
		parts = append(parts, info.City)
	}
	if info.District != "" {
		parts = append(parts, info.District)
	}

	if len(parts) == 0 {
		return "未知", nil
	}

	return strings.Join(parts, " "), nil
}
