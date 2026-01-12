package location

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strings"

	zh "golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// IpLocationInfo IP位置信息
type IpLocationInfo struct {
	OK       bool   // 是否查询成功
	IP       string // 查询的IP地址
	AreaName string // 省份名称
	AreaDesc string // 地区描述
	OpName   string // 运营商名称
	OpDesc   string // 运营商描述
}

// QQWryQuery QQWry查询器
type QQWryQuery struct {
	file       *bytes.Reader
	indexStart int64
	indexEnd   int64
}

// NewQQWryQuery 创建新的QQWry查询器
func NewQQWryQuery(dbfile string) (*QQWryQuery, error) {
	_file, err := os.Open(dbfile)
	if err != nil {
		return nil, fmt.Errorf("无法打开数据库文件: %w", err)
	}
	defer _file.Close()

	fileInfo, err := _file.Stat()
	if err != nil {
		return nil, fmt.Errorf("无法获取文件信息: %w", err)
	}

	buf := make([]byte, fileInfo.Size())
	_, err = _file.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	file := bytes.NewReader(buf)

	// 读取索引区起始和结束位置
	file.ReadAt(buf[0:8], 0)
	indexStart := int64(binary.LittleEndian.Uint32(buf[0:4]))
	indexEnd := int64(binary.LittleEndian.Uint32(buf[4:8]))

	return &QQWryQuery{
		file:       file,
		indexStart: indexStart,
		indexEnd:   indexEnd,
	}, nil
}

// QueryIP 查询IP地址的位置信息
func (q *QQWryQuery) QueryIP(ipStr string) (*IpLocationInfo, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("无效的IP地址: %s", ipStr)
	}

	ip4 := ip.To4()
	if len(ip4) != 4 {
		return nil, fmt.Errorf("不是IPv4地址: %s", ipStr)
	}

	// 二分法查找
	maxLoop := int64(32)
	head := q.indexStart
	tail := q.indexEnd
	got := false
	rpos := int64(0)
	buf := make([]byte, 1024)

	for ; maxLoop >= 0; maxLoop-- {
		idxNum := (tail - head) / 7
		pos := head + int64(idxNum/2)*7

		q.file.ReadAt(buf[0:7], pos)

		// startIP
		_ip := binary.LittleEndian.Uint32(buf[0:4])

		// 记录位置
		_buf := append(buf[4:7], 0x0) // 3byte + 1byte(0x00)
		rpos = int64(binary.LittleEndian.Uint32(_buf))

		q.file.ReadAt(buf[0:4], rpos)

		_ip2 := binary.LittleEndian.Uint32(buf[0:4])

		// 查询的ip被转成大端了
		_ipq := binary.BigEndian.Uint32(ip4)

		if _ipq > _ip2 {
			head = pos
			continue
		}

		if _ipq < _ip {
			tail = pos
			continue
		}

		// 找到了
		got = true
		break
	}

	loc := &IpLocationInfo{
		OK:       false,
		IP:       ipStr,
		AreaName: "",
		AreaDesc: "",
		OpDesc:   "",
		OpName:   "",
	}

	if !got {
		return loc, nil
	}

	_loc := q.getIpLocation(rpos)

	// 转换GBK到UTF-8
	var tr *transform.Reader
	tr = transform.NewReader(strings.NewReader(_loc.area_desc), zh.GBK.NewDecoder())
	if s, err := ioutil.ReadAll(tr); err == nil {
		loc.AreaDesc = string(s)
	}

	tr = transform.NewReader(strings.NewReader(_loc.op_desc), zh.GBK.NewDecoder())
	if s, err := ioutil.ReadAll(tr); err == nil {
		loc.OpDesc = string(s)
	}

	loc.OK = _loc.ok

	// 提取运营商名称
	re := regexp.MustCompile("(铁通|电信|联通|移动|网通)")
	loc.OpName = re.FindString(loc.OpDesc)

	// 提取省份名称
	re_area := regexp.MustCompile("(北京|天津|河北|山西|内蒙|辽宁|吉林|黑龙|上海|江苏|浙江|安徽|福建|江西|山东|河南|湖北|湖南|广东|广西|海南|重庆|四川|贵州|云南|西藏|陕西|甘肃|青海|宁夏|新疆|香港|澳门|台湾)")
	loc.AreaName = re_area.FindString(loc.AreaDesc)

	return loc, nil
}

type tIp2LocationResp struct {
	ok        bool
	area_desc string
	op_desc   string
}

func (q *QQWryQuery) getIpLocation(offset int64) (loc tIp2LocationResp) {
	buf := make([]byte, 1024)

	q.file.ReadAt(buf[0:1], offset+4)

	mod := buf[0]

	descOffset := int64(0)
	op_descOffset := int64(0)

	if 0x01 == mod {
		descOffset = q._readLong3(offset + 5)

		q.file.ReadAt(buf[0:1], descOffset)

		mod2 := buf[0]

		if 0x02 == mod2 {
			loc.area_desc = q._readString(q._readLong3(descOffset + 1))
			op_descOffset = descOffset + 4
		} else {
			loc.area_desc = q._readString(descOffset)
			op_descOffset = descOffset + int64(len(loc.area_desc)) + 1
		}

		loc.op_desc = q._readArea(op_descOffset)

	} else if 0x02 == mod {
		loc.area_desc = q._readString(q._readLong3(offset + 5))
		loc.op_desc = q._readArea(offset + 8)
	} else {
		loc.area_desc = q._readString(offset + 4)
		op_descOffset = offset + 4 + int64(len(loc.area_desc)) + 1
		loc.op_desc = q._readArea(op_descOffset)
	}

	loc.ok = true

	return
}

func (q *QQWryQuery) _readArea(offset int64) string {
	buf := make([]byte, 4)

	q.file.ReadAt(buf[0:1], offset)

	mod := buf[0]

	if 0x01 == mod || 0x02 == mod {
		op_descOffset := q._readLong3(offset + 1)
		if op_descOffset == 0 {
			return ""
		} else {
			return q._readString(op_descOffset)
		}
	}
	return q._readString(offset)
}

func (q *QQWryQuery) _readLong3(offset int64) int64 {
	buf := make([]byte, 4)
	q.file.ReadAt(buf, offset)
	buf[3] = 0x00

	return int64(binary.LittleEndian.Uint32(buf))
}

func (q *QQWryQuery) _readString(offset int64) string {
	buf := make([]byte, 1024)
	got := int64(0)

	for ; got < 1024; got++ {
		q.file.ReadAt(buf[got:got+1], offset+got)

		if buf[got] == 0x00 {
			break
		}
	}

	return string(buf[0:got])
}
